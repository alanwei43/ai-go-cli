package systemservice

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const unitName = "switch-hosts.service"

// Manager installs a system service or reports an existing service without changing it.
type Manager struct {
	output     io.Writer
	platform   string
	unitPath   string
	geteuid    func() int
	executable func() (string, error)
	run        func(string, ...string) ([]byte, error)
}

func NewManager(output io.Writer) *Manager {
	return &Manager{
		output:     output,
		platform:   runtime.GOOS,
		unitPath:   "/etc/systemd/system/" + unitName,
		geteuid:    os.Geteuid,
		executable: os.Executable,
		run: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).CombinedOutput()
		},
	}
}

// Add creates, enables and starts a new unit. Existing units are only inspected.
func (m *Manager) Add(remoteURL string, interval int) error {
	if m.platform != "linux" {
		return fmt.Errorf("--add-systemd 仅支持 Linux，当前系统为 %s", m.platform)
	}
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than 0")
	}
	state, err := m.state()
	if err != nil {
		return err
	}
	_, pathErr := os.Lstat(m.unitPath)
	if pathErr != nil && !os.IsNotExist(pathErr) {
		return fmt.Errorf("检查 systemd 服务文件: %w", pathErr)
	}
	if state["LoadState"] != "not-found" || pathErr == nil {
		return m.report(state)
	}
	if m.geteuid() != 0 {
		return fmt.Errorf("创建 %s 需要 root 权限，请使用 sudo；服务需写入 /etc/hosts", unitName)
	}
	parsedURL, err := url.Parse(remoteURL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || strings.ContainsAny(remoteURL, "\x00\r\n") {
		return fmt.Errorf("服务 URL 必须是有效的 HTTP 或 HTTPS 地址")
	}
	source, err := m.executable()
	if err != nil {
		return fmt.Errorf("获取当前程序路径: %w", err)
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("解析当前程序路径: %w", err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("检查当前可执行文件: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("当前程序不是可执行文件: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(m.unitPath), 0o755); err != nil {
		return fmt.Errorf("创建 systemd 服务目录: %w", err)
	}
	// Reserve the unit exclusively so a concurrent invocation cannot overwrite it.
	unit, err := os.OpenFile(m.unitPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("创建 %s（不会覆盖已有文件）: %w", m.unitPath, err)
	}
	_, writeErr := io.WriteString(unit, unitContent(source, remoteURL, interval))
	closeErr := unit.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(m.unitPath)
		if writeErr != nil {
			return fmt.Errorf("写入 systemd 服务文件: %w", writeErr)
		}
		return fmt.Errorf("关闭 systemd 服务文件: %w", closeErr)
	}
	fmt.Fprintf(m.output, "已创建 %s，服务以 root 运行，程序路径: %s\n", m.unitPath, source)
	if _, err := m.command("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if _, err := m.command("systemctl", "enable", "--now", unitName); err != nil {
		return err
	}
	fmt.Fprintf(m.output, "已启用并启动 %s\n", unitName)
	state, err = m.state()
	if err != nil {
		return err
	}
	return m.report(state)
}

// Delete stops and disables the service before removing its unit file.
func (m *Manager) Delete() error {
	if m.platform != "linux" {
		return fmt.Errorf("--del-systemd 仅支持 Linux，当前系统为 %s", m.platform)
	}
	if m.geteuid() != 0 {
		return fmt.Errorf("删除 %s 需要 root 权限，请使用 sudo", unitName)
	}
	state, err := m.state()
	if err != nil {
		return err
	}
	path := m.unitPath
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("检查 systemd 服务文件: %w", err)
	}
	if os.IsNotExist(err) {
		if state["LoadState"] == "not-found" {
			fmt.Fprintf(m.output, "%s 不存在，无需删除\n", unitName)
			return nil
		}
		// Existing units can also be installed in a different systemd unit directory.
		path = state["FragmentPath"]
		if !filepath.IsAbs(path) || filepath.Base(path) != unitName {
			return fmt.Errorf("无法定位 %s 的服务文件", unitName)
		}
		info, err = os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("检查 systemd 服务文件: %w", err)
		}
	}
	if info != nil && info.IsDir() {
		return fmt.Errorf("systemd 服务文件路径是目录: %s", path)
	}
	if state["LoadState"] == "not-found" {
		// A unit left behind by an interrupted installation may not be loaded yet.
		if _, err := m.command("systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	if _, err := m.command("systemctl", "stop", unitName); err != nil {
		return err
	}
	if _, err := m.command("systemctl", "disable", unitName); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 systemd 服务文件: %w", err)
	}
	if _, err := m.command("systemctl", "daemon-reload"); err != nil {
		return err
	}
	fmt.Fprintf(m.output, "已停止、禁用并删除 %s，服务文件: %s\n", unitName, path)
	return nil
}

func (m *Manager) command(name string, args ...string) ([]byte, error) {
	output, err := m.run(name, args...)
	if err != nil {
		return output, fmt.Errorf("%s %s: %w; %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (m *Manager) state() (map[string]string, error) {
	output, err := m.command("systemctl", "show", unitName, "--no-pager", "--all", "--property=LoadState,UnitFileState,ActiveState,SubState,FragmentPath")
	state := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			state[key] = value
		}
	}
	// Some systemctl versions exit unsuccessfully when the unit does not exist.
	if err != nil && state["LoadState"] != "not-found" {
		return nil, err
	}
	if state["LoadState"] == "" {
		return nil, fmt.Errorf("无法读取 %s 的 LoadState", unitName)
	}
	return state, nil
}

func (m *Manager) report(state map[string]string) error {
	enabled, running := "否", "否"
	if state["UnitFileState"] == "enabled" || state["UnitFileState"] == "enabled-runtime" {
		enabled = "是"
	}
	if state["ActiveState"] == "active" && state["SubState"] == "running" {
		running = "是"
	}
	fmt.Fprintf(m.output, "%s\n是否启用: %s (%s)\n是否正在运行: %s (%s/%s)\n加载状态: %s\n最近 50 条日志:\n", unitName, enabled, state["UnitFileState"], running, state["ActiveState"], state["SubState"], state["LoadState"])
	logs, err := m.command("journalctl", "--unit="+unitName, "--lines=50", "--no-pager")
	if err != nil {
		return err
	}
	if len(logs) == 0 {
		fmt.Fprintln(m.output, "暂无日志")
	} else {
		fmt.Fprintln(m.output, strings.TrimRight(string(logs), "\n"))
	}
	return nil
}

func unitContent(binaryPath, remoteURL string, interval int) string {
	return `[Unit]
Description=Synchronize local IP and subscribe to hosts
Wants=network-online.target
After=network-online.target

[Service]
Type=exec
User=root
Group=root
ExecStart=` + quoteArgument(binaryPath) + " service --interval " + strconv.Itoa(interval) + " -- " + quoteArgument(remoteURL) + `
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`
}

// systemd command lines have their own quoting, specifier and environment expansion.
func quoteArgument(value string) string {
	escaped := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%", "$", "$$", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(value)
	return "\"" + escaped + "\""
}
