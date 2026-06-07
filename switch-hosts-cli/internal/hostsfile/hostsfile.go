package hostsfile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func SystemHostsPath() string {
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		return filepath.Join(systemRoot, "System32", "drivers", "etc", "hosts")
	}

	return "/etc/hosts"
}

func UpdateNamedBlock(path string, name string, block string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read hosts file: %w", err)
	}

	updated, err := ReplaceNamedBlock(string(original), name, block)
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat hosts file: %w", err)
	}

	if err := writeBackup(path, original, info.Mode().Perm()); err != nil {
		return err
	}

	if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
		return fmt.Errorf("write hosts file: %w", err)
	}

	return nil
}

func ReplaceNamedBlock(content string, name string, block string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("block name is required")
	}

	startMarker := markerStart(name)
	endMarker := markerEnd(name)
	normalizedBlock := normalizeBlock(block)
	replacement := startMarker + "\n" + normalizedBlock + "\n" + endMarker

	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)
	if start >= 0 || end >= 0 {
		if start < 0 || end < 0 || end < start {
			return "", fmt.Errorf("invalid hosts block markers for %q", name)
		}

		end += len(endMarker)
		return content[:start] + replacement + content[end:], nil
	}

	if content == "" {
		return replacement + "\n", nil
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if !strings.HasSuffix(content, "\n\n") {
		content += "\n"
	}

	return content + replacement + "\n", nil
}

func markerStart(name string) string {
	return "# switch-hosts-cli start " + name
}

func markerEnd(name string) string {
	return "# switch-hosts-cli end " + name
}

func normalizeBlock(block string) string {
	normalized := strings.ReplaceAll(block, "\r\n", "\n")
	normalized = strings.Trim(normalized, "\n")
	return normalized
}

func writeBackup(path string, content []byte, mode os.FileMode) error {
	backupPath := backupPathForContent(path, content)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return fmt.Errorf("create hosts backup dir: %w", err)
	}
	if err := os.WriteFile(backupPath, content, mode); err != nil {
		return fmt.Errorf("write hosts backup: %w", err)
	}
	return nil
}

func backupPathForContent(path string, content []byte) string {
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	filename := filepath.Base(path) + "." + hash
	return filepath.Join(filepath.Dir(path), "hosts_backup", filename)
}
