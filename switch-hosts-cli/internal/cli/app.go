package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"switch-hosts-cli/internal/hostsfile"
	"switch-hosts-cli/internal/netutil"
)

const defaultIntervalSeconds = 300

type App struct {
	stdout     io.Writer
	stderr     io.Writer
	httpClient *http.Client
	hostnameFn func() (string, error)
	hostsPath  string
	sleepFn    func(time.Duration)
}

type syncPayload struct {
	HostName string   `json:"hostName"`
	IP       []string `json:"ip"`
}

func NewApp() *App {
	return &App{
		stdout: NewTimestampWriter(os.Stdout),
		stderr: NewTimestampWriter(os.Stderr),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		hostnameFn: os.Hostname,
		hostsPath:  hostsfile.SystemHostsPath(),
		sleepFn:    time.Sleep,
	}
}

func (a *App) NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "switch-hosts-cli",
		Short: "Hosts 文件管理工具",
		Long: `switch-hosts-cli 是一个 hosts 文件管理工具。

支持两种模式：
  subscribe - 订阅远程 hosts 内容并更新到本地 hosts 文件
  sync      - 将本机 IP 信息同步到远程服务器`,
	}

	rootCmd.AddCommand(a.newSubscribeCmd())
	rootCmd.AddCommand(a.newSyncCmd())

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
				content, err := a.fetchRemoteContent(url)
				if err != nil {
					return err
				}

				if err := hostsfile.UpdateNamedBlock(a.hostsPath, subscriptionName, content); err != nil {
					return err
				}

				fmt.Fprintf(a.stdout, "updated hosts subscription %q\n", subscriptionName)
				return nil
			}

			if err := a.loopWithInterval(time.Duration(interval)*time.Second, run); err != nil {
				fmt.Fprintln(a.stderr, err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "订阅名称")
	cmd.Flags().IntVar(&interval, "interval", defaultIntervalSeconds, "刷新间隔（秒）")

	return cmd
}

func (a *App) newSyncCmd() *cobra.Command {
	var hostName string
	var ipPrefix string
	var interval int
	var method string

	cmd := &cobra.Command{
		Use:   "sync <url>",
		Short: "同步本机 IP 信息到远程服务器",
		Long: `将本机 IP 信息同步到远程服务器。

会定时获取本机在指定 CIDR 范围内的 IP 地址，并通过 HTTP 请求同步到远程服务器。`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			url := args[0]
			resolvedHostName := strings.TrimSpace(hostName)
			if resolvedHostName == "" {
				var err error
				resolvedHostName, err = a.hostnameFn()
				if err != nil {
					fmt.Fprintf(a.stderr, "resolve hostname: %v\n", err)
					os.Exit(1)
				}
			}

			if interval <= 0 {
				fmt.Fprintln(a.stderr, "interval must be greater than 0")
				os.Exit(1)
			}

			httpMethod := strings.ToUpper(strings.TrimSpace(method))
			if httpMethod == "" {
				httpMethod = http.MethodPut
			}

			run := func() error {
				ips, err := netutil.LocalIPv4StringsWithinCIDR(ipPrefix)
				if err != nil {
					return err
				}

				payload := syncPayload{
					HostName: resolvedHostName,
					IP:       ips,
				}

				if err := a.pushHostInfo(httpMethod, url, payload); err != nil {
					return err
				}

				fmt.Fprintf(a.stdout, "synced host %q with %d ip(s)\n", resolvedHostName, len(ips))
				return nil
			}

			if err := a.loopWithInterval(time.Duration(interval)*time.Second, run); err != nil {
				fmt.Fprintln(a.stderr, err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&hostName, "host-name", "", "主机名")
	cmd.Flags().StringVar(&ipPrefix, "ip-prefix", "192.168.0.0/16", "本地 IP 地址的 CIDR 过滤范围")
	cmd.Flags().IntVar(&interval, "interval", defaultIntervalSeconds, "同步间隔（秒）")
	cmd.Flags().StringVar(&method, "method", http.MethodPut, "HTTP 请求方法")

	return cmd
}

func (a *App) loopWithInterval(interval time.Duration, run func() error) error {
	if err := run(); err != nil {
		return err
	}

	for {
		a.sleepFn(interval)
		if err := run(); err != nil {
			return err
		}
	}
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

func (a *App) pushHostInfo(method string, url string, payload syncPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}

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
