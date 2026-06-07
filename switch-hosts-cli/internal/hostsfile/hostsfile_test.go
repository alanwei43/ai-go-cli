package hostsfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceNamedBlockReplacesExistingBlock(t *testing.T) {
	original := "127.0.0.1 localhost\n\n# switch-hosts-cli start beijing\n1.1.1.1 old\n# switch-hosts-cli end beijing\n"

	updated, err := ReplaceNamedBlock(original, "beijing", "192.168.1.124 dellmini\n192.168.1.31 work\n")
	if err != nil {
		t.Fatalf("ReplaceNamedBlock() error = %v", err)
	}

	expected := "127.0.0.1 localhost\n\n# switch-hosts-cli start beijing\n192.168.1.124 dellmini\n192.168.1.31 work\n# switch-hosts-cli end beijing\n"
	if updated != expected {
		t.Fatalf("ReplaceNamedBlock() mismatch\nexpected:\n%s\nactual:\n%s", expected, updated)
	}
}

func TestReplaceNamedBlockAppendsMissingBlock(t *testing.T) {
	original := "127.0.0.1 localhost"

	updated, err := ReplaceNamedBlock(original, "home", "192.168.1.89 lecoo")
	if err != nil {
		t.Fatalf("ReplaceNamedBlock() error = %v", err)
	}

	expected := "127.0.0.1 localhost\n\n# switch-hosts-cli start home\n192.168.1.89 lecoo\n# switch-hosts-cli end home\n"
	if updated != expected {
		t.Fatalf("ReplaceNamedBlock() mismatch\nexpected:\n%s\nactual:\n%s", expected, updated)
	}
}

func TestReplaceNamedBlockRejectsInvalidMarkers(t *testing.T) {
	original := "127.0.0.1 localhost\n# switch-hosts-cli end beijing\n"

	_, err := ReplaceNamedBlock(original, "beijing", "192.168.1.124 dellmini")
	if err == nil {
		t.Fatal("ReplaceNamedBlock() expected error, got nil")
	}
}

func TestUpdateNamedBlockCreatesBackupBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	original := "127.0.0.1 localhost\n"

	if err := os.WriteFile(hostsPath, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := UpdateNamedBlock(hostsPath, "home", "192.168.1.89 lecoo"); err != nil {
		t.Fatalf("UpdateNamedBlock() error = %v", err)
	}

	backupPath := backupPathForContent(hostsPath, []byte(original))
	if filepath.Dir(backupPath) != filepath.Join(dir, "hosts_backup") {
		t.Fatalf("backup dir = %q, want %q", filepath.Dir(backupPath), filepath.Join(dir, "hosts_backup"))
	}

	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile(backup) error = %v", err)
	}

	if string(backupContent) != original {
		t.Fatalf("backup content = %q, want %q", string(backupContent), original)
	}
}
