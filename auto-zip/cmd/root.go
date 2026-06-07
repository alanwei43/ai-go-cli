package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"auto-zip/internal/scheduler"
	"auto-zip/internal/zipper"

	"github.com/spf13/cobra"
)

var (
	// 命令行参数
	passwordFile string
	targetDir    string
	cronExpr     string
)

// rootCmd 根命令
var rootCmd = &cobra.Command{
	Use:   "auto-zip [directory]",
	Short: "定时对指定目录进行 zip 打包",
	Long: `auto-zip 是一个命令行工具，用于定时对指定目录进行 zip 打包。

支持密码加密、自定义输出路径和 cron 表达式调度。
打包时不进行压缩，仅存储文件。`,
	Example: `  # 每天的10点，自动把 ./note 目录打包成 zip 压缩包
  auto-zip ./note --password ./password.txt --target ./archives/note --cron "0 0 10 * * ?"

  # 每天的当前时间自动把 blog 文件夹打包成压缩包
  auto-zip ./blog --target /data/archives`,
	Args: cobra.ExactArgs(1),
	Run:  runZip,
}

func init() {
	rootCmd.Flags().StringVar(&passwordFile, "password", "", "密码文件路径，文件内容作为 zip 压缩密码")
	rootCmd.Flags().StringVarP(&targetDir, "target", "t", "", "zip 文件存放路径（必填）")
	rootCmd.Flags().StringVar(&cronExpr, "cron", "", "cron 表达式，格式: 秒 分 时 日 月 周（可选，默认每天当前时间执行一次）")

	rootCmd.MarkFlagRequired("target")
}

// Execute 执行命令
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// runZip 主逻辑
func runZip(cmd *cobra.Command, args []string) {
	srcDir := args[0]

	// 验证源目录
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "错误: 源目录不存在: %s\n", srcDir)
		os.Exit(1)
	}

	// 读取密码（如果提供）
	var password string
	if passwordFile != "" {
		data, err := os.ReadFile(passwordFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: 无法读取密码文件: %s\n", err)
			os.Exit(1)
		}
		password = string(data)
		// 去除末尾换行符
		if len(password) > 0 && password[len(password)-1] == '\n' {
			password = password[:len(password)-1]
		}
	}

	// 解析 cron 表达式
	if cronExpr == "" {
		cronExpr = scheduler.GetDefaultCronExpr()
		fmt.Printf("未指定 cron 表达式，使用默认调度: %s（每天当前时间执行）\n", cronExpr)
	} else {
		if err := scheduler.ValidateCronExpr(cronExpr); err != nil {
			fmt.Fprintf(os.Stderr, "错误: %s\n", err)
			os.Exit(1)
		}
	}

	// 创建调度器
	sched := scheduler.New()

	// 定义打包任务
	zipTask := func() {
		fmt.Printf("\n[%s] 开始打包目录: %s\n", getNowTime(), srcDir)
		zipPath, err := zipper.CreateZip(srcDir, targetDir, password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "打包失败: %s\n", err)
			return
		}
		fmt.Printf("[%s] 打包完成: %s\n", getNowTime(), zipPath)
	}

	// 添加任务
	if err := sched.AddTask(cronExpr, zipTask); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %s\n", err)
		os.Exit(1)
	}

	// 立即执行一次
	zipTask()

	// 启动调度器
	sched.Start()
	defer sched.Stop()

	fmt.Printf("\n调度器已启动，cron 表达式: %s\n", cronExpr)
	fmt.Println("按 Ctrl+C 退出...")

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n正在退出...")
}

// getNowTime 获取当前时间字符串
func getNowTime() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
