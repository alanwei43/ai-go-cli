package zipper

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type archivePlan struct {
	workingDir string
	operands   []string
	exclusion  string
}

// Relative paths belong to the list, so they also work when systemd starts in /.
func readList(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("读取 txt 清单: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var paths []string
	for line := 1; ; line++ {
		text, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("读取 txt 清单第 %d 行: %w", line, err)
		}
		if line == 1 {
			text = strings.TrimPrefix(text, "\uFEFF")
		}
		text = strings.TrimSpace(text)
		if text != "" {
			entry := text
			if !filepath.IsAbs(entry) {
				entry = filepath.Join(filepath.Dir(path), entry)
			}
			// Resolve parent aliases while keeping the final symlink as a ZIP entry.
			parent, resolveErr := filepath.EvalSymlinks(filepath.Dir(entry))
			if resolveErr != nil {
				return nil, fmt.Errorf("txt 清单第 %d 行 %q: %w", line, text, resolveErr)
			}
			entry = filepath.Join(parent, filepath.Base(entry))
			info, statErr := os.Lstat(entry)
			if statErr != nil {
				return nil, fmt.Errorf("txt 清单第 %d 行 %q: %w", line, text, statErr)
			}
			if !info.IsDir() && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				return nil, fmt.Errorf("txt 清单第 %d 行 %q 不是普通文件或目录", line, text)
			}
			paths = append(paths, entry)
		}
		if err == io.EOF {
			break
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("txt 清单不能为空，必须包含至少一个文件或目录路径")
	}
	return paths, nil
}

func within(parent, path string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (a *Archiver) plan() (archivePlan, error) {
	paths := []string{a.source}
	if a.list {
		var err error
		paths, err = readList(a.source)
		if err != nil {
			return archivePlan{}, err
		}
	}
	// Use a common parent to retain distinct paths, including duplicate basenames.
	plan := archivePlan{workingDir: filepath.Dir(paths[0])}
	for _, path := range paths[1:] {
		for !within(plan.workingDir, filepath.Dir(path)) {
			plan.workingDir = filepath.Dir(plan.workingDir)
		}
	}
	for _, path := range paths {
		if path == a.target {
			return archivePlan{}, fmt.Errorf("目标目录不能与源目录相同: %s", path)
		}
		relative, err := filepath.Rel(plan.workingDir, path)
		if err != nil {
			return archivePlan{}, fmt.Errorf("解析打包路径: %w", err)
		}
		operand := "./" + relative
		if relative == "." {
			operand = "."
		}
		plan.operands = append(plan.operands, operand)
		if within(path, a.target) {
			relative, err := filepath.Rel(plan.workingDir, a.target)
			if err != nil {
				return archivePlan{}, fmt.Errorf("解析排除路径: %w", err)
			}
			plan.exclusion = escapePattern(filepath.ToSlash(relative)) + "/*"
		}
	}
	return plan, nil
}
