package zipper

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func requireZip(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("requires Linux/macOS")
	}
	if _, err := exec.LookPath("zip"); err != nil {
		t.Fatal("integration tests require system zip:", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCreateStoreArchive(t *testing.T) {
	requireZip(t)
	for _, name := range []string{"note", "中文 空格", "-note", "note[*]?", `note\name`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, name), filepath.Join(root, "archives")
			writeFile(t, filepath.Join(source, "sub", "hello.txt"), "hello\n")
			writeFile(t, filepath.Join(source, ".hidden"), "hidden")
			if err := os.Mkdir(filepath.Join(source, "empty"), 0755); err != nil {
				t.Fatal(err)
			}
			// Environment settings must not turn Store into compression.
			t.Setenv("ZIPOPT", "-9")
			a, err := New(source, target, "", "")
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.Local)
			path, err := a.Create(context.Background(), now)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(path) != name+"_2026-03-21-10-00-00.zip" {
				t.Fatalf("unexpected filename: %s", path)
			}
			archive, err := zip.OpenReader(path)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			entries := make(map[string]string)
			for _, file := range archive.File {
				if file.Method != zip.Store || file.Flags&1 != 0 {
					t.Fatalf("expected unencrypted Store entry: %+v", file.FileHeader)
				}
				reader, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(reader)
				reader.Close()
				if err != nil {
					t.Fatal(err)
				}
				entries[file.Name] = string(data)
			}
			if entries[name+"/sub/hello.txt"] != "hello\n" || entries[name+"/.hidden"] != "hidden" {
				t.Fatalf("missing file contents: %v", entries)
			}
			if _, ok := entries[name+"/empty/"]; !ok {
				t.Fatalf("missing empty directory: %v", entries)
			}
			if _, err := a.Create(context.Background(), now); !errors.Is(err, os.ErrExist) {
				t.Fatalf("expected filename collision: %v", err)
			}
			files, err := os.ReadDir(target)
			if err != nil || len(files) != 1 {
				t.Fatalf("temporary files left behind: %v, %v", files, err)
			}
		})
	}
}

func TestCustomZipNamePreservesArchiveContents(t *testing.T) {
	requireZip(t)
	for _, prefix := range []string{"rustfs-s3", `备份 数据 $name %i "引号"`} {
		t.Run(prefix, func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "data"), filepath.Join(root, "archives")
			writeFile(t, filepath.Join(source, "hello.txt"), "hello")
			a, err := New(source, target, "", prefix)
			if err != nil {
				t.Fatal(err)
			}
			path, err := a.Create(context.Background(), time.Date(2026, 3, 21, 10, 0, 0, 0, time.Local))
			if err != nil {
				t.Fatal(err)
			}
			if path != filepath.Join(target, prefix+"_2026-03-21-10-00-00.zip") {
				t.Fatalf("unexpected custom filename: %s", path)
			}
			archive, err := zip.OpenReader(path)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			for _, file := range archive.File {
				if file.Name == "data/hello.txt" {
					reader, err := file.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(reader)
					reader.Close()
					if err != nil || string(data) != "hello" {
						t.Fatalf("unexpected archive contents: %q, %v", data, err)
					}
					return
				}
			}
			t.Fatal("custom prefix changed source directory name inside ZIP")
		})
	}
}

func TestPasswordCompatibleWithUnzip(t *testing.T) {
	requireZip(t)
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Fatal("integration test requires unzip:", err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "note")
	writeFile(t, filepath.Join(source, "hello.txt"), "secret content\n")
	password := " secret $`'\"[] password "
	passwordFile := filepath.Join(root, "password.txt")
	writeFile(t, passwordFile, password+"\r\n")
	a, err := New(source, filepath.Join(root, "archives"), passwordFile, "")
	if err != nil {
		t.Fatal(err)
	}
	path, err := a.Create(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	for _, file := range archive.File {
		if file.Method != zip.Store || (!file.FileInfo().IsDir() && file.Flags&1 == 0) {
			t.Fatalf("expected encrypted Store entry: %+v", file.FileHeader)
		}
	}
	data, err := exec.Command("unzip", "-P", password, "-p", path, "note/hello.txt").CombinedOutput()
	if err != nil || string(data) != "secret content\n" {
		t.Fatalf("unzip failed: %v, %s", err, data)
	}
	if err := exec.Command("unzip", "-P", "wrong", "-t", path).Run(); err == nil {
		t.Fatal("wrong password unexpectedly succeeded")
	}
}

func TestNestedTargetExcluded(t *testing.T) {
	requireZip(t)
	for _, name := range []string{"archives", "archives[*]?", `archives\name`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "note[*]?")
			target := filepath.Join(source, name)
			writeFile(t, filepath.Join(source, "hello.txt"), "hello")
			writeFile(t, filepath.Join(target, "old.zip"), "old archive")
			// Resolve an external symlink to a target inside the source.
			alias := filepath.Join(root, "target-link")
			if err := os.Symlink(target, alias); err != nil {
				t.Fatal(err)
			}
			a, err := New(source, alias, "", "")
			if err != nil {
				t.Fatal(err)
			}
			path, err := a.Create(context.Background(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			archive, err := zip.OpenReader(path)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			found := false
			for _, file := range archive.File {
				if strings.HasPrefix(file.Name, "note[*]?/"+name+"/") {
					t.Fatalf("target included in archive: %s", file.Name)
				}
				if file.Name == "note[*]?/hello.txt" {
					found = true
				}
			}
			if !found {
				t.Fatal("source file missing")
			}
		})
	}
}

func TestSymlinksAreStoredWithoutFollowing(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	source := filepath.Join(root, "note")
	writeFile(t, filepath.Join(source, "hello.txt"), "hello")
	writeFile(t, filepath.Join(root, "outside", "secret.txt"), "outside")
	for name, target := range map[string]string{"outside": "../outside", "loop": ".", "broken": "missing"} {
		if err := os.Symlink(target, filepath.Join(source, name)); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(source, filepath.Join(root, "archives"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	path, err := a.Create(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	links := 0
	for _, file := range archive.File {
		if file.Mode()&os.ModeSymlink != 0 {
			links++
		}
		if strings.Contains(file.Name, "secret.txt") {
			t.Fatal("followed a symlink outside the source")
		}
	}
	if links != 3 {
		t.Fatalf("expected 3 symlinks, got %d", links)
	}
}

func TestEmptyDirectory(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	source := filepath.Join(root, "empty")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	a, err := New(source, filepath.Join(root, "archives"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	path, err := a.Create(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "empty/" {
		t.Fatalf("unexpected empty archive: %v", archive.File)
	}
}

func TestValidation(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	source, target := filepath.Join(root, "note"), filepath.Join(root, "archives")
	writeFile(t, filepath.Join(source, "hello.txt"), "hello")
	file := filepath.Join(root, "file")
	writeFile(t, file, "file")
	emptyPassword, nulPassword := filepath.Join(root, "empty-password"), filepath.Join(root, "nul-password")
	writeFile(t, emptyPassword, "\r\n")
	writeFile(t, nulPassword, "a\x00b")
	for _, test := range []struct{ name, source, target, password string }{
		{"empty source", "", target, ""},
		{"missing source", filepath.Join(root, "missing"), target, ""},
		{"file source", file, target, ""},
		{"empty target", source, "", ""},
		{"file target", source, file, ""},
		{"same target", source, source, ""},
		{"missing password", source, target, filepath.Join(root, "missing")},
		{"empty password", source, target, emptyPassword},
		{"nul password", source, target, nulPassword},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.source, test.target, test.password, ""); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := New(source, target, "", ""); err == nil || !strings.Contains(err.Error(), "zip") {
		t.Fatalf("expected missing zip error: %v", err)
	}
}

func TestFailureAndCancellationCleanUp(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	source, target := filepath.Join(root, "note"), filepath.Join(root, "archives")
	writeFile(t, filepath.Join(source, "hello.txt"), "hello")
	a, err := New(source, target, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// A failing system zip must not publish a partial archive.
	broken := filepath.Join(root, "broken-zip")
	writeFile(t, broken, "#!/bin/sh\necho simulated failure >&2\nexit 18\n")
	if err := os.Chmod(broken, 0700); err != nil {
		t.Fatal(err)
	}
	a.executable = broken
	if _, err := a.Create(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "simulated failure") {
		t.Fatalf("expected zip failure: %v", err)
	}
	// This shell does not spawn children; CommandContext must kill the running job.
	writeFile(t, broken, "#!/bin/sh\nwhile :; do :; done\n")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := a.Create(ctx, time.Now()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation: %v", err)
	}
	files, err := os.ReadDir(target)
	if err != nil || len(files) != 0 {
		t.Fatalf("partial archives left behind: %v, %v", files, err)
	}
}
