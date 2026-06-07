# switch-hosts-cli

`switch-hosts-cli` 是一个使用 Go 编写的命令行工具，提供两类能力：

- `subscribe`：定时拉取远程 hosts 内容，并写入本机 hosts 文件的命名区块
- `sync`：定时采集本机局域网 IP，并通过 HTTP 接口上报到远端服务

项目适合以下场景：

- 多台机器统一订阅一份远程 hosts 配置
- 将当前设备的主机名和内网 IP 自动同步到中心服务
- 通过定时任务持续刷新 hosts 或上报网络信息

## 功能特性

- 使用 Go 实现，依赖简单
- 自动识别系统 hosts 文件路径
- 更新 hosts 时使用命名区块，避免覆盖整份文件
- 写入前自动生成 hosts 备份文件
- 支持周期性执行，无需额外定时器
- 提供跨平台构建脚本


## 命令行使用教程

### 命令总览

```bash
hosts subscribe --name <name> --interval <interval> <url>
hosts sync --host-name <host-name> --ip-prefix <ip-prefix> --interval <interval> --method <method> <url>
```

说明：

- 程序启动后会先立即执行一次
- 如果设置了 `--interval`，之后会按间隔持续循环执行
- 如需停止，直接按 `Ctrl+C`

### 1. subscribe

`subscribe` 用于从远程地址拉取 hosts 文本，并更新到本机 hosts 文件中。

#### 用法

```bash
hosts subscribe <url> --name <name> --interval <seconds>
```

#### 参数说明

- `<url>`：远程 hosts 文件地址，必填
- `--name`：写入 hosts 时使用的区块名称，选填；未指定时会自动使用 URL 的 SHA-256 摘要
- `--interval`：拉取间隔，单位秒，默认 `300`

#### 示例

每 5 分钟拉取一次远程 hosts 配置，并写入名称为 `office` 的区块：

```bash
sudo ./hosts subscribe https://example.com/hosts.txt --name office --interval 300
```

执行后，本机 hosts 文件中会生成类似内容：

```text
# switch-hosts-cli start office
127.0.0.1 example.local
192.168.1.10 api.internal
# switch-hosts-cli end office
```

#### 行为说明

- 如果对应命名区块已存在，会原地替换该区块内容
- 如果区块不存在，会追加到 hosts 文件末尾
- 每次写入前都会在同目录生成一个备份文件
- 在 Linux 和 macOS 下默认操作 `/etc/hosts`
- 在 Windows 下默认操作 `C:\Windows\System32\drivers\etc\hosts`

### 2. sync

`sync` 用于扫描本机符合指定 CIDR 的 IPv4 地址，然后通过 HTTP 请求同步到远端接口。

#### 用法

```bash
hosts sync <url> --host-name <host-name> --ip-prefix <cidr> --interval <seconds> --method <method>
```

#### 参数说明

- `<url>`：远端同步接口地址，必填
- `--host-name`：主机名，选填；未指定时自动使用当前系统主机名
- `--ip-prefix`：用于筛选本地 IP 的 CIDR，默认 `192.168.0.0/16`
- `--interval`：同步间隔，单位秒，默认 `300`
- `--method`：请求方法，默认 `PUT`

#### 示例

每 60 秒扫描本机 `192.168.0.0/16` 范围内的 IP，并通过 `PUT` 上报：

```bash
./hosts sync https://example.com/api/hosts --host-name dev-mac --ip-prefix 192.168.0.0/16 --interval 60 --method PUT
```

如果希望自动读取当前机器主机名，可以省略 `--host-name`：

```bash
./hosts sync https://example.com/api/hosts --ip-prefix 10.0.0.0/8 --interval 120
```

#### 请求体格式

`sync` 会发送 JSON 数据，格式如下：

```json
{
  "hostName": "dev-mac",
  "ip": ["192.168.1.20", "192.168.1.21"]
}
```

#### 行为说明

- 仅采集 IPv4 地址
- 只会上报命中 `--ip-prefix` 指定网段的地址
- 会自动去重并排序 IP 列表
- 当服务端返回非 `2xx` 状态码时，程序会直接退出并打印错误

## 常见使用场景

### 场景一：订阅团队统一 hosts

```bash
sudo ./hosts subscribe https://intranet.example.com/dev-hosts.txt --name team-dev --interval 300
```

适用于团队维护统一测试域名映射的场景。

### 场景二：将开发机内网 IP 同步到中心服务

```bash
./hosts sync https://registry.example.com/api/nodes --host-name devbox-01 --ip-prefix 10.10.0.0/16 --interval 30 --method POST
```

适用于动态办公网络或多网卡环境中，自动上报当前可用内网地址。

## 构建教程

### 本地构建

在项目根目录执行：

```bash
go build -o hosts .
```

构建完成后会生成当前平台可执行文件 `hosts`。

### 运行测试

```bash
go test ./...
```

### 多平台构建

项目提供了 `build.sh`，会生成以下目标平台的二进制文件：

- `linux/amd64`
- `darwin/arm64`
- `windows/amd64`

执行方式：

```bash
chmod +x build.sh
./build.sh
```

生成结果位于 `dist/` 目录，例如：

```text
dist/hosts-linux-amd64
dist/hosts-darwin-arm64
dist/hosts-windows-amd64.exe
```

### 构建脚本说明

`build.sh` 的行为包括：

- 清理旧的 `dist/` 目录
- 使用 `CGO_ENABLED=0` 进行静态构建
- 使用 `-trimpath -ldflags="-s -w"` 减小二进制体积

## 发布流程

项目包含 GitHub Actions 工作流 `.github/workflows/release.yml`，在推送到 `master` 分支时会自动：

- 安装 Go 环境
- 执行 `./build.sh`
- 将 `dist/` 下产物上传到 GitHub Release

## 注意事项

- `subscribe` 会直接修改系统 hosts 文件，建议以管理员权限运行
- 写入 hosts 前虽然会生成备份，但仍建议先在测试环境验证
- `--interval` 必须大于 `0`
- 远程接口异常、网络错误或返回非 `2xx` 时，程序会退出

## 许可证

如需补充许可证信息，请在仓库中增加对应的 `LICENSE` 文件。
