package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHelp(t *testing.T) {
	command := NewCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--help"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"--target", "--password", "--cron", "--help", "--add-systemd", "--del-systemd", "六字段", "Linux/macOS", "启动时立即打包一次"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("help missing %q: %s", text, &output)
		}
	}
}

type cancelOnArchive struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelOnArchive) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if strings.Contains(w.Buffer.String(), "打包完成:") {
		w.cancel()
	}
	return n, err
}

func TestCommandArchivesImmediately(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("requires Linux/macOS")
	}
	for _, withCron := range []bool{false, true} {
		t.Run(fmt.Sprint(withCron), func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
			if err := os.Mkdir(source, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "hello.txt"), []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			output := &cancelOnArchive{cancel: cancel}
			var errors bytes.Buffer
			command := NewCommand()
			command.SetOut(output)
			command.SetErr(&errors)
			args := []string{source, "-t", target}
			if withCron {
				args = append(args, "--cron", time.Now().Add(time.Hour).Format("5 4 15")+" * * *")
			}
			command.SetArgs(args)
			if err := command.ExecuteContext(ctx); err != nil {
				t.Fatal(err)
			}
			files, err := os.ReadDir(target)
			if err != nil || len(files) != 1 || !strings.HasSuffix(files[0].Name(), ".zip") || !strings.Contains(output.String(), "打包完成:") {
				t.Fatalf("expected one startup archive: files=%v err=%v output=%s errors=%s", files, err, output, &errors)
			}
		})
	}
}

type fakeServices struct {
	added, deleted string
	args           []string
	err            error
}

func (f *fakeServices) Add(_ context.Context, name string, args []string) error {
	f.added, f.args = name, args
	return f.err
}

func (f *fakeServices) Delete(_ context.Context, name string) error {
	f.deleted = name
	return f.err
}

func TestAddSystemdOnlyInstalls(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only feature")
	}
	root := t.TempDir()
	source, target, password := filepath.Join(root, "中文目录"), filepath.Join(root, "备份"), filepath.Join(root, "密码.txt")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(password, []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative := func(path string) string {
		t.Helper()
		path, err := filepath.Rel(workingDir, path)
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, withCron := range []bool{true, false} {
		t.Run(fmt.Sprint(withCron), func(t *testing.T) {
			services := &fakeServices{}
			command := newCommand(services)
			var output bytes.Buffer
			command.SetOut(&output)
			args := []string{relative(source), "-t", relative(target), "-p", relative(password), "--add-systemd", "note-backup"}
			want := []string{source, "--target", target, "--password", password}
			if withCron {
				args = append(args, "--cron", "* * * * * *")
				want = append(want, "--cron", "* * * * * *")
			}
			command.SetArgs(args)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if services.added != "note-backup" || services.deleted != "" || !reflect.DeepEqual(services.args, want) {
				t.Fatalf("unexpected service command: %+v; want %v", services, want)
			}
			files, err := os.ReadDir(target)
			if err != nil || len(files) != 0 || strings.Contains(output.String(), "下次打包") {
				t.Fatalf("installer started archive scheduling: %v, %v, %s", files, err, &output)
			}
		})
	}
}

func TestDeleteSystemdDoesNotRequireArchiveArguments(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only feature")
	}
	services := &fakeServices{}
	command := newCommand(services)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--del-systemd", "note-backup.service"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if services.deleted != "note-backup.service" || services.added != "" {
		t.Fatalf("unexpected deletion: %+v", services)
	}
}

func TestInvalidSystemdArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--add-systemd", "backup"},
		{"source", "--add-systemd", "backup"},
		{"source", "-t", "target", "--add-systemd", ""},
		{"--del-systemd", ""},
		{"--del-systemd", "../other"},
		{"--add-systemd", "backup", "--del-systemd", "backup"},
		{"source", "--del-systemd", "backup"},
		{"--del-systemd", "backup", "-t", "target"},
		{"--del-systemd", "backup", "-p", "password"},
		{"--del-systemd", "backup", "--cron", "* * * * * *"},
	} {
		services := &fakeServices{}
		command := newCommand(services)
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Errorf("accepted invalid management args: %v", args)
		}
		if services.added != "" || services.deleted != "" {
			t.Errorf("invalid args modified services: %+v", services)
		}
	}
}

func TestAddValidatesArchiveConfiguration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only feature")
	}
	root := t.TempDir()
	for _, extra := range [][]string{
		{"--cron", "invalid"},
		{"--password", filepath.Join(root, "missing-password")},
	} {
		services := &fakeServices{}
		command := newCommand(services)
		args := []string{root, "-t", filepath.Join(root, "target"), "--add-systemd", "backup"}
		command.SetArgs(append(args, extra...))
		if err := command.Execute(); err == nil || services.added != "" {
			t.Fatalf("invalid configuration installed service: %v, %+v", err, services)
		}
	}
}

func TestServiceErrorsPropagate(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only feature")
	}
	services := &fakeServices{err: fmt.Errorf("systemctl unavailable")}
	command := newCommand(services)
	command.SetArgs([]string{"--del-systemd", "backup"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "systemctl unavailable") {
		t.Fatalf("lost service error: %v", err)
	}
}

func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{}, {"source"}, {"source", "extra", "-t", "target"},
		{"source", "-t", ""}, {"source", "-t", "target", "--cron", ""},
		{"source", "-t", "target", "--cron", "* * * * *"},
		{"source", "-t", "target", "--unknown"},
	} {
		command := NewCommand()
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Errorf("accepted invalid args: %v", args)
		}
	}
}

func TestInvalidCronDoesNotCreateTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	command := NewCommand()
	command.SetArgs([]string{root, "-t", target, "--cron", "not valid"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected invalid cron")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("invalid configuration created target: %v", err)
	}
}

func TestCancelledCommandDoesNotArchiveImmediately(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := NewCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{source, "-t", target})
	if err := command.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(target)
	if err != nil || len(files) != 0 {
		t.Fatalf("unexpected startup archive: %v, %v", files, err)
	}
	if !strings.Contains(output.String(), "下次打包") {
		t.Fatalf("missing schedule output: %s", &output)
	}
}
