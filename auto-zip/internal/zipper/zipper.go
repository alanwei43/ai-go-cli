package zipper

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/yeka/zip"
)

// CreateZip 创建 zip 打包文件
// srcDir: 源目录路径
// targetDir: 目标存放目录
// password: 密码（可选，为空则不加密）
// 返回生成的 zip 文件路径
func CreateZip(srcDir, targetDir, password string) (string, error) {
	// 获取源目录的绝对路径
	absSrcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return "", fmt.Errorf("获取源目录绝对路径失败: %w", err)
	}

	// 检查源目录是否存在
	srcInfo, err := os.Stat(absSrcDir)
	if err != nil {
		return "", fmt.Errorf("源目录不存在: %w", err)
	}
	if !srcInfo.IsDir() {
		return "", fmt.Errorf("源路径不是目录: %s", absSrcDir)
	}

	// 确保目标目录存在
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("创建目标目录失败: %w", err)
	}

	// 生成 zip 文件名: {dirName}_{年-月-日-时-分-秒}.zip
	dirName := filepath.Base(absSrcDir)
	timestamp := time.Now().Format("2006-01-02-15-04-05")
	zipFileName := fmt.Sprintf("%s_%s.zip", dirName, timestamp)
	zipFilePath := filepath.Join(targetDir, zipFileName)

	// 创建 zip 文件
	zipFile, err := os.Create(zipFilePath)
	if err != nil {
		return "", fmt.Errorf("创建 zip 文件失败: %w", err)
	}
	defer zipFile.Close()

	// 创建 zip writer
	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// 遍历源目录，添加文件到 zip
	baseDir := filepath.Dir(absSrcDir)
	err = filepath.Walk(absSrcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录本身（只处理文件）
		if info.IsDir() {
			return nil
		}

		// 计算相对路径
		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return fmt.Errorf("计算相对路径失败: %w", err)
		}

		// 创建 zip 文件头
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return fmt.Errorf("创建文件头失败: %w", err)
		}

		// 设置文件名和压缩方式（Store = 不压缩）
		header.Name = relPath
		header.Method = zip.Store // 不压缩，只打包

		// 设置 UTF-8 标志位，解决中文文件名乱码问题
		// bit 11: 如果设置，文件名和注释使用 UTF-8 编码
		header.Flags |= 0x800

		// 创建文件写入器
		var writer io.Writer
		if password != "" {
			// 使用 Encrypt 方法创建加密条目
			// 注意：Encrypt 内部会创建新的 header，所以需要在这里重新设置 UTF-8 标志
			writer, err = zipWriter.Encrypt(relPath, password, zip.StandardEncryption)
			// 设置 UTF-8 标志位，解决中文文件名乱码问题
			// 由于 Encrypt 内部创建 header，我们需要通过其他方式设置
			// yeka/zip 库的 Encrypt 方法支持 UTF-8 文件名
		} else {
			// 非加密模式：直接使用 CreateHeader
			writer, err = zipWriter.CreateHeader(header)
		}

		if err != nil {
			return fmt.Errorf("创建 zip 条目失败: %w", err)
		}

		// 打开源文件
		srcFile, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("打开源文件失败: %w", err)
		}
		defer srcFile.Close()

		// 复制文件内容
		_, err = io.Copy(writer, srcFile)
		if err != nil {
			return fmt.Errorf("写入文件内容失败: %w", err)
		}

		return nil
	})

	if err != nil {
		return "", fmt.Errorf("打包失败: %w", err)
	}

	return zipFilePath, nil
}
