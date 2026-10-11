// Package systemservice installs and removes auto-zip systemd system services.
package systemservice

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const ownershipMarker = "# Managed by auto-zip\n"

var serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func UnitName(name string) (string, error) {
	if !serviceNamePattern.MatchString(name) {
		return "", fmt.Errorf("服务名必须以英文字母或数字开头，且仅包含字母、数字、点、下划线和连字符")
	}
	if !strings.HasSuffix(name, ".service") {
		name += ".service"
	}
	if len(name) > 255 {
		return "", fmt.Errorf("服务文件名不能超过 255 字节")
	}
	return name, nil
}

type Manager struct {
	platform   string
	directory  string
	geteuid    func() int
	executable func() (string, error)
	run        func(context.Context, ...string) ([]byte, error)
}

func NewManager() *Manager {
	return &Manager{
		platform: runtime.GOOS, directory: "/etc/systemd/system",
		geteuid: os.Geteuid, executable: os.Executable,
		run: func(ctx context.Context, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
		},
	}
}

func (m *Manager) check() error {
	if m.platform != "linux" {
		return fmt.Errorf("systemd 参数仅支持 Linux，当前系统为 %s", m.platform)
	}
	if m.geteuid() != 0 {
		return fmt.Errorf("管理 systemd 系统服务需要 root 权限，请使用 sudo")
	}
	return nil
}

// Add leaves the generated file available for inspection and Delete if a
// systemctl operation fails. It never overwrites an existing system service.
func (m *Manager) Add(ctx context.Context, name string, args []string) error {
	if err := m.check(); err != nil {
		return err
	}
	unit, err := UnitName(name)
	if err != nil {
		return err
	}
	path := filepath.Join(m.directory, unit)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("服务文件已存在，不会覆盖: %s", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查服务文件: %w", err)
	}
	// Include units from vendor and runtime directories in the collision check.
	output, err := m.command(ctx, "list-unit-files", "--type=service", "--no-legend", "--no-pager", "--", unit)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(output)) != "" {
		return fmt.Errorf("systemd 已存在同名服务，不会覆盖: %s", unit)
	}
	binary, err := m.executable()
	if err != nil {
		return fmt.Errorf("获取当前程序路径: %w", err)
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return fmt.Errorf("解析当前程序路径: %w", err)
	}
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("当前程序路径不是可用的可执行文件: %s", binary)
	}
	content, err := unitContent(binary, args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.directory, 0755); err != nil {
		return fmt.Errorf("创建 systemd 服务目录: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("创建服务文件（不会覆盖已有文件）: %w", err)
	}
	_, writeErr := io.WriteString(file, content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		if writeErr != nil {
			return fmt.Errorf("写入服务文件: %w", writeErr)
		}
		return fmt.Errorf("关闭服务文件: %w", closeErr)
	}
	if _, err := m.command(ctx, "daemon-reload"); err != nil {
		return fmt.Errorf("服务文件保留于 %s: %w", path, err)
	}
	if _, err := m.command(ctx, "enable", "--now", "--", unit); err != nil {
		return fmt.Errorf("服务文件保留于 %s，可用 --del-systemd 删除: %w", path, err)
	}
	return nil
}

// Delete disables and stops the service before removing its unit and drop-ins.
// systemctl disable also removes the associated enablement symlinks.
func (m *Manager) Delete(ctx context.Context, name string) error {
	if err := m.check(); err != nil {
		return err
	}
	unit, err := UnitName(name)
	if err != nil {
		return err
	}
	path := filepath.Join(m.directory, unit)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查服务文件: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("服务文件不是普通文件: %s", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取服务文件: %w", err)
	}
	if !strings.HasPrefix(string(content), ownershipMarker) {
		return fmt.Errorf("服务文件并非 auto-zip 创建，拒绝删除: %s", path)
	}
	if _, err := m.command(ctx, "disable", "--now", "--", unit); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("删除服务文件: %w", err)
	}
	if err := os.RemoveAll(path + ".d"); err != nil {
		return fmt.Errorf("删除服务附加配置: %w", err)
	}
	if _, err := m.command(ctx, "daemon-reload"); err != nil {
		return err
	}
	return nil
}

func (m *Manager) command(ctx context.Context, args ...string) ([]byte, error) {
	output, err := m.run(ctx, args...)
	if err != nil {
		return output, fmt.Errorf("systemctl %s: %w；%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func unitContent(binary string, args []string) (string, error) {
	if !filepath.IsAbs(binary) {
		return "", fmt.Errorf("服务程序路径必须为绝对路径")
	}
	// systemd rejects these characters in the executable name even when quoted.
	// They remain valid in the arguments containing source/target/password paths.
	for i := 0; i < len(binary); i++ {
		if binary[i] < 0x20 || binary[i] == 0x7f || strings.ContainsRune("\"'\\", rune(binary[i])) {
			return "", fmt.Errorf("systemd 不支持程序路径中的引号、反斜杠或控制字符，请将 auto-zip 放在普通路径")
		}
	}
	values := append([]string{binary}, args...)
	for _, value := range values {
		if strings.ContainsRune(value, '\x00') {
			return "", fmt.Errorf("服务命令参数不能包含 NUL 字符")
		}
	}
	command := []string{quoteArgument(binary)}
	for _, arg := range args {
		command = append(command, quoteArgument(arg))
	}
	return ownershipMarker + `[Unit]
Description=auto-zip scheduled directory archive

[Service]
Type=exec
WorkingDirectory=/
ExecStart=:` + strings.Join(command, " ") + `
Restart=on-failure
RestartSec=1h

[Install]
WantedBy=multi-user.target
`, nil
}

// systemd is not a shell. Escape specifiers and syntax characters, and use the
// ExecStart ':' prefix to preserve literal '$' characters without expansion.
func quoteArgument(value string) string {
	var quoted strings.Builder
	quoted.WriteByte('"')
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\', '"':
			quoted.WriteByte('\\')
			quoted.WriteByte(value[i])
		case '%':
			quoted.WriteString("%%")
		default:
			if value[i] < 0x20 || value[i] == 0x7f {
				fmt.Fprintf(&quoted, "\\x%02x", value[i])
			} else {
				quoted.WriteByte(value[i])
			}
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
}
