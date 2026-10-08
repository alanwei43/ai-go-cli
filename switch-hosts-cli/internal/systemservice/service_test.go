package systemservice

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const missingState = "LoadState=not-found\nUnitFileState=\nActiveState=inactive\nSubState=dead\n"
const runningState = "LoadState=loaded\nUnitFileState=enabled\nActiveState=active\nSubState=running\n"

func testManager(t *testing.T) (*Manager, *bytes.Buffer) {
	t.Helper()
	var output bytes.Buffer
	m := NewManager(&output)
	m.platform = "linux"
	m.geteuid = func() int { return 0 }
	dir := t.TempDir()
	m.unitPath = filepath.Join(dir, "systemd", unitName)
	source := filepath.Join(dir, "hosts tools", "hosts")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("test binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.executable = func() (string, error) { return source, nil }
	return m, &output
}

func TestAddUsesCurrentExecutableAndStartsIt(t *testing.T) {
	m, output := testManager(t)
	var calls [][]string
	shows := 0
	m.run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "systemctl" && args[0] == "show" {
			shows++
			if shows == 1 {
				return []byte(missingState), errors.New("exit status 1")
			}
			return []byte(runningState), nil
		}
		if name == "journalctl" {
			return []byte("hosts updated\n"), nil
		}
		return nil, nil
	}
	const remoteURL = "https://example.test/hosts?team=office%20one&token=${TOKEN}"
	if err := m.Add(remoteURL, 17); err != nil {
		t.Fatal(err)
	}
	wantCalls := [][]string{
		{"systemctl", "show", unitName, "--no-pager", "--all", "--property=LoadState,UnitFileState,ActiveState,SubState,FragmentPath"},
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", unitName},
		{"systemctl", "show", unitName, "--no-pager", "--all", "--property=LoadState,UnitFileState,ActiveState,SubState,FragmentPath"},
		{"journalctl", "--unit=" + unitName, "--lines=50", "--no-pager"},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
	unit, err := os.ReadFile(m.unitPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"User=root\n", "Group=root\n", "Type=exec\n", "Restart=on-failure\n", "WantedBy=multi-user.target\n", ` service --interval 17 -- "https://example.test/hosts?team=office%%20one&token=$${TOKEN}"`} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("unit missing %q: %s", want, unit)
		}
	}
	if strings.Contains(string(unit), "--add-systemd") || strings.Contains(string(unit), "--del-systemd") {
		t.Fatal("service must run the foreground command, not recursively install itself")
	}
	source, err := m.executable()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), `ExecStart="`+source+`" service`) {
		t.Fatalf("unit must reference the original executable: %s", unit)
	}
	binary, err := os.ReadFile(source)
	if err != nil || string(binary) != "test binary" {
		t.Fatalf("original binary = %q, error = %v", binary, err)
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(m.unitPath), "..", "*", "*"))
	if err != nil || len(files) != 2 {
		t.Fatalf("only the original binary and unit should exist: %v, %v", files, err)
	}
	for _, want := range []string{"是否启用: 是 (enabled)", "是否正在运行: 是 (active/running)", "hosts updated"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
}

func TestAddExistingServiceIsReadOnly(t *testing.T) {
	for _, state := range []string{runningState, "LoadState=loaded\nUnitFileState=disabled\nActiveState=inactive\nSubState=dead\n", "LoadState=masked\nUnitFileState=masked\nActiveState=failed\nSubState=failed\n", missingState} {
		t.Run(strings.TrimSpace(state), func(t *testing.T) {
			m, output := testManager(t)
			// Even a unit not yet loaded by systemd must never be overwritten.
			if err := os.MkdirAll(filepath.Dir(m.unitPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(m.unitPath, []byte("existing unit"), 0o644); err != nil {
				t.Fatal(err)
			}
			m.geteuid = func() int { return 1000 }
			m.executable = func() (string, error) { t.Fatal("must not install"); return "", nil }
			var commands []string
			m.run = func(name string, args ...string) ([]byte, error) {
				commands = append(commands, name+" "+args[0])
				if name == "systemctl" && args[0] == "show" {
					return []byte(state), nil
				}
				if name == "journalctl" {
					return []byte("recent log"), nil
				}
				t.Fatalf("unexpected command: %s %v", name, args)
				return nil, nil
			}
			if err := m.Add("https://changed.test/ignored", 99); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(commands, []string{"systemctl show", "journalctl --unit=" + unitName}) {
				t.Fatalf("unexpected commands: %v", commands)
			}
			unit, err := os.ReadFile(m.unitPath)
			if err != nil || string(unit) != "existing unit" {
				t.Fatalf("existing unit changed: %q, %v", unit, err)
			}
			if !strings.Contains(output.String(), "recent log") {
				t.Fatalf("missing logs: %s", output)
			}
			if state != runningState && (!strings.Contains(output.String(), "是否启用: 否") || !strings.Contains(output.String(), "是否正在运行: 否")) {
				t.Fatalf("incorrect inactive state: %s", output)
			}
		})
	}
}

func TestAddFindsVendorUnitWithoutLocalFile(t *testing.T) {
	m, _ := testManager(t)
	m.geteuid = func() int { return 1000 }
	m.run = func(name string, args ...string) ([]byte, error) {
		if name == "systemctl" && args[0] == "show" {
			return []byte(runningState), nil
		}
		if name == "journalctl" {
			return nil, nil
		}
		t.Fatalf("unexpected mutation: %s %v", name, args)
		return nil, nil
	}
	if err := m.Add("https://example.test/hosts", 17); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.unitPath); !os.IsNotExist(err) {
		t.Fatalf("must not create another unit: %v", err)
	}
}

func TestAddFailures(t *testing.T) {
	tests := []struct {
		name, platform, failure, remoteURL, want string
		uid, interval                            int
		wantUnit                                 bool
	}{
		{name: "macOS", platform: "darwin", uid: 0, interval: 17, want: "仅支持 Linux"},
		{name: "Windows", platform: "windows", uid: 0, interval: 17, want: "仅支持 Linux"},
		{name: "invalid interval", platform: "linux", uid: 0, interval: 0, want: "greater than 0"},
		{name: "not root", platform: "linux", uid: 1000, interval: 17, want: "root 权限"},
		{name: "systemd unavailable", platform: "linux", uid: 0, interval: 17, failure: "show", want: "bus unavailable"},
		{name: "invalid show output", platform: "linux", uid: 0, interval: 17, failure: "empty", want: "LoadState"},
		{name: "invalid URL", platform: "linux", uid: 0, interval: 17, remoteURL: "file:///etc/hosts", want: "HTTP 或 HTTPS"},
		{name: "executable unavailable", platform: "linux", uid: 0, interval: 17, failure: "executable", want: "程序路径"},
		{name: "executable missing", platform: "linux", uid: 0, interval: 17, failure: "missing", want: "检查当前可执行文件"},
		{name: "not executable", platform: "linux", uid: 0, interval: 17, failure: "permissions", want: "不是可执行文件"},
		{name: "reload failed", platform: "linux", uid: 0, interval: 17, failure: "daemon-reload", want: "operation failed", wantUnit: true},
		{name: "start failed", platform: "linux", uid: 0, interval: 17, failure: "enable", want: "operation failed", wantUnit: true},
		{name: "journal failed", platform: "linux", uid: 0, interval: 17, failure: "journalctl", want: "operation failed", wantUnit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := testManager(t)
			m.platform = tt.platform
			m.geteuid = func() int { return tt.uid }
			if tt.failure == "executable" {
				m.executable = func() (string, error) { return "", errors.New("unavailable") }
			} else if tt.failure == "missing" {
				m.executable = func() (string, error) { return filepath.Join(t.TempDir(), "missing"), nil }
			}
			if tt.failure == "permissions" {
				source, _ := m.executable()
				if err := os.Chmod(source, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			shows := 0
			m.run = func(name string, args ...string) ([]byte, error) {
				if tt.platform != "linux" || tt.interval <= 0 {
					t.Fatal("must reject before calling systemctl")
				}
				if name == "systemctl" && args[0] == "show" {
					shows++
					if tt.failure == "show" {
						return []byte("bus unavailable"), errors.New("exit status 1")
					}
					if tt.failure == "empty" {
						return nil, nil
					}
					if shows == 1 {
						return []byte(missingState), nil
					}
					return []byte(runningState), nil
				}
				if tt.failure == name || tt.failure == args[0] {
					return []byte("operation failed"), errors.New("exit status 1")
				}
				return nil, nil
			}
			remoteURL := tt.remoteURL
			if remoteURL == "" {
				remoteURL = "https://example.test/hosts"
			}
			err := m.Add(remoteURL, tt.interval)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			_, statErr := os.Stat(m.unitPath)
			if tt.wantUnit && statErr != nil || !tt.wantUnit && !os.IsNotExist(statErr) {
				t.Fatalf("unit existence error = %v, want unit = %v", statErr, tt.wantUnit)
			}
		})
	}
}

func TestDeleteStopsDisablesAndRemovesUnit(t *testing.T) {
	for _, variant := range []string{"local", "other directory", "not loaded yet", "file already removed"} {
		t.Run(variant, func(t *testing.T) {
			m, output := testManager(t)
			path := m.unitPath
			if variant == "other directory" {
				path = filepath.Join(filepath.Dir(m.unitPath), "vendor", unitName)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if variant != "file already removed" {
				if err := os.WriteFile(path, []byte("existing unit"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			source, _ := m.executable()
			m.executable = func() (string, error) { t.Fatal("must not access executable during deletion"); return "", nil }
			var commands []string
			m.run = func(name string, args ...string) ([]byte, error) {
				if name != "systemctl" {
					t.Fatalf("unexpected command: %s %v", name, args)
				}
				commands = append(commands, args[0])
				if args[0] == "show" {
					if variant == "not loaded yet" {
						return []byte(missingState), nil
					}
					return []byte(runningState + "FragmentPath=" + path + "\n"), nil
				}
				if args[0] == "stop" || args[0] == "disable" {
					if len(args) != 2 || args[1] != unitName {
						t.Fatalf("must only target %s: %v", unitName, args)
					}
					if _, err := os.Stat(path); err != nil && variant != "file already removed" {
						t.Fatal("must not remove the unit before stopping and disabling it")
					}
				}
				return nil, nil
			}
			if err := m.Delete(); err != nil {
				t.Fatal(err)
			}
			wantCommands := []string{"show", "stop", "disable", "daemon-reload"}
			if variant == "not loaded yet" {
				wantCommands = []string{"show", "daemon-reload", "stop", "disable", "daemon-reload"}
			}
			if !reflect.DeepEqual(commands, wantCommands) {
				t.Fatalf("commands = %v, want %v", commands, wantCommands)
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("unit should be removed: %v", err)
			}
			binary, err := os.ReadFile(source)
			if err != nil || string(binary) != "test binary" {
				t.Fatalf("original executable changed: %q, %v", binary, err)
			}
			if !strings.Contains(output.String(), "已停止、禁用并删除") {
				t.Fatalf("missing success message: %s", output)
			}
		})
	}
}

func TestDeleteMissingService(t *testing.T) {
	m, output := testManager(t)
	m.run = func(name string, args ...string) ([]byte, error) {
		if name != "systemctl" || args[0] != "show" {
			t.Fatalf("nonexistent service must not be changed: %s %v", name, args)
		}
		return []byte(missingState), errors.New("exit status 1")
	}
	if err := m.Delete(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "不存在，无需删除") {
		t.Fatalf("missing nonexistent service message: %s", output)
	}
}

func TestDeleteRejectsUnknownUnitFile(t *testing.T) {
	for _, fragment := range []string{"", "relative/switch-hosts.service", "/etc/systemd/system/other.service"} {
		t.Run(fragment, func(t *testing.T) {
			m, _ := testManager(t)
			m.run = func(name string, args ...string) ([]byte, error) {
				if name != "systemctl" || args[0] != "show" {
					t.Fatalf("must locate the unit before changing it: %s %v", name, args)
				}
				return []byte(runningState + "FragmentPath=" + fragment + "\n"), nil
			}
			if err := m.Delete(); err == nil || !strings.Contains(err.Error(), "无法定位") {
				t.Fatalf("error = %v, want unknown unit file error", err)
			}
		})
	}
}

func TestDeleteFailures(t *testing.T) {
	tests := []struct {
		name, platform, failure, want string
		uid                           int
		wantCommands                  []string
		unitRemoved                   bool
	}{
		{name: "macOS", platform: "darwin", uid: 0, want: "--del-systemd 仅支持 Linux"},
		{name: "Windows", platform: "windows", uid: 0, want: "--del-systemd 仅支持 Linux"},
		{name: "not root", platform: "linux", uid: 1000, want: "root 权限"},
		{name: "systemd unavailable", platform: "linux", uid: 0, failure: "show", want: "operation failed", wantCommands: []string{"show"}},
		{name: "stop failed", platform: "linux", uid: 0, failure: "stop", want: "operation failed", wantCommands: []string{"show", "stop"}},
		{name: "disable failed", platform: "linux", uid: 0, failure: "disable", want: "operation failed", wantCommands: []string{"show", "stop", "disable"}},
		{name: "remove failed", platform: "linux", uid: 0, failure: "remove", want: "删除 systemd 服务文件", wantCommands: []string{"show", "stop", "disable"}},
		{name: "reload failed", platform: "linux", uid: 0, failure: "daemon-reload", want: "operation failed", wantCommands: []string{"show", "stop", "disable", "daemon-reload"}, unitRemoved: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, output := testManager(t)
			m.platform = tt.platform
			m.geteuid = func() int { return tt.uid }
			if err := os.MkdirAll(filepath.Dir(m.unitPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(m.unitPath, []byte("existing unit"), 0o644); err != nil {
				t.Fatal(err)
			}
			var commands []string
			m.run = func(name string, args ...string) ([]byte, error) {
				commands = append(commands, args[0])
				if args[0] == tt.failure {
					return []byte("operation failed"), errors.New("exit status 1")
				}
				if args[0] == "show" {
					return []byte(runningState), nil
				}
				if args[0] == "disable" && tt.failure == "remove" {
					if err := os.Remove(m.unitPath); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(m.unitPath, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(m.unitPath, "child"), nil, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return nil, nil
			}
			err := m.Delete()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if !reflect.DeepEqual(commands, tt.wantCommands) {
				t.Fatalf("commands = %v, want %v", commands, tt.wantCommands)
			}
			_, statErr := os.Lstat(m.unitPath)
			if tt.unitRemoved != os.IsNotExist(statErr) {
				t.Fatalf("unit removal state: %v, want removed = %v", statErr, tt.unitRemoved)
			}
			if strings.Contains(output.String(), "已停止、禁用并删除") {
				t.Fatalf("must not report success on failure: %s", output)
			}
		})
	}
}

func TestUnitQuotesSpecialCharacters(t *testing.T) {
	unit := unitContent(`/opt/hosts tools/hosts`, `https://example.test/a%20b?value=$HOME&quote="&slash=\`, 17)
	if !strings.Contains(unit, `ExecStart="/opt/hosts tools/hosts" service --interval 17 -- "https://example.test/a%%20b?value=$$HOME&quote=\"&slash=\\"`) {
		t.Fatalf("incorrect systemd argument quoting: %s", unit)
	}
}

func TestGeneratedUnitPassesSystemdVerification(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd verification requires Linux")
	}
	analyzer, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze is not installed")
	}
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "hosts tools", "hosts")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, unitName)
	unit := unitContent(binaryPath, `https://example.test/a%20b?value=$HOME&quote="&slash=\`, 17)
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(analyzer, "verify", "--man=no", path).CombinedOutput(); err != nil {
		t.Fatalf("systemd rejected generated unit: %v\n%s", err, output)
	}
}
