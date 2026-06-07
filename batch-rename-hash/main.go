package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const mapFileName = "file_map.txt"

type fileEntry struct {
	OriginalPath string
	RelativePath string
	Hash         string
	NewPath      string
	NewRelative  string
	TempPath     string
	FileSize     int64
}

type logger struct {
	verbose bool
	start   time.Time
}

func (l *logger) log(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

func (l *logger) verboseLog(format string, args ...interface{}) {
	if l.verbose {
		fmt.Printf(format+"\n", args...)
	}
}

func (l *logger) step(stepNum, totalSteps int, format string, args ...interface{}) {
	fmt.Printf("\n[步骤 %d/%d] %s\n", stepNum, totalSteps, fmt.Sprintf(format, args...))
	fmt.Println(strings.Repeat("-", 50))
}

func (l *logger) elapsed() time.Duration {
	return time.Since(l.start).Round(time.Millisecond)
}

var log *logger

var (
	keepOldFile bool
	verbose     bool
)

var rootCmd = &cobra.Command{
	Use:   "batch-rename-hash <目录>",
	Short: "批量将文件重命名为其内容的 SHA-256 hash 值",
	Long: `批量将文件重命名为其内容的 SHA-256 hash 值。

扫描指定目录下的所有文件，将文件名替换为文件内容的 SHA-256 哈希值，
同时保留原文件扩展名。操作完成后会生成 file_map.txt 映射文件记录对应关系。`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := args[0]
		log = &logger{verbose: verbose, start: time.Now()}
		if err := run(dir, keepOldFile); err != nil {
			fmt.Fprintf(os.Stderr, "\n错误: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n总耗时: %s\n", log.elapsed())
	},
}

func init() {
	rootCmd.Flags().BoolVar(&keepOldFile, "keep-old-file", false, "保留原文件，复制一份以 hash 命名的新文件")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "显示详细日志")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(root string, keepOldFile bool) error {
	const totalSteps = 5

	// 步骤 1: 验证目录
	log.step(1, totalSteps, "验证目标目录")
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	log.log("目标目录: %s", absRoot)

	info, err := os.Stat(absRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s 不是目录", absRoot)
	}
	log.log("✓ 目录验证通过")

	// 步骤 2: 扫描文件并计算 hash
	log.step(2, totalSteps, "扫描文件并计算 Hash")
	entries, err := collectEntries(absRoot)
	if err != nil {
		return err
	}
	log.log("✓ 共发现 %d 个文件", len(entries))

	// 步骤 3: 验证目标路径
	log.step(3, totalSteps, "验证目标路径")
	if err := validateTargets(entries); err != nil {
		return err
	}
	log.log("✓ 目标路径验证通过")

	// 步骤 4: 执行重命名或复制
	if keepOldFile {
		log.step(4, totalSteps, "复制文件")
		err = copyEntries(entries)
	} else {
		log.step(4, totalSteps, "重命名文件")
		err = renameEntries(entries)
	}
	if err != nil {
		return err
	}

	// 步骤 5: 写入映射文件
	log.step(5, totalSteps, "写入映射文件")
	if err := writeMapFile(absRoot, entries); err != nil {
		return err
	}
	log.log("✓ 映射文件已写入: %s", filepath.Join(absRoot, mapFileName))

	// 总结
	fmt.Println()
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("执行摘要")
	fmt.Println(strings.Repeat("=", 50))
	if keepOldFile {
		log.log("操作模式: 复制（保留原文件）")
		log.log("已复制: %d 个文件", len(entries))
	} else {
		log.log("操作模式: 重命名")
		log.log("已重命名: %d 个文件", len(entries))
	}
	log.log("映射文件: %s", filepath.Join(absRoot, mapFileName))

	return nil
}

func collectEntries(root string) ([]fileEntry, error) {
	var entries []fileEntry
	mapFilePath := filepath.Join(root, mapFileName)
	var fileCount int

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		if samePath(path, mapFilePath) {
			log.verboseLog("  跳过映射文件: %s", path)
			return nil
		}

		fileCount++
		log.verboseLog("  [%d] 正在计算 hash: %s", fileCount, filepath.Base(path))

		hash, err := hashFile(path)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// 保留原文件扩展名
		ext := filepath.Ext(path)
		newFileName := hash + ext
		newPath := filepath.Join(filepath.Dir(path), newFileName)
		newRel, err := filepath.Rel(root, newPath)
		if err != nil {
			return err
		}

		log.verboseLog("      → hash: %s", hash[:16]+"...")

		entries = append(entries, fileEntry{
			OriginalPath: path,
			RelativePath: filepath.ToSlash(rel),
			Hash:         hash,
			NewPath:      newPath,
			NewRelative:  filepath.ToSlash(newRel),
			FileSize:     info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].OriginalPath < entries[j].OriginalPath
	})
	return entries, nil
}

func validateTargets(entries []fileEntry) error {
	targets := make(map[string]string, len(entries))
	originals := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		originals[cleanPath(entry.OriginalPath)] = struct{}{}
	}

	var skipCount int
	for _, entry := range entries {
		targetKey := cleanPath(entry.NewPath)
		if existing, ok := targets[targetKey]; ok {
			return fmt.Errorf("同一目录下多个文件解析到相同目标 %s: %s 和 %s", entry.NewPath, existing, entry.OriginalPath)
		}
		targets[targetKey] = entry.OriginalPath

		if samePath(entry.OriginalPath, entry.NewPath) {
			log.verboseLog("  跳过（文件名已是 hash）: %s", entry.RelativePath)
			skipCount++
			continue
		}

		if _, ok := originals[targetKey]; ok {
			continue
		}
		if _, err := os.Stat(entry.NewPath); err == nil {
			return fmt.Errorf("目标文件已存在: %s", entry.NewPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if skipCount > 0 {
		log.log("  %d 个文件名已是 hash，无需处理", skipCount)
	}
	return nil
}

func renameEntries(entries []fileEntry) error {
	var processedCount int

	// 第一阶段：移动到临时文件
	log.log("  第一阶段: 移动文件到临时位置")
	for i := range entries {
		if samePath(entries[i].OriginalPath, entries[i].NewPath) {
			continue
		}

		log.verboseLog("    [%d] 临时移动: %s", processedCount+1, entries[i].RelativePath)
		tempPath, err := uniqueTempPath(entries[i].OriginalPath)
		if err != nil {
			return err
		}
		if err := os.Rename(entries[i].OriginalPath, tempPath); err != nil {
			return err
		}
		entries[i].TempPath = tempPath
		processedCount++
	}
	log.log("  ✓ 已移动 %d 个文件到临时位置", processedCount)

	// 第二阶段：重命名为最终名称
	log.log("  第二阶段: 重命名为 hash 名称")
	for i, entry := range entries {
		if samePath(entry.OriginalPath, entry.NewPath) {
			continue
		}
		log.verboseLog("    [%d] 重命名: %s -> %s", i+1, filepath.Base(entry.TempPath), entry.Hash[:16]+"...")
		if err := os.Rename(entry.TempPath, entry.NewPath); err != nil {
			return err
		}
	}
	log.log("  ✓ 已重命名 %d 个文件", processedCount)
	return nil
}

func copyEntries(entries []fileEntry) error {
	var processedCount int
	for _, entry := range entries {
		if samePath(entry.OriginalPath, entry.NewPath) {
			continue
		}
		processedCount++
		log.verboseLog("  [%d] 复制: %s -> %s...", processedCount, entry.RelativePath, entry.Hash[:16])
		if err := copyFile(entry.OriginalPath, entry.NewPath); err != nil {
			return err
		}
	}
	log.log("  ✓ 已复制 %d 个文件", processedCount)
	return nil
}

func writeMapFile(root string, entries []fileEntry) error {
	mapPath := filepath.Join(root, mapFileName)
	log.verboseLog("  写入路径: %s", mapPath)

	file, err := os.Create(mapPath)
	if err != nil {
		return err
	}
	defer file.Close()

	for i, entry := range entries {
		if i > 0 {
			fmt.Fprintln(file)
		}
		fmt.Fprintf(file, "源文件完整路径: %s\n", entry.OriginalPath)
		fmt.Fprintf(file, "新文件完整路径: %s\n", entry.NewPath)
		fmt.Fprintf(file, "文件大小: %d\n", entry.FileSize)
	}
	log.verboseLog("  写入 %d 条记录", len(entries))
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	hash := hex.EncodeToString(sum.Sum(nil))
	return hash, nil
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return err
	}

	destination, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	return destination.Sync()
}

func uniqueTempPath(path string) (string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	for i := 0; i < 1000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf(".%s.rename-tmp-%d", base, i))
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not create temporary rename path for %s", path)
}

func samePath(a, b string) bool {
	return cleanPath(a) == cleanPath(b)
}

func cleanPath(path string) string {
	return filepath.Clean(path)
}
