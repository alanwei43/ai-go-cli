package zipper

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func archiveContents(t *testing.T, path string) map[string]string {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := make(map[string]string)
	for _, file := range archive.File {
		if file.Method != zip.Store {
			t.Fatalf("expected Store entry: %s", file.Name)
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
	return entries
}

func TestListArchivesFilesAndDirectories(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	list := filepath.Join(root, "lists", "备份.TXT")
	source := filepath.Join(root, "data[*]?")
	target := filepath.Join(source, "archives[*]?")
	writeFile(t, filepath.Join(source, "hello.txt"), "hello")
	writeFile(t, filepath.Join(source, ".hidden"), "hidden")
	writeFile(t, filepath.Join(target, "old.zip"), "old archive")
	writeFile(t, filepath.Join(root, "other", "hello.txt"), "other")
	writeFile(t, filepath.Join(root, "-中文 空格.txt"), "spaces")
	if err := os.Mkdir(filepath.Join(source, "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	// BOM, CRLF, blank lines, mixed absolute/relative paths and no final newline.
	writeFile(t, list, "\uFEFF../data[*]?\r\n\r\n"+filepath.Join(root, "other", "hello.txt")+"\r\n../-中文 空格.txt\n../link")
	a, err := New(list, target, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	path, err := a.Create(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	assertArchiveName(t, path, "备份", now)
	entries := archiveContents(t, path)
	for name, want := range map[string]string{
		"data[*]?/hello.txt": "hello", "data[*]?/.hidden": "hidden",
		"other/hello.txt": "other", "-中文 空格.txt": "spaces", "link": "missing",
		"data[*]?/empty/": "",
	} {
		if got, ok := entries[name]; !ok || got != want {
			t.Errorf("entry %q: got %q (present=%t), want %q", name, got, ok, want)
		}
	}
	for name := range entries {
		if strings.Contains(name, "archives") || strings.Contains(name, "备份.TXT") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") {
			t.Errorf("unexpected archive entry: %s", name)
		}
	}
}

func TestListReloadAndFailurePreserveArchives(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	list, target := filepath.Join(root, "backup.txt"), filepath.Join(root, "archives")
	writeFile(t, filepath.Join(root, "first.txt"), "first")
	writeFile(t, filepath.Join(root, "second.txt"), "second")
	writeFile(t, list, "first.txt")
	a, err := New(list, target, "", "custom", 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first, err := a.Create(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, list, "second.txt")
	second, err := a.Create(context.Background(), now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	assertArchiveName(t, second, "custom", now.Add(time.Second))
	entries := archiveContents(t, second)
	if len(entries) != 1 || entries["second.txt"] != "second" {
		t.Fatalf("updated list was not used: %v", entries)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("retention did not remove first archive: %v", err)
	}
	for _, content := range []string{"first.txt\nmissing", "\n", "archives"} {
		writeFile(t, list, content)
		if _, err := a.Create(context.Background(), now.Add(2*time.Second)); err == nil {
			t.Fatalf("accepted invalid list: %q", content)
		}
		files, err := os.ReadDir(target)
		if err != nil || len(files) != 1 || files[0].Name() != filepath.Base(second) {
			t.Fatalf("failed list changed archives or left temporary files: %v, %v", files, err)
		}
	}
}

func TestListValidation(t *testing.T) {
	requireZip(t)
	for _, test := range []struct{ name, filename, content, want string }{
		{"non txt", "backup.csv", "file.txt", "目录或 .txt"},
		{"empty", "backup.txt", "\uFEFF\r\n  \n", "清单不能为空"},
		{"missing entry", "backup.txt", "file.txt\nmissing", "第 2 行"},
		{"missing parent", "backup.txt", "missing/file", "第 1 行"},
		{"nul", "backup.txt", "bad\x00path", "第 1 行"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "file.txt"), "hello")
			list, target := filepath.Join(root, test.filename), filepath.Join(root, "archives")
			writeFile(t, list, test.content)
			if _, err := New(list, target, "", "", 0); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("invalid source created target: %v", err)
			}
		})
	}
}
