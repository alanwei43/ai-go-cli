package cmd

import (
	"fmt"
	"os"
	"time"

	"github-release-downloader/internal/github"
)

func listReleases(repository string, limit int) {
	client := github.NewClient()

	releases, err := client.ListReleases(repository, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取 releases 失败: %v\n", err)
		os.Exit(1)
	}

	if len(releases) == 0 {
		fmt.Printf("仓库 %s 没有 release\n", repository)
		return
	}

	fmt.Printf("仓库 %s 最近 %d 条 release:\n\n", repository, len(releases))
	for i, release := range releases {
		pubTime := formatTime(release.PublishedAt)
		fmt.Printf("  %d. %-20s  %s\n", i+1, release.TagName, pubTime)
	}
}

func formatTime(timestamp string) string {
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return timestamp
	}
	return t.Format("2006-01-02 15:04:05")
}
