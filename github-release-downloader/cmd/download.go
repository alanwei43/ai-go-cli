package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github-release-downloader/internal/downloader"
	"github-release-downloader/internal/github"
)

func downloadRelease(repository, version string) {
	client := github.NewClient()

	release, err := client.GetReleaseByVersion(repository, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取 release 失败: %v\n", err)
		os.Exit(1)
	}

	if len(release.Assets) == 0 {
		fmt.Printf("版本 %s 没有 release 产物\n", version)
		return
	}

	// 创建下载目录: 仓库名称/版本号
	repoName := filepath.Base(repository)
	downloadDir := filepath.Join(repoName, version)
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "创建目录失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("正在下载 %s@%s 的 %d 个产物到 %s/:\n", repository, version, len(release.Assets), downloadDir)
	for i, asset := range release.Assets {
		fmt.Printf("  %d. %-40s  %s\n", i+1, asset.Name, formatSize(asset.Size))
	}
	fmt.Println()

	d := downloader.New()
	for _, asset := range release.Assets {
		fmt.Printf("下载 %s ... ", asset.Name)
		destPath := filepath.Join(downloadDir, asset.Name)
		if err := d.Download(asset.URL, destPath); err != nil {
			fmt.Printf("失败: %v\n", err)
		} else {
			fmt.Println("完成")
		}
	}

	fmt.Printf("\n所有产物下载完成! 保存目录: %s\n", downloadDir)
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
