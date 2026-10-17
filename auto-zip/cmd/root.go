package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"auto-zip/internal/scheduler"
	"auto-zip/internal/systemservice"
	"auto-zip/internal/zipper"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	return newCommand(systemservice.NewManager())
}

type serviceManager interface {
	Add(context.Context, string, []string) error
	Delete(context.Context, string) error
}

func newCommand(services serviceManager) *cobra.Command {
	var target, passwordFile, zipName, expression, addService, deleteService string
	var maxCount int
	command := &cobra.Command{
		Use:     "auto-zip [directory|list.txt] --target <save-path> [options]",
		Short:   "使用系统 zip 定时打包目录或 txt 清单（Store 模式）",
		Long:    "按本地时间的六字段 cron（秒 分 时 日 月 周）定时打包目录或 txt 清单中的文件/目录。\n源路径必须是目录或 .txt 文件；清单每行一个路径，相对路径以 txt 所在目录为基准，忽略空行，每次打包重新读取。\n启动时立即打包一次，然后按 cron 调度；默认每天在启动时的时分秒执行。\n需要 Linux/macOS 以及支持密码加密的系统 zip 命令。\nLinux 可用 --add-systemd 服务名安装并启用服务，或单独用 --del-systemd 服务名删除服务。",
		Example: "  auto-zip ./note -t ./archives -p ./password.txt --cron \"0 0 10 * * ?\"\n  auto-zip ./backup.txt -t ./archives\n  auto-zip ./data -t ./archives --zip-name rustfs-s3\n  sudo auto-zip ./note -t ./archives --cron \"0 0 10 * * ?\" --add-systemd note-backup\n  sudo auto-zip --del-systemd note-backup",
		Args: func(command *cobra.Command, args []string) error {
			add, del := command.Flags().Changed("add-systemd"), command.Flags().Changed("del-systemd")
			if add && del {
				return fmt.Errorf("--add-systemd 与 --del-systemd 不能同时使用")
			}
			if (add || del) && runtime.GOOS != "linux" {
				return fmt.Errorf("systemd 参数仅支持 Linux")
			}
			if del {
				if _, err := systemservice.UnitName(deleteService); err != nil {
					return err
				}
				if len(args) != 0 || command.Flags().Changed("target") || command.Flags().Changed("password") || command.Flags().Changed("zip-name") || command.Flags().Changed("cron") || command.Flags().Changed("max-count") {
					return fmt.Errorf("--del-systemd 必须单独使用，不接受源路径、target、password、zip-name、cron 或 max-count")
				}
				return nil
			}
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return err
			}
			if !command.Flags().Changed("target") || target == "" {
				return fmt.Errorf("必须指定非空 --target 目标目录")
			}
			if add {
				_, err := systemservice.UnitName(addService)
				return err
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, args []string) error {
			if command.Flags().Changed("del-systemd") {
				if err := services.Delete(command.Context(), deleteService); err != nil {
					return err
				}
				unit, _ := systemservice.UnitName(deleteService)
				fmt.Fprintf(command.OutOrStdout(), "已禁用、停止并删除 %s（不存在时无需删除）\n", unit)
				return nil
			}
			now := time.Now()
			schedule, spec, err := scheduler.Parse(expression, now)
			if err != nil {
				return err
			}
			archive, err := zipper.New(args[0], target, passwordFile, zipName, maxCount)
			if err != nil {
				return err
			}
			if command.Flags().Changed("add-systemd") {
				serviceArgs, err := archiveArguments(args[0], target, passwordFile, zipName, expression, command.Flags().Changed("cron"))
				if err != nil {
					return err
				}
				if command.Flags().Changed("max-count") {
					serviceArgs = append(serviceArgs, "--max-count", strconv.Itoa(maxCount))
				}
				if err := services.Add(command.Context(), addService, serviceArgs); err != nil {
					return err
				}
				unit, _ := systemservice.UnitName(addService)
				fmt.Fprintf(command.OutOrStdout(), "已安装、启用并启动 %s，失败后间隔 1 小时自动重启\n", unit)
				return nil
			}
			out := command.OutOrStdout()
			fmt.Fprintf(out, "源路径: %s\n目标目录: %s\n调度: %s（本地时区 %s）\n下次打包: %s\n按 Ctrl+C 退出。\n",
				archive.Source(), archive.Target(), spec, now.Location(), schedule.Next(now).Format(time.RFC3339))
			return scheduler.Run(command.Context(), schedule, func() error {
				path, err := archive.Create(command.Context(), time.Now())
				if err == nil {
					fmt.Fprintf(out, "打包完成: %s\n", path)
				}
				return err
			}, command.ErrOrStderr())
		},
	}
	command.Flags().StringVarP(&target, "target", "t", "", "ZIP 存放目录，必填；不存在时自动创建")
	command.Flags().StringVarP(&passwordFile, "password", "p", "", "密码文件路径；读取内容并去除末尾 CR/LF")
	command.Flags().StringVarP(&zipName, "zip-name", "n", "", "ZIP 文件名前缀，默认使用源目录名或 txt 文件名（去除扩展名）；生成 前缀_年月日时分秒_SHA256.zip（64 位小写哈希）；不能包含路径分隔符或控制字符")
	command.Flags().StringVar(&expression, "cron", "", "六字段 cron：秒 分 时 日 月 周；默认每天启动时的时分秒")
	command.Flags().IntVarP(&maxCount, "max-count", "c", 0, "最多保留的 ZIP 数量，0 表示不限制；打包和哈希重命名完成后，仅统计严格匹配 前缀_年月日时分秒_64位小写SHA256.zip 的文件，超限时删除最旧的历史 ZIP")
	command.Flags().StringVar(&addService, "add-systemd", "", "仅 Linux：服务名；配合目录或 txt 清单和 --target 安装、启用并启动 systemd 服务（需 root）")
	command.Flags().StringVar(&deleteService, "del-systemd", "", "仅 Linux：服务名；单独使用，禁用、停止并删除本工具创建的服务（需 root）")
	command.Flags().BoolP("help", "h", false, "显示详细用法和参数")
	// Explicitly empty values must not silently select defaults.
	command.PreRunE = func(command *cobra.Command, _ []string) error {
		if maxCount < 0 {
			return fmt.Errorf("--max-count 必须为非负整数，0 表示不限制")
		}
		if command.Flags().Changed("cron") && expression == "" {
			return fmt.Errorf("cron 表达式不能为空")
		}
		if command.Flags().Changed("zip-name") && zipName == "" {
			return fmt.Errorf("--zip-name 文件名前缀不能为空")
		}
		return nil
	}
	return command
}

// Rebuild the equivalent archive command without management flags, retaining
// optional flags only when supplied. Absolute paths preserve their meaning
// across systemd starts; unresolved symlinks retain the requested archive name.
func archiveArguments(source, target, passwordFile, zipName, expression string, hasCron bool) ([]string, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("解析服务源路径: %w", err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("解析服务目标目录: %w", err)
	}
	args := []string{source, "--target", target}
	if passwordFile != "" {
		passwordFile, err = filepath.Abs(passwordFile)
		if err != nil {
			return nil, fmt.Errorf("解析服务密码文件: %w", err)
		}
		args = append(args, "--password", passwordFile)
	}
	if zipName != "" {
		args = append(args, "--zip-name", zipName)
	}
	if hasCron {
		args = append(args, "--cron", expression)
	}
	return args, nil
}
