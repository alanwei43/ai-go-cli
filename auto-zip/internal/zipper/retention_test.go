package zipper

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveRetention(t *testing.T) {
	requireZip(t)
	for _, prefix := range []string{"", "rustfs_s3_daily", "备份_每日_[*]?"} {
		for _, limit := range []int{0, 1, 2, 3, 5} {
			t.Run(fmt.Sprintf("prefix=%s/limit=%d", prefix, limit), func(t *testing.T) {
				root := t.TempDir()
				source, target := filepath.Join(root, "note"), filepath.Join(root, "archives")
				writeFile(t, filepath.Join(source, "hello.txt"), "hello")
				name := prefix
				if name == "" {
					name = "note"
				}
				base := time.Date(2026, 3, 21, 10, 0, 0, 0, time.Local)
				paths := make([]string, 5)
				for i := 0; i < 3; i++ {
					stamp := base.Add(time.Duration(i) * time.Hour)
					filename := fmt.Sprintf("%s_%s_%x.zip", name, stamp.Format(timestampLayout), sha256.Sum256([]byte("old archive")))
					paths[i] = filepath.Join(target, filename)
					writeFile(t, paths[i], "old archive")
					// Modification times deliberately disagree with filename order.
					modified := base.Add(time.Duration(3-i) * time.Hour)
					if err := os.Chtimes(paths[i], modified, modified); err != nil {
						t.Fatal(err)
					}
				}
				untouched := []string{
					"other_2020-01-01-00-00-00.zip", name + "_extra_2020-01-01-00-00-00.zip",
					name + "_2020-01-01-00-00-00.zip", name + "_2026-03-21-13-00-00.zip",
					name + "_2020-02-30-00-00-00.zip", name + "_2020-1-01-00-00-00.zip",
					name + "_manual.zip", "notes.txt",
					name + "_20200101000000.zip", name + "_20200101000000_bad-hash.zip",
					name + "_20200230000000_" + strings.Repeat("a", 64) + ".zip",
					name + "_20200101000000_" + strings.Repeat("g", 64) + ".zip",
					name + "_20200101000000_" + strings.Repeat("A", 64) + ".zip",
					"other_20200101000000_" + strings.Repeat("a", 64) + ".zip",
					name + "extra_20200101000000_" + strings.Repeat("a", 64) + ".zip",
					name + "_extra_20200101000000_" + strings.Repeat("a", 64) + ".zip",
					name + "_20200101000000_" + strings.Repeat("a", 63) + ".zip",
					name + "_20200101000000_" + strings.Repeat("a", 65) + ".zip",
					name + "_20200101000000_" + strings.Repeat("a", 64) + ".zip.bak",
					name + "_20200101000000_" + strings.Repeat("a", 64) + "_extra.zip",
					name + "_2020010100000_" + strings.Repeat("a", 64) + ".zip",
					name + "_202001010000000_" + strings.Repeat("a", 64) + ".zip",
				}
				for _, file := range untouched {
					writeFile(t, filepath.Join(target, file), "keep")
				}
				directory := filepath.Join(target, name+"_20200102000000_"+strings.Repeat("a", 64)+".zip")
				writeFile(t, filepath.Join(directory, "keep.txt"), "keep")
				link := filepath.Join(target, name+"_20200103000000_"+strings.Repeat("a", 64)+".zip")
				if err := os.Symlink(paths[2], link); err != nil {
					t.Fatal(err)
				}
				for i := 3; i < 5; i++ {
					// Recreating the archiver models restarting the CLI/service.
					a, err := New(source, target, "", prefix, limit)
					if err != nil {
						t.Fatal(err)
					}
					paths[i], err = a.Create(context.Background(), base.Add(time.Duration(i)*time.Hour))
					if err != nil {
						t.Fatal(err)
					}
					first := 0
					if limit > 0 && i+1 > limit {
						first = i + 1 - limit
					}
					for j := 0; j <= i; j++ {
						_, err := os.Stat(paths[j])
						if (j < first && !os.IsNotExist(err)) || (j >= first && err != nil) {
							t.Fatalf("unexpected retention for archive %d after creation %d: %v", j, i, err)
						}
					}
				}
				for _, file := range append(untouched, filepath.Base(directory), filepath.Base(link)) {
					if _, err := os.Lstat(filepath.Join(target, file)); err != nil {
						t.Fatalf("unrelated entry removed: %s: %v", file, err)
					}
				}
			})
		}
	}
}

func TestRetentionPreservesArchivesOnCollisionAndCancellation(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	source, target := filepath.Join(root, "note"), filepath.Join(root, "archives")
	writeFile(t, filepath.Join(source, "hello.txt"), "hello")
	old := filepath.Join(target, "note_20260320100000_"+strings.Repeat("a", 64)+".zip")
	latest := filepath.Join(target, "note_20260321100000_"+strings.Repeat("a", 64)+".zip")
	writeFile(t, old, "old")
	writeFile(t, latest, "latest")
	a, err := New(source, target, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.Local)
	if _, err := a.Create(context.Background(), now); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected collision: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Create(ctx, now.Add(time.Hour)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
	for path, want := range map[string]string{old: "old", latest: "latest"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("existing archive changed: %s: %q, %v", path, data, err)
		}
	}
}

func TestRetentionSkipsFailedArchive(t *testing.T) {
	requireZip(t)
	for _, test := range []struct {
		name, script, wantError string
		timeout                 bool
	}{
		{"packing", "#!/bin/sh\nexit 18\n", "系统 zip 执行失败", false},
		{"hashing", "#!/bin/sh\nexit 0\n", "SHA-256", false},
		{"cancellation", "#!/bin/sh\nwhile :; do :; done\n", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "note"), filepath.Join(root, "archives")
			writeFile(t, filepath.Join(source, "hello.txt"), "hello")
			old := filepath.Join(target, "note_20260320100000_"+strings.Repeat("a", 64)+".zip")
			writeFile(t, old, "old")
			a, err := New(source, target, "", "", 1)
			if err != nil {
				t.Fatal(err)
			}
			broken := filepath.Join(root, "broken-zip")
			writeFile(t, broken, test.script)
			if err := os.Chmod(broken, 0700); err != nil {
				t.Fatal(err)
			}
			a.executable = broken
			ctx := context.Background()
			if test.timeout {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			_, err = a.Create(ctx, time.Now())
			if err == nil || !strings.Contains(err.Error(), test.wantError) || (test.timeout && !errors.Is(err, context.DeadlineExceeded)) {
				t.Fatalf("unexpected archive error: %v", err)
			}
			if data, err := os.ReadFile(old); err != nil || string(data) != "old" {
				t.Fatalf("failed archive changed historical ZIP: %q, %v", data, err)
			}
			if files, err := os.ReadDir(target); err != nil || len(files) != 1 {
				t.Fatalf("unexpected output after failure: %v, %v", files, err)
			}
		})
	}
}

func TestRetentionRunsAfterPackingAndPublishing(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	source, target := filepath.Join(root, "note"), filepath.Join(root, "archives")
	writeFile(t, filepath.Join(source, "hello.txt"), "hello")
	old := filepath.Join(target, "note_20260320100000_"+strings.Repeat("a", 64)+".zip")
	writeFile(t, old, "old")
	a, err := New(source, target, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTO_ZIP_TEST_OLD", old)
	t.Setenv("AUTO_ZIP_TEST_REAL_ZIP", a.executable)
	wrapper := filepath.Join(root, "checked-zip")
	writeFile(t, wrapper, "#!/bin/sh\n[ -f \"$AUTO_ZIP_TEST_OLD\" ] || exit 99\nexec \"$AUTO_ZIP_TEST_REAL_ZIP\" \"$@\"\n")
	if err := os.Chmod(wrapper, 0700); err != nil {
		t.Fatal(err)
	}
	a.executable = wrapper
	now := time.Now()
	path, err := a.Create(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	assertArchiveName(t, path, "note", now)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old archive was not removed after successful packing: %v", err)
	}
	if files, err := os.ReadDir(target); err != nil || len(files) != 1 || files[0].Name() != filepath.Base(path) {
		t.Fatalf("expected only the completed, hashed archive: %v, %v", files, err)
	}
}

func TestRetentionSkipsFailedPublication(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	source, target := filepath.Join(root, "note"), filepath.Join(root, "archives")
	writeFile(t, filepath.Join(source, "hello.txt"), "hello")
	old := filepath.Join(target, "note_20260320100000_"+strings.Repeat("a", 64)+".zip")
	writeFile(t, old, "old")
	a, err := New(source, target, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 10, 30, 0, 0, time.Local)
	finalPath := filepath.Join(target, fmt.Sprintf("note_%s_%x.zip", now.Format(timestampLayout), sha256.Sum256([]byte("archive"))))
	t.Setenv("AUTO_ZIP_TEST_COLLISION", finalPath)
	broken := filepath.Join(root, "colliding-zip")
	// The fifth argument is the output path for an unencrypted zip invocation.
	// Introduce a collision after the initial timestamp check, before publishing.
	writeFile(t, broken, "#!/bin/sh\nprintf archive > \"$5\"\nmkdir \"$AUTO_ZIP_TEST_COLLISION\"\n")
	if err := os.Chmod(broken, 0700); err != nil {
		t.Fatal(err)
	}
	a.executable = broken
	if _, err := a.Create(context.Background(), now); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected final filename collision: %v", err)
	}
	if data, err := os.ReadFile(old); err != nil || string(data) != "old" {
		t.Fatalf("publication failure changed historical ZIP: %q, %v", data, err)
	}
}

func TestRetentionKeepsNewArchiveWhenClockMovesBack(t *testing.T) {
	requireZip(t)
	root := t.TempDir()
	source, target := filepath.Join(root, "note"), filepath.Join(root, "archives")
	writeFile(t, filepath.Join(source, "hello.txt"), "hello")
	old := filepath.Join(target, "note_20261016100000_"+strings.Repeat("a", 64)+".zip")
	writeFile(t, old, "old")
	a, err := New(source, target, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 10, 30, 0, 0, time.Local)
	path, err := a.Create(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	assertArchiveName(t, path, "note", now)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("historical archive should be removed instead of the just-created ZIP: %v", err)
	}
}
