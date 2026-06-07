package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type logger struct {
	verbose bool
	start   time.Time
}

func (l *logger) log(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

func (l *logger) verboseLog(format string, args ...interface{}) {
	if l != nil && l.verbose {
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
	handle  string
	verbose bool
	ignores []string
)

var rootCmd = &cobra.Command{
	Use:   "flat-sub-dir-files <source> <target>",
	Short: "递归将子目录下的文件复制或移动到指定目录",
	Long: `递归扫描源目录下的所有文件，将它们复制或移动到目标目录的根层级。

如果目标目录存在同名文件，会将来源文件重命名为 <原文件名>.<文件hash>.<扩展名>。
如果包含 hash 的文件名也已存在，则直接覆盖（相同 hash 只保留一份）。`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		source := args[0]
		target := args[1]
		log = &logger{verbose: verbose, start: time.Now()}

		if handle != "copy" && handle != "move" {
			fmt.Fprintf(os.Stderr, "错误: --handle 参数必须为 copy 或 move，当前值: %s\n", handle)
			os.Exit(1)
		}

		if err := run(source, target, handle, ignores); err != nil {
			fmt.Fprintf(os.Stderr, "\n错误: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n总耗时: %s\n", log.elapsed())
	},
}

func init() {
	rootCmd.Flags().StringVar(&handle, "handle", "copy", "操作方式: copy（复制）或 move（移动）")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "显示详细日志")
	rootCmd.Flags().StringArrayVar(&ignores, "ignore", nil, "忽略的文件夹名或文件名，可多次指定")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

type fileEntry struct {
	SourcePath string
	FileName   string
	FileSize   int64
}

func run(source, target, handle string, ignores []string) error {
	const totalSteps = 4

	// 步骤 1: 验证目录
	log.step(1, totalSteps, "验证目录")
	absSource, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	log.log("源目录: %s", absSource)
	log.log("目标目录: %s", absTarget)

	srcInfo, err := os.Stat(absSource)
	if err != nil {
		return fmt.Errorf("源目录不存在: %s", absSource)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("源路径不是目录: %s", absSource)
	}

	// 创建目标目录（如果不存在）
	if err := os.MkdirAll(absTarget, 0755); err != nil {
		return fmt.Errorf("无法创建目标目录: %w", err)
	}
	log.log("✓ 目录验证通过")

	// 步骤 2: 扫描源目录
	log.step(2, totalSteps, "扫描文件")
	entries, err := collectEntries(absSource, ignores)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		log.log("源目录中没有文件，无需处理")
		return nil
	}
	log.log("✓ 共发现 %d 个文件", len(entries))

	// 步骤 3: 执行复制或移动
	if handle == "copy" {
		log.step(3, totalSteps, "复制文件")
		err = processEntries(entries, absTarget, false)
	} else {
		log.step(3, totalSteps, "移动文件")
		err = processEntries(entries, absTarget, true)
	}
	if err != nil {
		return err
	}

	// 步骤 4: 移动模式下删除空目录
	if handle == "move" {
		log.step(4, totalSteps, "清理空目录")
		removed, err := removeEmptyDirs(absSource, ignores)
		if err != nil {
			return err
		}
		log.log("✓ 已删除 %d 个空目录", removed)
	} else {
		log.step(4, totalSteps, "完成")
		log.log("✓ 操作模式: 复制")
	}

	// 总结
	fmt.Println()
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("执行摘要")
	fmt.Println(strings.Repeat("=", 50))
	if handle == "copy" {
		log.log("操作模式: 复制")
	} else {
		log.log("操作模式: 移动")
	}
	log.log("已处理: %d 个文件", len(entries))
	log.log("源目录: %s", absSource)
	log.log("目标目录: %s", absTarget)

	return nil
}

func collectEntries(root string, ignores []string) ([]fileEntry, error) {
	var entries []fileEntry
	var fileCount int

	ignoreSet := newIgnoreSet(ignores)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// 忽略 . 开头的隐藏文件和文件夹
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// 忽略 --ignore 指定的文件名或文件夹名
		if ignoreSet.match(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
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

		fileCount++
		log.verboseLog("  [%d] 发现文件: %s", fileCount, filepath.Base(path))

		entries = append(entries, fileEntry{
			SourcePath: path,
			FileName:   filepath.Base(path),
			FileSize:   info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func processEntries(entries []fileEntry, targetDir string, isMove bool) error {
	var processedCount int
	var skippedCount int

	for i, entry := range entries {
		destPath, err := resolveDestPath(entry, targetDir)
		if err != nil {
			return fmt.Errorf("确定目标路径失败 %s: %w", entry.SourcePath, err)
		}

		// 检查源路径和目标路径是否相同（源目录就是目标目录的情况）
		if samePath(entry.SourcePath, destPath) {
			log.verboseLog("  [%d] 跳过（文件已在目标位置）: %s", i+1, entry.FileName)
			skippedCount++
			continue
		}

		processedCount++
		action := "复制"
		if isMove {
			action = "移动"
		}

		if isMove {
			log.verboseLog("  [%d] %s: %s -> %s", i+1, action, entry.SourcePath, destPath)
			if err := os.Rename(entry.SourcePath, destPath); err != nil {
				// 跨设备移动需要用复制+删除
				if isCrossDeviceError(err) {
					log.verboseLog("      跨设备移动，使用复制+删除方式")
					if err := copyFile(entry.SourcePath, destPath); err != nil {
						return fmt.Errorf("复制文件失败 %s: %w", entry.SourcePath, err)
					}
					if err := os.Remove(entry.SourcePath); err != nil {
						return fmt.Errorf("删除源文件失败 %s: %w", entry.SourcePath, err)
					}
				} else {
					return fmt.Errorf("移动文件失败 %s: %w", entry.SourcePath, err)
				}
			}
		} else {
			log.verboseLog("  [%d] %s: %s -> %s", i+1, action, entry.SourcePath, destPath)
			if err := copyFile(entry.SourcePath, destPath); err != nil {
				return fmt.Errorf("复制文件失败 %s: %w", entry.SourcePath, err)
			}
		}
	}

	if isMove {
		log.log("✓ 已移动 %d 个文件", processedCount)
	} else {
		log.log("✓ 已复制 %d 个文件", processedCount)
	}
	if skippedCount > 0 {
		log.log("  跳过 %d 个已在目标位置的文件", skippedCount)
	}
	return nil
}

// resolveDestPath 确定文件的最终目标路径，处理同名冲突
// 逻辑：
// 1. 如果目标路径不存在同名文件，直接使用原始文件名
// 2. 如果存在同名文件，计算源文件 hash，使用 <原文件名>.<hash>.<扩展名> 格式
// 3. 如果包含 hash 的文件名也已存在，则覆盖（相同 hash 只保留一份）
func resolveDestPath(entry fileEntry, targetDir string) (string, error) {
	baseDestPath := filepath.Join(targetDir, entry.FileName)

	// 目标路径不存在同名文件，直接使用
	if _, err := os.Stat(baseDestPath); os.IsNotExist(err) {
		return baseDestPath, nil
	} else if err != nil {
		return "", err
	}

	// 存在同名文件，按需计算源文件 hash
	hash, err := hashFile(entry.SourcePath)
	if err != nil {
		return "", fmt.Errorf("计算文件 hash 失败 %s: %w", entry.SourcePath, err)
	}
	log.verboseLog("      同名文件已存在，计算 hash: %s...", hash[:16])

	// 使用 hash 重命名
	hashedName := buildHashedFileName(entry.FileName, hash)
	hashedDestPath := filepath.Join(targetDir, hashedName)

	// 包含 hash 的文件名也已存在，直接覆盖（相同 hash 保留一份）
	if _, err := os.Stat(hashedDestPath); err == nil {
		log.verboseLog("      同名+hash 文件已存在，将覆盖: %s", hashedName)
		return hashedDestPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	// 使用 hash 重命名的路径
	log.verboseLog("      重命名为: %s", hashedName)
	return hashedDestPath, nil
}

// buildHashedFileName 构建 <原文件名>.<hash>.<扩展名> 格式的文件名
// 例如: hello.txt -> hello.xxxyyyzzz.txt
// 无扩展名时: README -> README.xxxyyyzzz
// 隐藏文件时: .gitignore -> .gitignore.xxxyyyzzz, .env -> .env.xxxyyyzzz
func buildHashedFileName(fileName, hash string) string {
	ext := filepath.Ext(fileName)
	// filepath.Ext 对点号开头的文件名（如 .gitignore, .env）返回整个文件名作为 ext，
	// 这种情况下应视为无扩展名，hash 直接追加在文件名末尾
	if ext == "" || ext == fileName {
		return fileName + "." + hash
	}
	nameWithoutExt := strings.TrimSuffix(fileName, ext)
	return nameWithoutExt + "." + hash + ext
}

func removeEmptyDirs(root string, ignores []string) (int, error) {
	var removedCount int
	ignoreSet := newIgnoreSet(ignores)

	// 反复扫描删除空目录，直到没有空目录为止
	for {
		found := false
		// 收集所有空目录后再删除，避免遍历时修改目录树
		var emptyDirs []string
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				// 忽略已删除目录的访问错误
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			// 忽略 . 开头的隐藏目录（跳过整个子树）
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") && !samePath(path, root) {
				return filepath.SkipDir
			}
			// 忽略 --ignore 指定的目录
			if d.IsDir() && ignoreSet.match(d.Name()) && !samePath(path, root) {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				return nil
			}
			// 跳过根目录本身
			if samePath(path, root) {
				return nil
			}

			entries, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				emptyDirs = append(emptyDirs, path)
			}
			return nil
		})
		if err != nil {
			return removedCount, err
		}

		for _, dir := range emptyDirs {
			log.verboseLog("  删除空目录: %s", dir)
			if err := os.Remove(dir); err != nil {
				if !os.IsNotExist(err) {
					return removedCount, err
				}
			}
			removedCount++
			found = true
		}

		if !found {
			break
		}
	}

	return removedCount, nil
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

	destination, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	return destination.Sync()
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func isCrossDeviceError(err error) bool {
	// 跨设备移动时，os.Rename 会返回 EXDEV 错误
	if linkErr, ok := err.(*os.LinkError); ok {
		return linkErr.Err.Error() == "invalid cross-device link"
	}
	return false
}

// ignoreSet 用于高效判断文件名或目录名是否应被忽略
type ignoreSet struct {
	set map[string]bool
}

func newIgnoreSet(items []string) ignoreSet {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return ignoreSet{set: s}
}

func (is ignoreSet) match(name string) bool {
	return is.set[name]
}
