// Package zipper creates Store archives using the system's zip executable.
package zipper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Archiver struct {
	source, target, name, password, executable string
	workingDir, operand, exclusion             string
}

func New(source, target, passwordFile string) (*Archiver, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("auto-zip 仅支持 Linux/macOS")
	}
	if strings.TrimSpace(source) == "" || strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("源目录和目标目录不能为空")
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("解析源目录: %w", err)
	}
	name := filepath.Base(absSource)
	source, err = filepath.EvalSymlinks(absSource)
	if err != nil {
		return nil, fmt.Errorf("读取源目录: %w", err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("读取源目录: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("源路径不是目录: %s", source)
	}
	executable, err := exec.LookPath("zip")
	if err != nil {
		return nil, fmt.Errorf("找不到系统 zip 命令，请安装系统 zip 工具: %w", err)
	}
	password := ""
	if passwordFile != "" {
		data, err := os.ReadFile(passwordFile)
		if err != nil {
			return nil, fmt.Errorf("读取密码文件: %w", err)
		}
		password = strings.TrimRight(string(data), "\r\n")
		if password == "" {
			return nil, fmt.Errorf("密码文件内容不能为空")
		}
		if strings.ContainsRune(password, '\x00') {
			return nil, fmt.Errorf("密码不能包含 NUL 字符")
		}
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("解析目标目录: %w", err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return nil, fmt.Errorf("创建目标目录: %w", err)
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return nil, fmt.Errorf("解析目标目录: %w", err)
	}
	if source == target {
		return nil, fmt.Errorf("目标目录不能与源目录相同")
	}
	archiver := &Archiver{
		source: source, target: target, name: name, password: password, executable: executable,
		workingDir: filepath.Dir(source), operand: "./" + filepath.Base(source),
	}
	// Filesystem roots have no parent directory to use as an archive prefix.
	if source == filepath.Dir(source) {
		archiver.name, archiver.workingDir, archiver.operand = "root", source, "."
	}
	if relative, err := filepath.Rel(source, target); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		entry := filepath.Join(filepath.Base(source), relative)
		if archiver.operand == "." {
			entry = relative
		}
		archiver.exclusion = escapePattern(filepath.ToSlash(entry)) + "/*"
	}
	return archiver, nil
}

func (a *Archiver) Source() string { return a.source }
func (a *Archiver) Target() string { return a.target }

func (a *Archiver) Create(ctx context.Context, now time.Time) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := filepath.Join(a.target, a.name+"_"+now.Format("2006-01-02-15-04-05")+".zip")
	staging, err := os.MkdirTemp(a.target, ".auto-zip-*")
	if err != nil {
		return "", fmt.Errorf("创建打包临时目录: %w", err)
	}
	defer os.RemoveAll(staging)
	temporary := filepath.Join(staging, "archive.zip")
	args := []string{"-r0", "-q", "-y", "-MM"}
	if a.password != "" {
		args = append(args, "-P", a.password)
	}
	args = append(args, temporary)
	if a.exclusion != "" {
		args = append(args, "-x", a.exclusion, "@")
	}
	args = append(args, "--", a.operand)
	command := exec.CommandContext(ctx, a.executable, args...)
	command.Dir = a.workingDir
	// Environment options must not override Store mode or alter encryption.
	for _, variable := range os.Environ() {
		key, _, _ := strings.Cut(variable, "=")
		if key != "ZIP" && key != "ZIPOPT" {
			command.Env = append(command.Env, variable)
		}
	}
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("系统 zip 执行失败: %w；%s", err, strings.TrimSpace(string(output)))
	}
	// Publish a complete archive atomically, without replacing an existing name.
	// Both paths are on the target filesystem, so linking cannot cross devices.
	if err := os.Link(temporary, path); err != nil {
		return "", fmt.Errorf("保存 ZIP（不会覆盖同名文件）: %w", err)
	}
	return path, nil
}

func escapePattern(path string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(path)
}
