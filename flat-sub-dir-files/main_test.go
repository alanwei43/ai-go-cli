package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// =============================================================================
// 辅助函数
// =============================================================================

// createTempDir 创建临时目录并在测试结束后自动清理
func createTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "flat-test-")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// writeFile 在指定目录下创建文件，自动创建父目录
func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("创建目录失败 %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("写入文件失败 %s: %v", fullPath, err)
	}
}

// readFile 读取文件内容
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败 %s: %v", path, err)
	}
	return string(data)
}

// fileExists 检查文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// listDir 列出目录下的文件名（排序后）
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("列出目录失败 %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// computeHash 计算文件 SHA-256 hash
func computeHash(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开文件失败 %s: %v", path, err)
	}
	defer file.Close()

	sum := sha256.New()
	if _, err := sum.Write([]byte(readFile(t, path))); err != nil {
		t.Fatalf("计算 hash 失败: %v", err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// =============================================================================
// buildHashedFileName 测试
// =============================================================================

func TestBuildHashedFileName(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		hash     string
		want     string
	}{
		{
			name:     "普通文件带扩展名",
			fileName: "hello.txt",
			hash:     "abc123",
			want:     "hello.abc123.txt",
		},
		{
			name:     "无扩展名",
			fileName: "README",
			hash:     "def456",
			want:     "README.def456",
		},
		{
			name:     "多段扩展名",
			fileName: "archive.tar.gz",
			hash:     "ghi789",
			want:     "archive.tar.ghi789.gz",
		},
		{
			name:     "隐藏文件（点号开头，filepath.Ext 视为无扩展名）",
			fileName: ".gitignore",
			hash:     "xyz000",
			want:     ".gitignore.xyz000",
		},
		{
			name:     "仅点号开头无扩展名",
			fileName: ".env",
			hash:     "aaa111",
			want:     ".env.aaa111",
		},
		{
			name:     "长 hash",
			fileName: "data.json",
			hash:     "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03",
			want:     "data.5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildHashedFileName(tt.fileName, tt.hash)
			if got != tt.want {
				t.Errorf("buildHashedFileName(%q, %q) = %q, want %q", tt.fileName, tt.hash, got, tt.want)
			}
		})
	}
}

// =============================================================================
// hashFile 测试
// =============================================================================

func TestHashFile(t *testing.T) {
	dir := createTempDir(t)
	filePath := filepath.Join(dir, "test.txt")
	content := "hello world"
	os.WriteFile(filePath, []byte(content), 0644)

	got, err := hashFile(filePath)
	if err != nil {
		t.Fatalf("hashFile() error = %v", err)
	}

	// 验证返回的是有效的 64 字符 hex 字符串（SHA-256）
	if len(got) != 64 {
		t.Errorf("hashFile() 返回长度 = %d, want 64", len(got))
	}

	// 手动计算期望值
	sum := sha256.Sum256([]byte(content))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Errorf("hashFile() = %q, want %q", got, want)
	}
}

func TestHashFileNotExist(t *testing.T) {
	_, err := hashFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("hashFile() 对不存在的文件应返回错误，但返回 nil")
	}
}

// =============================================================================
// samePath 测试
// =============================================================================

func TestSamePath(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"相同路径", "/tmp/a.txt", "/tmp/a.txt", true},
		{"尾斜杠归一", "/tmp/dir", "/tmp/dir/", true},
		{"不同路径", "/tmp/a.txt", "/tmp/b.txt", false},
		{"相对路径点号", "./a.txt", "a.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := samePath(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("samePath(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// =============================================================================
// copyFile 测试
// =============================================================================

func TestCopyFile(t *testing.T) {
	dir := createTempDir(t)
	srcPath := filepath.Join(dir, "source.txt")
	dstPath := filepath.Join(dir, "dest.txt")
	content := "copy test content\n第二行"

	os.WriteFile(srcPath, []byte(content), 0644)

	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}

	got := readFile(t, dstPath)
	if got != content {
		t.Errorf("copyFile() 内容 = %q, want %q", got, content)
	}

	// 验证源文件未被修改
	srcGot := readFile(t, srcPath)
	if srcGot != content {
		t.Error("copyFile() 不应修改源文件")
	}
}

func TestCopyFilePreservePermissions(t *testing.T) {
	dir := createTempDir(t)
	srcPath := filepath.Join(dir, "source.sh")
	dstPath := filepath.Join(dir, "dest.sh")

	os.WriteFile(srcPath, []byte("#!/bin/bash"), 0755)

	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}

	srcInfo, _ := os.Stat(srcPath)
	dstInfo, _ := os.Stat(dstPath)

	if srcInfo.Mode().Perm() != dstInfo.Mode().Perm() {
		t.Errorf("copyFile() 权限 = %o, want %o", dstInfo.Mode().Perm(), srcInfo.Mode().Perm())
	}
}

func TestCopyFileSourceNotExist(t *testing.T) {
	err := copyFile("/nonexistent/file.txt", "/tmp/dest.txt")
	if err == nil {
		t.Error("copyFile() 对不存在的源文件应返回错误")
	}
}

// =============================================================================
// collectEntries 测试
// =============================================================================

func TestCollectEntries(t *testing.T) {
	dir := createTempDir(t)
	writeFile(t, dir, "a.txt", "aaa")
	writeFile(t, dir, "sub1/b.txt", "bbb")
	writeFile(t, dir, "sub1/sub2/c.txt", "ccc")
	writeFile(t, dir, "sub3/d.txt", "ddd")

	entries, err := collectEntries(dir, nil)
	if err != nil {
		t.Fatalf("collectEntries() error = %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("collectEntries() 文件数量 = %d, want 4", len(entries))
	}

	// 验证每个 entry 的文件名
	names := make(map[string]bool)
	for _, e := range entries {
		names[e.FileName] = true
	}
	for _, want := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		if !names[want] {
			t.Errorf("collectEntries() 缺少文件 %q", want)
		}
	}
}

func TestCollectEntriesEmptyDir(t *testing.T) {
	dir := createTempDir(t)

	entries, err := collectEntries(dir, nil)
	if err != nil {
		t.Fatalf("collectEntries() error = %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("collectEntries() 空目录应返回 0 个文件，got %d", len(entries))
	}
}

func TestCollectEntriesSkipsNonRegularFiles(t *testing.T) {
	dir := createTempDir(t)
	writeFile(t, dir, "regular.txt", "hello")

	// 创建符号链接
	linkPath := filepath.Join(dir, "link.txt")
	os.Symlink(filepath.Join(dir, "regular.txt"), linkPath)

	entries, err := collectEntries(dir, nil)
	if err != nil {
		t.Fatalf("collectEntries() error = %v", err)
	}

	// 只应收集到普通文件，不包括符号链接
	if len(entries) != 1 {
		t.Errorf("collectEntries() 应跳过符号链接，got %d 个文件", len(entries))
	}
	if entries[0].FileName != "regular.txt" {
		t.Errorf("collectEntries()[0].FileName = %q, want %q", entries[0].FileName, "regular.txt")
	}
}

func TestCollectEntriesSkipsHiddenFilesAndDirs(t *testing.T) {
	dir := createTempDir(t)
	writeFile(t, dir, "a.txt", "aaa")
	writeFile(t, dir, ".hidden_file", "hidden")
	writeFile(t, dir, "sub/b.txt", "bbb")
	writeFile(t, dir, ".hidden_dir/c.txt", "should be skipped")
	writeFile(t, dir, "sub/.hidden", "should be skipped")

	entries, err := collectEntries(dir, nil)
	if err != nil {
		t.Fatalf("collectEntries() error = %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("collectEntries() 应跳过隐藏文件和隐藏目录，got %d 个文件", len(entries))
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.FileName] = true
	}
	if !names["a.txt"] {
		t.Error("应包含 a.txt")
	}
	if !names["b.txt"] {
		t.Error("应包含 b.txt")
	}
	if names[".hidden_file"] {
		t.Error("应跳过 .hidden_file")
	}
	if names["c.txt"] {
		t.Error("应跳过 .hidden_dir 下的 c.txt")
	}
	if names[".hidden"] {
		t.Error("应跳过 .hidden")
	}
}

// =============================================================================
// resolveDestPath 测试
// =============================================================================

func TestResolveDestPathNoConflict(t *testing.T) {
	targetDir := createTempDir(t)
	sourceDir := createTempDir(t)
	writeFile(t, sourceDir, "hello.txt", "world")

	entry := fileEntry{
		SourcePath: filepath.Join(sourceDir, "hello.txt"),
		FileName:   "hello.txt",
	}

	destPath, err := resolveDestPath(entry, targetDir)
	if err != nil {
		t.Fatalf("resolveDestPath() error = %v", err)
	}

	want := filepath.Join(targetDir, "hello.txt")
	if destPath != want {
		t.Errorf("resolveDestPath() 无冲突时 = %q, want %q", destPath, want)
	}
}

func TestResolveDestPathWithConflict(t *testing.T) {
	targetDir := createTempDir(t)
	sourceDir := createTempDir(t)

	// 目标目录已存在同名文件
	writeFile(t, targetDir, "hello.txt", "existing")
	// 源文件
	writeFile(t, sourceDir, "hello.txt", "source content")

	entry := fileEntry{
		SourcePath: filepath.Join(sourceDir, "hello.txt"),
		FileName:   "hello.txt",
	}

	destPath, err := resolveDestPath(entry, targetDir)
	if err != nil {
		t.Fatalf("resolveDestPath() error = %v", err)
	}

	// 应该重命名为 hello.<hash>.txt
	hash := computeHash(t, entry.SourcePath)
	want := filepath.Join(targetDir, "hello."+hash+".txt")
	if destPath != want {
		t.Errorf("resolveDestPath() 有冲突时 = %q, want %q", destPath, want)
	}
}

func TestResolveDestPathHashedFileAlsoExists(t *testing.T) {
	targetDir := createTempDir(t)
	sourceDir := createTempDir(t)

	// 目标目录已存在同名文件
	writeFile(t, targetDir, "hello.txt", "existing")
	// 源文件
	writeFile(t, sourceDir, "hello.txt", "source content")
	// 目标目录也存在 hash 文件名
	hash := computeHash(t, filepath.Join(sourceDir, "hello.txt"))
	writeFile(t, targetDir, "hello."+hash+".txt", "old hash file")

	entry := fileEntry{
		SourcePath: filepath.Join(sourceDir, "hello.txt"),
		FileName:   "hello.txt",
	}

	destPath, err := resolveDestPath(entry, targetDir)
	if err != nil {
		t.Fatalf("resolveDestPath() error = %v", err)
	}

	// hash 文件已存在时应返回 hash 路径（允许覆盖）
	want := filepath.Join(targetDir, "hello."+hash+".txt")
	if destPath != want {
		t.Errorf("resolveDestPath() hash 文件已存在时 = %q, want %q", destPath, want)
	}
}

// =============================================================================
// processEntries 测试（复制模式）
// =============================================================================

func TestProcessEntriesCopy(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	writeFile(t, sourceDir, "a.txt", "aaa")
	writeFile(t, sourceDir, "sub/b.txt", "bbb")
	writeFile(t, sourceDir, "sub/deep/c.txt", "ccc")

	entries, err := collectEntries(sourceDir, nil)
	if err != nil {
		t.Fatalf("collectEntries() error = %v", err)
	}

	if err := processEntries(entries, targetDir, false); err != nil {
		t.Fatalf("processEntries() copy error = %v", err)
	}

	// 目标目录应有 3 个平铺文件
	names := listDir(t, targetDir)
	sort.Strings(names)
	want := []string{"a.txt", "b.txt", "c.txt"}
	if len(names) != len(want) {
		t.Fatalf("目标文件数 = %d, want %d; got %v", len(names), len(want), names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("目标文件[%d] = %q, want %q", i, names[i], n)
		}
	}

	// 验证内容正确
	if got := readFile(t, filepath.Join(targetDir, "a.txt")); got != "aaa" {
		t.Errorf("a.txt 内容 = %q, want %q", got, "aaa")
	}
	if got := readFile(t, filepath.Join(targetDir, "b.txt")); got != "bbb" {
		t.Errorf("b.txt 内容 = %q, want %q", got, "bbb")
	}
	if got := readFile(t, filepath.Join(targetDir, "c.txt")); got != "ccc" {
		t.Errorf("c.txt 内容 = %q, want %q", got, "ccc")
	}

	// 复制模式下源文件应保留
	if !fileExists(filepath.Join(sourceDir, "a.txt")) {
		t.Error("复制模式不应删除源文件")
	}
}

func TestProcessEntriesCopyWithConflict(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	writeFile(t, sourceDir, "sub/hello.txt", "source hello")
	writeFile(t, targetDir, "hello.txt", "target hello")

	entries, err := collectEntries(sourceDir, nil)
	if err != nil {
		t.Fatalf("collectEntries() error = %v", err)
	}

	if err := processEntries(entries, targetDir, false); err != nil {
		t.Fatalf("processEntries() copy with conflict error = %v", err)
	}

	// 目标应有原文件 + hash 重命名文件
	names := listDir(t, targetDir)
	if len(names) != 2 {
		t.Fatalf("目标文件数 = %d, want 2; got %v", len(names), names)
	}

	// 原文件内容不变
	if got := readFile(t, filepath.Join(targetDir, "hello.txt")); got != "target hello" {
		t.Errorf("原 hello.txt 内容 = %q, want %q", got, "target hello")
	}

	// hash 重命名文件内容是源文件内容
	hash := computeHash(t, filepath.Join(sourceDir, "sub/hello.txt"))
	hashedName := "hello." + hash + ".txt"
	if got := readFile(t, filepath.Join(targetDir, hashedName)); got != "source hello" {
		t.Errorf("hash 文件内容 = %q, want %q", got, "source hello")
	}
}

// =============================================================================
// processEntries 测试（移动模式）
// =============================================================================

func TestProcessEntriesMove(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	writeFile(t, sourceDir, "a.txt", "aaa")
	writeFile(t, sourceDir, "sub/b.txt", "bbb")

	entries, err := collectEntries(sourceDir, nil)
	if err != nil {
		t.Fatalf("collectEntries() error = %v", err)
	}

	if err := processEntries(entries, targetDir, true); err != nil {
		t.Fatalf("processEntries() move error = %v", err)
	}

	// 目标目录应有文件
	names := listDir(t, targetDir)
	if len(names) != 2 {
		t.Fatalf("目标文件数 = %d, want 2; got %v", len(names), names)
	}

	// 源文件应被移走
	if fileExists(filepath.Join(sourceDir, "a.txt")) {
		t.Error("移动模式应删除源文件")
	}
}

func TestProcessEntriesMoveWithConflict(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	writeFile(t, sourceDir, "sub/hello.txt", "source hello")
	writeFile(t, targetDir, "hello.txt", "target hello")

	entries, err := collectEntries(sourceDir, nil)
	if err != nil {
		t.Fatalf("collectEntries() error = %v", err)
	}

	if err := processEntries(entries, targetDir, true); err != nil {
		t.Fatalf("processEntries() move with conflict error = %v", err)
	}

	// 源文件应被移走
	if fileExists(filepath.Join(sourceDir, "sub/hello.txt")) {
		t.Error("移动模式应删除源文件")
	}

	// 目标应有原文件 + hash 文件
	names := listDir(t, targetDir)
	if len(names) != 2 {
		t.Fatalf("目标文件数 = %d, want 2; got %v", len(names), names)
	}
}

// =============================================================================
// removeEmptyDirs 测试
// =============================================================================

func TestRemoveEmptyDirs(t *testing.T) {
	dir := createTempDir(t)
	os.MkdirAll(filepath.Join(dir, "a/b/c"), 0755)
	os.MkdirAll(filepath.Join(dir, "x/y"), 0755)
	writeFile(t, dir, "a/b/file.txt", "keep")

	removed, err := removeEmptyDirs(dir, nil)
	if err != nil {
		t.Fatalf("removeEmptyDirs() error = %v", err)
	}

	// x/y, x, a/b/c 应被删除 = 3 个
	if removed != 3 {
		t.Errorf("removeEmptyDirs() 删除数 = %d, want 3", removed)
	}

	// 验证: a/b 和 a/b/file.txt 应保留
	if !fileExists(filepath.Join(dir, "a/b/file.txt")) {
		t.Error("有文件的目录不应被删除")
	}
	if fileExists(filepath.Join(dir, "x")) {
		t.Error("空目录 x 应被删除")
	}
	if fileExists(filepath.Join(dir, "a/b/c")) {
		t.Error("空目录 a/b/c 应被删除")
	}
}

func TestRemoveEmptyDirsNoEmptyDirs(t *testing.T) {
	dir := createTempDir(t)
	writeFile(t, dir, "a.txt", "hello")

	removed, err := removeEmptyDirs(dir, nil)
	if err != nil {
		t.Fatalf("removeEmptyDirs() error = %v", err)
	}

	if removed != 0 {
		t.Errorf("removeEmptyDirs() 无空目录时 = %d, want 0", removed)
	}
}

func TestRemoveEmptyDirsPreservesRoot(t *testing.T) {
	dir := createTempDir(t)
	// 根目录本身为空，但不应被删除
	removed, err := removeEmptyDirs(dir, nil)
	if err != nil {
		t.Fatalf("removeEmptyDirs() error = %v", err)
	}

	if removed != 0 {
		t.Errorf("removeEmptyDirs() 空根目录不应被删除, got %d", removed)
	}

	if !fileExists(dir) {
		t.Error("根目录不应被删除")
	}
}

func TestRemoveEmptyDirsSkipsHiddenDirs(t *testing.T) {
	dir := createTempDir(t)
	os.MkdirAll(filepath.Join(dir, "empty_sub"), 0755)
	os.MkdirAll(filepath.Join(dir, ".hidden_empty"), 0755)
	os.MkdirAll(filepath.Join(dir, ".hidden_with_file/nested"), 0755)
	writeFile(t, dir, ".hidden_with_file/secret.txt", "keep")

	removed, err := removeEmptyDirs(dir, nil)
	if err != nil {
		t.Fatalf("removeEmptyDirs() error = %v", err)
	}

	// 只有 empty_sub 应被删除
	if removed != 1 {
		t.Errorf("removeEmptyDirs() 删除数 = %d, want 1", removed)
	}

	// 隐藏目录应保留
	if !fileExists(filepath.Join(dir, ".hidden_empty")) {
		t.Error("隐藏空目录 .hidden_empty 不应被删除")
	}
	if !fileExists(filepath.Join(dir, ".hidden_with_file")) {
		t.Error("隐藏目录 .hidden_with_file 不应被删除")
	}
}

// =============================================================================
// isCrossDeviceError 测试
// =============================================================================

func TestIsCrossDeviceError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil 错误",
			err:  nil,
			want: false,
		},
		{
			name: "普通错误",
			err:  os.ErrNotExist,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCrossDeviceError(tt.err)
			if got != tt.want {
				t.Errorf("isCrossDeviceError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// run 集成测试
// =============================================================================

func TestRunCopy(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	writeFile(t, sourceDir, "a.txt", "aaa")
	writeFile(t, sourceDir, "sub1/b.txt", "bbb")
	writeFile(t, sourceDir, "sub1/deep/c.txt", "ccc")

	// 初始化全局 logger
	log = &logger{verbose: false, start: time.Now()}

	if err := run(sourceDir, targetDir, "copy", nil); err != nil {
		t.Fatalf("run() copy error = %v", err)
	}

	// 验证目标目录平铺了所有文件
	names := listDir(t, targetDir)
	sort.Strings(names)
	want := []string{"a.txt", "b.txt", "c.txt"}
	if len(names) != len(want) {
		t.Fatalf("目标文件数 = %d, want %d; got %v", len(names), len(want), names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("目标文件[%d] = %q, want %q", i, names[i], n)
		}
	}

	// 源文件应保留
	if !fileExists(filepath.Join(sourceDir, "a.txt")) {
		t.Error("复制模式不应删除源文件")
	}
}

func TestRunMove(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	writeFile(t, sourceDir, "a.txt", "aaa")
	writeFile(t, sourceDir, "sub/b.txt", "bbb")

	log = &logger{verbose: false, start: time.Now()}

	if err := run(sourceDir, targetDir, "move", nil); err != nil {
		t.Fatalf("run() move error = %v", err)
	}

	// 目标目录应有文件
	names := listDir(t, targetDir)
	if len(names) != 2 {
		t.Fatalf("目标文件数 = %d, want 2; got %v", len(names), names)
	}

	// 源文件应被移走
	if fileExists(filepath.Join(sourceDir, "a.txt")) {
		t.Error("移动模式应删除源文件")
	}

	// 源目录的空子目录应被清理
	if fileExists(filepath.Join(sourceDir, "sub")) {
		t.Error("移动模式应删除空子目录")
	}
}

func TestRunSourceNotExist(t *testing.T) {
	log = &logger{verbose: false, start: time.Now()}

	err := run("/nonexistent/dir", "/tmp/target", "copy", nil)
	if err == nil {
		t.Error("run() 源目录不存在时应返回错误")
	}
}

func TestRunEmptySourceDir(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	log = &logger{verbose: false, start: time.Now()}

	if err := run(sourceDir, targetDir, "copy", nil); err != nil {
		t.Fatalf("run() 空源目录不应返回错误: %v", err)
	}

	// 目标目录应为空
	names := listDir(t, targetDir)
	if len(names) != 0 {
		t.Errorf("空源目录处理后目标应有 0 个文件, got %d", len(names))
	}
}

func TestRunWithSameNameConflict(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	writeFile(t, sourceDir, "sub/hello.txt", "from source")
	writeFile(t, targetDir, "hello.txt", "already exists")

	log = &logger{verbose: false, start: time.Now()}

	if err := run(sourceDir, targetDir, "copy", nil); err != nil {
		t.Fatalf("run() copy with conflict error = %v", err)
	}

	// 目标应有 2 个文件
	names := listDir(t, targetDir)
	if len(names) != 2 {
		t.Fatalf("目标文件数 = %d, want 2; got %v", len(names), names)
	}

	// 原文件内容不变
	if got := readFile(t, filepath.Join(targetDir, "hello.txt")); got != "already exists" {
		t.Errorf("原 hello.txt = %q, want %q", got, "already exists")
	}

	// hash 文件内容是源文件内容
	hash := computeHash(t, filepath.Join(sourceDir, "sub/hello.txt"))
	hashedName := "hello." + hash + ".txt"
	if !fileExists(filepath.Join(targetDir, hashedName)) {
		t.Errorf("hash 文件 %q 不存在", hashedName)
	}
	if got := readFile(t, filepath.Join(targetDir, hashedName)); got != "from source" {
		t.Errorf("hash 文件内容 = %q, want %q", got, "from source")
	}
}

func TestRunHashedFileOverwrite(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	writeFile(t, sourceDir, "sub/hello.txt", "source content")
	writeFile(t, targetDir, "hello.txt", "target hello")
	// 预先创建 hash 文件
	hash := computeHash(t, filepath.Join(sourceDir, "sub/hello.txt"))
	writeFile(t, targetDir, "hello."+hash+".txt", "old hash content")

	log = &logger{verbose: false, start: time.Now()}

	if err := run(sourceDir, targetDir, "copy", nil); err != nil {
		t.Fatalf("run() copy with hash conflict error = %v", err)
	}

	// hash 文件应被覆盖为源文件内容
	got := readFile(t, filepath.Join(targetDir, "hello."+hash+".txt"))
	if got != "source content" {
		t.Errorf("hash 文件应被覆盖，内容 = %q, want %q", got, "source content")
	}

	// 总共只有 2 个文件（hello.txt + hello.<hash>.txt）
	names := listDir(t, targetDir)
	if len(names) != 2 {
		t.Errorf("目标文件数 = %d, want 2; got %v", len(names), names)
	}
}

// =============================================================================
// ignoreSet 测试
// =============================================================================

func TestIgnoreSet(t *testing.T) {
	is := newIgnoreSet([]string{"node_modules", "dist", "Thumbs.db"})

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"匹配忽略项", "node_modules", true},
		{"匹配忽略项2", "dist", true},
		{"匹配忽略项3", "Thumbs.db", true},
		{"不匹配", "src", false},
		{"不匹配空字符串", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := is.match(tt.input)
			if got != tt.want {
				t.Errorf("ignoreSet.match(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIgnoreSetEmpty(t *testing.T) {
	is := newIgnoreSet(nil)
	if is.match("anything") {
		t.Error("空 ignoreSet 不应匹配任何名称")
	}
}

// =============================================================================
// --ignore 功能测试
// =============================================================================

func TestCollectEntriesWithIgnore(t *testing.T) {
	dir := createTempDir(t)
	writeFile(t, dir, "a.txt", "aaa")
	writeFile(t, dir, "node_modules/pkg/b.txt", "bbb")
	writeFile(t, dir, "dist/c.txt", "ccc")
	writeFile(t, dir, "src/d.txt", "ddd")
	writeFile(t, dir, "Thumbs.db", "thumb")

	entries, err := collectEntries(dir, []string{"node_modules", "dist", "Thumbs.db"})
	if err != nil {
		t.Fatalf("collectEntries() error = %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("collectEntries() 忽略后应剩 2 个文件，got %d", len(entries))
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.FileName] = true
	}
	if !names["a.txt"] {
		t.Error("应包含 a.txt")
	}
	if !names["d.txt"] {
		t.Error("应包含 d.txt")
	}
	if names["b.txt"] {
		t.Error("应忽略 node_modules 下的 b.txt")
	}
	if names["c.txt"] {
		t.Error("应忽略 dist 下的 c.txt")
	}
	if names["Thumbs.db"] {
		t.Error("应忽略 Thumbs.db")
	}
}

func TestRemoveEmptyDirsSkipsIgnoredDirs(t *testing.T) {
	dir := createTempDir(t)
	os.MkdirAll(filepath.Join(dir, "empty_sub"), 0755)
	os.MkdirAll(filepath.Join(dir, "node_modules/empty"), 0755)

	removed, err := removeEmptyDirs(dir, []string{"node_modules"})
	if err != nil {
		t.Fatalf("removeEmptyDirs() error = %v", err)
	}

	// 只有 empty_sub 应被删除
	if removed != 1 {
		t.Errorf("removeEmptyDirs() 删除数 = %d, want 1", removed)
	}

	if !fileExists(filepath.Join(dir, "node_modules")) {
		t.Error("node_modules 目录不应被删除")
	}
}

func TestRunWithIgnore(t *testing.T) {
	sourceDir := createTempDir(t)
	targetDir := createTempDir(t)

	writeFile(t, sourceDir, "a.txt", "aaa")
	writeFile(t, sourceDir, "node_modules/pkg/b.txt", "bbb")
	writeFile(t, sourceDir, "src/c.txt", "ccc")

	log = &logger{verbose: false, start: time.Now()}

	if err := run(sourceDir, targetDir, "copy", []string{"node_modules"}); err != nil {
		t.Fatalf("run() copy with ignore error = %v", err)
	}

	names := listDir(t, targetDir)
	sort.Strings(names)
	want := []string{"a.txt", "c.txt"}
	if len(names) != len(want) {
		t.Fatalf("目标文件数 = %d, want %d; got %v", len(names), len(want), names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("目标文件[%d] = %q, want %q", i, names[i], n)
		}
	}
}
