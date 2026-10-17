// Package zipper creates Store archives using the system's zip executable.
package zipper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Archiver struct {
	source, target, name, password, executable string
	list                                       bool
	maxCount                                   int
}

const timestampLayout = "20060102150405"

var archiveSuffixPattern = regexp.MustCompile(`^([0-9]{14})_[0-9a-f]{64}\.zip$`)

func New(source, target, passwordFile, zipName string, maxCount int) (*Archiver, error) {
	if maxCount < 0 {
		return nil, fmt.Errorf("--max-count 必须为非负整数，0 表示不限制")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("auto-zip 仅支持 Linux/macOS")
	}
	if strings.TrimSpace(source) == "" || strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("源路径和目标目录不能为空")
	}
	if zipName != "" && (strings.TrimSpace(zipName) == "" || strings.ContainsAny(zipName, `/\`) || strings.ContainsFunc(zipName, unicode.IsControl)) {
		return nil, fmt.Errorf("--zip-name 必须为非空文件名前缀，不能包含路径分隔符或控制字符")
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("解析源路径: %w", err)
	}
	name := filepath.Base(absSource)
	source, err = filepath.EvalSymlinks(absSource)
	if err != nil {
		return nil, fmt.Errorf("读取源路径: %w", err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("读取源路径: %w", err)
	}
	list := !info.IsDir()
	if list && (!info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(absSource), ".txt")) {
		return nil, fmt.Errorf("源路径必须是目录或 .txt 文件: %s", absSource)
	}
	// Keep the list's original location as the base for relative entries.
	if list {
		source = absSource
		name = strings.TrimSuffix(name, filepath.Ext(name))
		if name == "" {
			name = "list"
		}
		if _, err := readList(source); err != nil {
			return nil, err
		}
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
	archiver := &Archiver{
		source: source, target: target, name: name, password: password, executable: executable,
		list: list, maxCount: maxCount,
	}
	if source == filepath.Dir(source) {
		archiver.name = "root"
	}
	if zipName != "" {
		archiver.name = zipName
	}
	if _, err := archiver.plan(); err != nil {
		return nil, err
	}
	return archiver, nil
}

func (a *Archiver) Source() string { return a.source }
func (a *Archiver) Target() string { return a.target }

func (a *Archiver) Create(ctx context.Context, now time.Time) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := a.checkTimestamp(now); err != nil {
		return "", err
	}
	plan, err := a.plan()
	if err != nil {
		return "", err
	}
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
	if plan.exclusion != "" {
		args = append(args, "-x", plan.exclusion, "@")
	}
	args = append(args, "--")
	args = append(args, plan.operands...)
	command := exec.CommandContext(ctx, a.executable, args...)
	command.Dir = plan.workingDir
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
	digest, err := fileSHA256(ctx, temporary)
	if err != nil {
		return "", fmt.Errorf("计算 ZIP SHA-256: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := filepath.Join(a.target, a.name+"_"+now.Format(timestampLayout)+"_"+digest+".zip")
	// Publish a complete archive atomically, without replacing an existing name.
	// Both paths are on the target filesystem, so linking cannot cross devices.
	if err := os.Link(temporary, path); err != nil {
		return "", fmt.Errorf("保存 ZIP（不会覆盖同名文件）: %w", err)
	}
	if err := a.prune(ctx, path); err != nil {
		return path, fmt.Errorf("ZIP 已保存至 %s，但清理历史 ZIP 失败: %w", path, err)
	}
	return path, nil
}

// Keep the existing one-archive-per-second collision protection separate from
// retention, which must only run after the new archive has been published.
func (a *Archiver) checkTimestamp(now time.Time) error {
	entries, err := os.ReadDir(a.target)
	if err != nil {
		return fmt.Errorf("读取历史 ZIP: %w", err)
	}
	for _, entry := range entries {
		if stamp, ok := a.archiveTimestamp(entry.Name()); ok && stamp == now.Format(timestampLayout) {
			return fmt.Errorf("保存 ZIP（同一前缀和时间戳已存在，不会覆盖）: %w", os.ErrExist)
		}
	}
	return nil
}

// prune counts the completed archive together with matching historical archives,
// then removes the oldest excess files, always keeping the just-published ZIP.
func (a *Archiver) prune(ctx context.Context, keepPath string) error {
	if a.maxCount == 0 {
		return nil
	}
	entries, err := os.ReadDir(a.target)
	if err != nil {
		return fmt.Errorf("读取历史 ZIP: %w", err)
	}
	type archive struct{ name, stamp string }
	var archives []archive
	for _, entry := range entries {
		name := entry.Name()
		stamp, ok := a.archiveTimestamp(name)
		if !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("读取历史 ZIP 信息: %w", err)
		}
		if info.Mode().IsRegular() {
			archives = append(archives, archive{name, stamp})
		}
	}
	sort.Slice(archives, func(i, j int) bool {
		if archives[i].stamp == archives[j].stamp {
			return archives[i].name < archives[j].name
		}
		return archives[i].stamp < archives[j].stamp
	})
	excess := len(archives) - a.maxCount
	for _, archive := range archives {
		if excess <= 0 {
			break
		}
		path := filepath.Join(a.target, archive.name)
		if path == keepPath {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("删除历史 ZIP %s: %w", path, err)
		}
		excess--
	}
	return nil
}

// Match the prefix literally before validating the entire remaining suffix.
// Prefixes may contain underscores and regular expression metacharacters.
func (a *Archiver) archiveTimestamp(name string) (string, bool) {
	prefix := a.name + "_"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	match := archiveSuffixPattern.FindStringSubmatch(strings.TrimPrefix(name, prefix))
	if match == nil {
		return "", false
	}
	stamp := match[1]
	parsed, err := time.Parse(timestampLayout, stamp)
	if err != nil || parsed.Format(timestampLayout) != stamp {
		return "", false
	}
	return stamp, true
}

func fileSHA256(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := file.Read(buffer)
		hash.Write(buffer[:n])
		if err == io.EOF {
			return hex.EncodeToString(hash.Sum(nil)), nil
		}
		if err != nil {
			return "", err
		}
	}
}

func escapePattern(path string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(path)
}
