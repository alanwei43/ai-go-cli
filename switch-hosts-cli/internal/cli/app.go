package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"switch-hosts-cli/internal/hostsfile"
	"switch-hosts-cli/internal/netutil"
	"switch-hosts-cli/internal/systemservice"
)

const defaultIntervalSeconds = 300

type App struct {
	stdout       io.Writer
	stderr       io.Writer
	httpClient   *http.Client
	hostnameFn   func() (string, error)
	localIPsFn   func(string) ([]string, error)
	hostsPath    string
	sleepFn      func(time.Duration)
	addSystemdFn func(string, int) error
	delSystemdFn func() error
}

type syncPayload struct {
	HostName string   `json:"hostName"`
	IP       []string `json:"ip"`
}

func NewApp() *App {
	app := &App{
		stdout: NewTimestampWriter(os.Stdout),
		stderr: NewTimestampWriter(os.Stderr),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		hostnameFn: os.Hostname,
		localIPsFn: netutil.LocalIPv4StringsWithinCIDR,
		hostsPath:  hostsfile.SystemHostsPath(),
		sleepFn:    time.Sleep,
	}
	manager := systemservice.NewManager(app.stdout)
	app.addSystemdFn = manager.Add
	app.delSystemdFn = manager.Delete
	return app
}

func (a *App) NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "switch-hosts-cli",
		Short: "Hosts 文件管理工具",
		Long: `switch-hosts-cli 是一个 hosts 文件管理工具。

支持三种模式：
  subscribe - 订阅远程 hosts 内容并更新到本地 hosts 文件
  sync      - 将本机 IP 信息同步到远程服务器
  service   - 同步本机 IP 并订阅 hosts，可安装为 Linux systemd 服务`,
	}

	rootCmd.AddCommand(a.newSubscribeCmd())
	rootCmd.AddCommand(a.newSyncCmd())
	rootCmd.AddCommand(a.newServiceCmd())

	return rootCmd
}

func (a *App) newSubscribeCmd() *cobra.Command {
	var name string
	var interval int

	cmd := &cobra.Command{
		Use:   "subscribe <url>",
		Short: "订阅远程 hosts 内容",
		Long: `订阅远程 hosts 内容并更新到本地 hosts 文件。

会定时从指定 URL 获取 hosts 内容，将内容写入本地 hosts 文件的命名块中。`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			url := args[0]
			subscriptionName := name
			if subscriptionName == "" {
				subscriptionName = hashText(url)
			}

			if interval <= 0 {
				fmt.Fprintln(a.stderr, "interval must be greater than 0")
				os.Exit(1)
			}

			run := func() error {
				return a.updateSubscription(url, subscriptionName)
			}

			a.loopWithInterval(time.Duration(interval)*time.Second, run)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "订阅名称")
	cmd.Flags().IntVar(&interval, "interval", defaultIntervalSeconds, "刷新间隔（秒）")

	return cmd
}

func (a *App) newSyncCmd() *cobra.Command {
	var hostName, ipPrefix, method string
	var interval int
	cmd := &cobra.Command{
		Use:   "sync <url>",
		Short: "同步本机 IP 信息到远程服务器",
		Long: `将本机 IP 信息同步到远程服务器。

会定时获取本机在指定 CIDR 范围内的 IP 地址，取排序后的第一个 IP 通过 HTTP 请求同步到远程服务器；
若与上次成功上报的 IP 相同则跳过本次上报请求。请求失败仅打印日志，等待下一周期重试。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if interval <= 0 {
				return fmt.Errorf("interval must be greater than 0")
			}
			run, err := a.newSyncRun(args[0], hostName, ipPrefix, method)
			if err != nil {
				return err
			}
			a.loopWithInterval(time.Duration(interval)*time.Second, run)
			return nil
		},
	}
	cmd.Flags().StringVar(&hostName, "host-name", "", "主机名，默认使用系统主机名")
	cmd.Flags().StringVar(&ipPrefix, "ip-prefix", "192.168.0.0/16", "本地 IP 地址的 CIDR 过滤范围")
	cmd.Flags().IntVar(&interval, "interval", defaultIntervalSeconds, "同步间隔（秒），必须大于 0")
	cmd.Flags().StringVar(&method, "method", http.MethodPut, "HTTP 请求方法")
	return cmd
}

func (a *App) newServiceCmd() *cobra.Command {
	var interval int
	var addSystemd, delSystemd bool
	cmd := &cobra.Command{
		Use:   "service [url]",
		Short: "同步本机 IP 并订阅远程 hosts",
		Long: `启动时及每个周期先同步本机 IP，再从同一 URL 订阅 hosts 内容。

使用系统主机名、192.168.0.0/16 网段和 PUT 上报；使用 GET 获取 hosts 文本。
即使没有匹配 IP、IP 未变化或上报失败，也会执行订阅。请求失败仅打印日志，等待下一周期重试。
--add-systemd 仅支持 Linux：以 root 创建、启用并启动 switch-hosts.service，直接使用当前可执行文件路径；
已有服务则显示启用状态、运行状态及最近 50 条日志，不修改服务。
--del-systemd 仅支持 Linux：以 root 停止、禁用并删除 switch-hosts.service，无需 URL。
两个参数互斥。前台运行及 --add-systemd 必须提供 URL。`,
		Args: func(cmd *cobra.Command, args []string) error {
			if delSystemd {
				return cobra.MaximumNArgs(1)(cmd, args)
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if delSystemd {
				return a.delSystemdFn()
			}
			if interval <= 0 {
				return fmt.Errorf("interval must be greater than 0")
			}
			url := args[0]
			if addSystemd {
				return a.addSystemdFn(url, interval)
			}
			run, err := a.newSyncRun(url, "", "192.168.0.0/16", http.MethodPut)
			if err != nil {
				return err
			}
			a.loopWithInterval(time.Duration(interval)*time.Second, func() error {
				return errors.Join(run(), a.updateSubscription(url, hashText(url)))
			})
			return nil
		},
	}
	cmd.Flags().IntVar(&interval, "interval", defaultIntervalSeconds, "同步和订阅间隔（秒），必须大于 0")
	cmd.Flags().BoolVar(&addSystemd, "add-systemd", false, "仅限 Linux：创建、启用并启动 systemd 服务；已有服务则显示状态和最近日志")
	cmd.Flags().BoolVar(&delSystemd, "del-systemd", false, "仅限 Linux：停止、禁用并删除 systemd 服务，无需 URL")
	cmd.MarkFlagsMutuallyExclusive("add-systemd", "del-systemd")
	return cmd
}

func (a *App) newSyncRun(url, hostName, ipPrefix, method string) (func() error, error) {
	resolvedHostName := strings.TrimSpace(hostName)
	if resolvedHostName == "" {
		var err error
		resolvedHostName, err = a.hostnameFn()
		if err != nil {
			return nil, fmt.Errorf("resolve hostname: %w", err)
		}
	}
	httpMethod := strings.ToUpper(strings.TrimSpace(method))
	if httpMethod == "" {
		httpMethod = http.MethodPut
	}
	var lastIP string
	return func() error {
		// 每次重新获取，不缓存 IP 地址；netutil 内部已去重并按 ASCII 排序。
		ips, err := a.localIPsFn(ipPrefix)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "匹配的 IP 地址(%d): %s\n", len(ips), joinIPs(ips))
		if len(ips) == 0 {
			fmt.Fprintln(a.stdout, "未找到符合 IP 网段的 IP 地址，跳过本次同步")
			return nil
		}
		currentIP := ips[0]
		if currentIP == lastIP {
			fmt.Fprintf(a.stdout, "系统IP未发生变化: %s, 忽略同步操作\n", currentIP)
			return nil
		}
		body, err := json.Marshal(syncPayload{HostName: resolvedHostName, IP: []string{currentIP}})
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		fmt.Fprintf(a.stdout, "传给远程接口的数据: %s\n", body)
		if err := a.pushHostInfo(httpMethod, url, body); err != nil {
			return err
		}
		lastIP = currentIP
		return nil
	}, nil
}

// loopWithInterval 立即执行一次 run，随后按 interval 周期性执行。
// 单次 run 失败（含接口调用失败）时仅在控制台打印日志，不退出程序、不立即重试，
// 而是等待下一个 interval 再次执行。
func (a *App) loopWithInterval(interval time.Duration, run func() error) {
	if err := run(); err != nil {
		fmt.Fprintln(a.stderr, err)
	}

	for {
		a.sleepFn(interval)
		if err := run(); err != nil {
			fmt.Fprintln(a.stderr, err)
		}
	}
}

func (a *App) updateSubscription(url string, name string) error {
	content, err := a.fetchRemoteContent(url)
	if err != nil {
		return err
	}
	if err := hostsfile.UpdateNamedBlock(a.hostsPath, name, content); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "updated hosts subscription %q\n", name)
	return nil
}

func (a *App) fetchRemoteContent(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request remote hosts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("request remote hosts failed: status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read remote hosts response: %w", err)
	}

	return string(body), nil
}

func (a *App) pushHostInfo(method string, url string, body []byte) error {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sync host info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sync host info failed: status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return nil
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func joinIPs(ips []string) string {
	if len(ips) == 0 {
		return "(none)"
	}
	return strings.Join(ips, ", ")
}
