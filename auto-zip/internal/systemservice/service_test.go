package systemservice

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type commandCall struct {
	args []string
}

func testManager(t *testing.T) (*Manager, *[]commandCall) {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "auto-zip")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	calls := []commandCall{}
	m := NewManager()
	m.platform, m.directory = "linux", filepath.Join(root, "systemd")
	m.geteuid = func() int { return 0 }
	m.executable = func() (string, error) { return binary, nil }
	m.run = func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, commandCall{append([]string(nil), args...)})
		return nil, nil
	}
	return m, &calls
}

func TestUnitNames(t *testing.T) {
	for input, want := range map[string]string{"backup": "backup.service", "backup.service": "backup.service", "Note_backup-1.2": "Note_backup-1.2.service"} {
		got, err := UnitName(input)
		if err != nil || got != want {
			t.Errorf("UnitName(%q) = %q, %v", input, got, err)
		}
	}
	for _, name := range []string{"", ".", "..", "../backup", "/tmp/unit", "-backup", "bad name", "中文", "backup@", "bad\nname", strings.Repeat("a", 256)} {
		if _, err := UnitName(name); err == nil {
			t.Errorf("accepted invalid name: %q", name)
		}
	}
}

func TestInstallAndDelete(t *testing.T) {
	m, calls := testManager(t)
	args := []string{"/data/中文 目录", "--target", "/backup/archives", "--password", "/data/password.txt", "--cron", "0 0 10 * * ?"}
	if err := m.Add(context.Background(), "note-backup", args); err != nil {
		t.Fatal(err)
	}
	unit := "note-backup.service"
	path := filepath.Join(m.directory, unit)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{ownershipMarker, "Type=exec\n", "Restart=on-failure\n", "RestartSec=1h\n", `"/data/中文 目录" "--target" "/backup/archives"`, `"--cron" "0 0 10 * * ?"`, "WantedBy=multi-user.target\n"} {
		if !strings.Contains(string(content), required) {
			t.Errorf("unit missing %q: %s", required, content)
		}
	}
	if strings.Contains(string(content), "--add-systemd") || strings.Contains(string(content), "--del-systemd") {
		t.Fatal("recursive management command in unit")
	}
	want := []commandCall{
		{[]string{"list-unit-files", "--type=service", "--full", "--no-legend", "--no-pager"}},
		{[]string{"daemon-reload"}},
		{[]string{"enable", "--now", "--", unit}},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("install commands = %v; want %v", *calls, want)
	}
	dropIn := path + ".d"
	if err := os.Mkdir(dropIn, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropIn, "override.conf"), []byte("[Service]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Both the main file and related configuration must still exist when disable runs.
	m.run = func(_ context.Context, args ...string) ([]byte, error) {
		*calls = append(*calls, commandCall{append([]string(nil), args...)})
		if args[0] == "disable" {
			if _, err := os.Stat(path); err != nil {
				t.Fatal("service removed before disable:", err)
			}
			if _, err := os.Stat(dropIn); err != nil {
				t.Fatal("drop-ins removed before disable:", err)
			}
		}
		return nil, nil
	}
	if err := m.Delete(context.Background(), unit); err != nil {
		t.Fatal(err)
	}
	want = append(want, commandCall{[]string{"disable", "--now", "--", unit}}, commandCall{[]string{"daemon-reload"}})
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("delete commands = %v; want %v", *calls, want)
	}
	for _, removed := range []string{path, dropIn} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("file not removed: %s: %v", removed, err)
		}
	}
	if err := m.Delete(context.Background(), "note-backup"); err != nil || !reflect.DeepEqual(*calls, want) {
		t.Fatalf("repeated delete was not a no-op: %v; %v", err, *calls)
	}
}

func TestPlatformAndPermission(t *testing.T) {
	for _, test := range []struct {
		platform string
		uid      int
	}{{"darwin", 0}, {"windows", 0}, {"linux", 1000}} {
		m, calls := testManager(t)
		m.platform, m.geteuid = test.platform, func() int { return test.uid }
		if err := m.Add(context.Background(), "backup", nil); err == nil {
			t.Error("unauthorized installation succeeded")
		}
		if err := m.Delete(context.Background(), "backup"); err == nil {
			t.Error("unauthorized deletion succeeded")
		}
		if len(*calls) != 0 {
			t.Fatal("ran systemctl before permission/platform check")
		}
	}
}

func TestExistingServicesAreProtected(t *testing.T) {
	for _, vendor := range []bool{false, true} {
		m, calls := testManager(t)
		path := filepath.Join(m.directory, "backup.service")
		if vendor {
			m.run = func(_ context.Context, args ...string) ([]byte, error) {
				*calls = append(*calls, commandCall{args})
				return []byte("backup.service enabled\n"), nil
			}
		} else {
			if err := os.MkdirAll(m.directory, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("existing unit"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.Add(context.Background(), "backup", nil); err == nil {
			t.Fatal("replaced an existing service")
		}
		if !vendor {
			content, _ := os.ReadFile(path)
			if string(content) != "existing unit" {
				t.Fatal("modified existing service")
			}
			if err := m.Delete(context.Background(), "backup"); err == nil {
				t.Fatal("deleted a service without the ownership marker")
			}
		}
		for _, call := range *calls {
			if call.args[0] != "list-unit-files" {
				t.Fatal("modified existing service state:", *calls)
			}
		}
	}
}

func TestServiceInventoryMatchesExactName(t *testing.T) {
	for _, test := range []struct {
		name    string
		output  string
		present bool
	}{
		{"unrelated", "other.service enabled enabled\n", false},
		{"similar", "backup.service-extra.service disabled enabled\nold-backup.service static -\n", false},
		{"existing", "other.service enabled enabled\nbackup.service disabled enabled\n", true},
		{"masked", "backup.service masked enabled\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, calls := testManager(t)
			m.run = func(_ context.Context, args ...string) ([]byte, error) {
				*calls = append(*calls, commandCall{append([]string(nil), args...)})
				if args[0] == "list-unit-files" {
					return []byte(test.output), nil
				}
				return nil, nil
			}
			err := m.Add(context.Background(), "backup", nil)
			if test.present {
				if err == nil || !strings.Contains(err.Error(), "systemd 已存在同名服务") {
					t.Fatalf("existing service was not protected: %v", err)
				}
				if len(*calls) != 1 {
					t.Fatalf("modified existing service: %v", *calls)
				}
				if _, err := os.Stat(filepath.Join(m.directory, "backup.service")); !os.IsNotExist(err) {
					t.Fatalf("created unit despite existing service: %v", err)
				}
			} else if err != nil {
				t.Fatalf("unrelated service blocked installation: %v", err)
			}
		})
	}
}

func TestAddWithSystemctlInventory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires systemctl on Linux")
	}
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		t.Skip("systemctl not installed")
	}
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprintf("present=%t", present), func(t *testing.T) {
			m, calls := testManager(t)
			root := t.TempDir()
			vendor := filepath.Join(root, "usr/lib/systemd/system")
			if err := os.MkdirAll(vendor, 0755); err != nil {
				t.Fatal(err)
			}
			name := "autozip-rustfs-s3.service"
			listed := name + "-other.service"
			if present {
				listed = name
			}
			if err := os.WriteFile(filepath.Join(vendor, listed), []byte("[Service]\nExecStart=/bin/true\n"), 0644); err != nil {
				t.Fatal(err)
			}
			m.run = func(ctx context.Context, args ...string) ([]byte, error) {
				*calls = append(*calls, commandCall{append([]string(nil), args...)})
				if args[0] == "list-unit-files" {
					// Query real systemctl against an isolated vendor inventory;
					// daemon-reload and enable never touch the host system.
					return exec.CommandContext(ctx, systemctl, append([]string{"--root", root}, args...)...).CombinedOutput()
				}
				return nil, nil
			}
			err := m.Add(context.Background(), name, nil)
			if present {
				if err == nil || !strings.Contains(err.Error(), "systemd 已存在同名服务") || len(*calls) != 1 {
					t.Fatalf("vendor service was not protected: %v; %v", err, *calls)
				}
			} else if err != nil {
				t.Fatalf("installation of absent service failed: %v", err)
			} else if _, err := os.Stat(filepath.Join(m.directory, name)); err != nil {
				t.Fatalf("service file not created: %v", err)
			}
		})
	}
}

func TestSystemctlFailures(t *testing.T) {
	for _, failure := range []string{"list-unit-files", "daemon-reload", "enable", "disable"} {
		t.Run(failure, func(t *testing.T) {
			m, calls := testManager(t)
			if failure == "disable" {
				if err := m.Add(context.Background(), "backup", nil); err != nil {
					t.Fatal(err)
				}
			}
			m.run = func(_ context.Context, args ...string) ([]byte, error) {
				*calls = append(*calls, commandCall{args})
				if args[0] == failure {
					return []byte("simulated systemctl error"), fmt.Errorf("exit status 1")
				}
				return nil, nil
			}
			var err error
			if failure == "disable" {
				err = m.Delete(context.Background(), "backup")
			} else {
				err = m.Add(context.Background(), "backup", nil)
			}
			if err == nil || !strings.Contains(err.Error(), "simulated systemctl error") {
				t.Fatalf("lost systemctl failure: %v", err)
			}
			path := filepath.Join(m.directory, "backup.service")
			if failure == "list-unit-files" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("unit created despite failed collision check")
				}
			} else if _, err := os.Stat(path); err != nil {
				t.Fatal("unit lost after failed systemctl operation:", err)
			}
			if last := (*calls)[len(*calls)-1].args[0]; last != failure {
				t.Fatalf("continued modifying state after failure: %v", *calls)
			}
		})
	}
}

func TestUnitQuotingAndSystemdVerification(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires systemd parser on Linux")
	}
	root := t.TempDir()
	binary := filepath.Join(root, `auto-zip 中文 $name %i`)
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	args := []string{`/data/中文 "quotes" $HOME %i \path`, "--target", "/backup/target", "--password", "/data/pass\tword\nfile", "--cron", "0 0 10 * * ?"}
	content, err := unitContent(binary, args)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`ExecStart=:`, `$HOME %%i`, `\"quotes\"`, `\\path`, `pass\x09word\x0afile`} {
		if !strings.Contains(content, want) {
			t.Errorf("incorrect argument encoding, missing %q: %s", want, content)
		}
	}
	if strings.Contains(content, "--add-systemd") {
		t.Fatal("unexpected management flag")
	}
	verifier, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze not installed; string escaping verified")
	}
	unit := filepath.Join(root, "auto-zip-verify.service")
	if err := os.WriteFile(unit, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(verifier, "verify", "--generators=no", unit)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("systemd rejected generated unit: %v\n%s\n%s", err, output, content)
	}
}

func TestInvalidUnitPathsAndNUL(t *testing.T) {
	for _, test := range []struct {
		binary string
		args   []string
	}{
		{"relative", nil},
		{"/bin/auto'zip", nil},
		{"/bin/auto\"zip", nil},
		{"/bin/auto\\zip", nil},
		{"/bin/auto\nzip", nil},
		{"/bin/auto-zip", []string{"a\x00b"}},
	} {
		if _, err := unitContent(test.binary, test.args); err == nil {
			t.Fatal("accepted invalid unit arguments")
		}
	}
}
