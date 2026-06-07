package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var limit int

var rootCmd = &cobra.Command{
	Use:   "gh-release-downloader [repository] [version]",
	Short: "GitHub Release 文件下载工具",
	Long: `GitHub Release 文件下载 CLI 工具

- repository 是必须参数，是 GitHub 仓库唯一标识
  例如：coder/code-server
- version 是可选参数，标识 GitHub Release 中的版本号

如果只传了 repository，列出该仓库下的 release 版本号
如果同时传了 repository 和 version，下载指定版本的所有 release 产物到当前目录`,
	Args: cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		repository := args[0]

		if len(args) == 1 {
			// 列出 release 版本号
			listReleases(repository, limit)
		} else {
			// 下载指定版本的产物
			version := args[1]
			downloadRelease(repository, version)
		}
	},
}

func init() {
	rootCmd.Flags().IntVarP(&limit, "number", "n", 10, "显示的 release 数量")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
