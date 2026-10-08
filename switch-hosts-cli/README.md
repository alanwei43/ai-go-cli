# switch-hosts-cli

`switch-hosts-cli` 是一个使用 Go 编写的命令行工具，提供三类能力：

- `subscribe`：定时拉取远程 hosts 内容，并写入本机 hosts 文件的命名区块
- `sync`：定时采集本机局域网 IP，并通过 HTTP 接口上报到远端服务
- `service`：定时同步本机 IP 并订阅远程 hosts，可在 Linux 下安装为 systemd 服务

项目适合以下场景：

- 多台机器统一订阅一份远程 hosts 配置
- 将当前设备的主机名和内网 IP 自动同步到中心服务
- 通过定时任务持续刷新 hosts 或上报网络信息

## 功能特性

- 使用 Go 实现，依赖简单
- 自动识别系统 hosts 文件路径
- 更新 hosts 时使用命名区块，避免覆盖整份文件
- 支持周期性执行，无需额外定时器
- 提供跨平台构建脚本

## 命令行使用教程

### 命令总览

```bash
hosts subscribe --name <name> --interval <interval> <url>
hosts sync --host-name <host-name> --ip-prefix <ip-prefix> --interval <interval> --method <method> <url>
hosts service <url> --interval <seconds> [--add-systemd]
hosts service --del-systemd
```

说明：

- 前台模式启动后会先立即执行一次
- 之后会按 `--interval` 持续循环执行，未指定时默认每 300 秒执行一次
- 如需停止，直接按 `Ctrl+C`
- 如果请求 `<url>` 失败，仅在终端打印失败日志，程序不要退出，等待下次 interval 继续请求

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
- 写入前自动生成 hosts 备份文件，备份目录为:
  - Linux/macOS: `/etc/hosts_backup`
  - Windows: `C:\Windows\System32\drivers\etc\hosts_backup`
- 在 Linux 和 macOS 下默认操作 `/etc/hosts`
- 在 Windows 下默认操作 `C:\Windows\System32\drivers\etc\hosts`

### 2. sync

`sync` 用于扫描本机符合指定 CIDR 的 IPv4 地址，然后通过 HTTP 请求同步到远端接口。

#### 用法

```bash
hosts sync <url> --host-name <host-name> --ip-prefix <cidr> --interval <seconds> --method <method>
```

#### 参数说明

- `<url>`：必须参数，远端同步接口地址
- `--host-name`：可选参数，主机名，选填；未指定时自动使用当前系统主机名
- `--ip-prefix`：可选参数，用于筛选本地 IP 的 CIDR，默认 `192.168.0.0/16`
- `--interval`：可选参数，同步间隔，单位秒，默认 `300`
- `--method`：可选参数，请求方法，默认 `PUT`

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
  "ip": ["192.168.1.20"]
}
```

`ip` 字段为数组，仅包含本次采集并按 ASCII 排序后的第一个 IP。

#### 行为说明

- 仅采集 IPv4 地址
- 只采集命中 `--ip-prefix` 指定网段的地址，去重后按 ASCII 排序
- 每次采集后会在控制台打印所有符合条件的 IP，并取排序后的第一个 IP 上报
- 上报时会在控制台打印传给远程接口的完整数据
- 当本次取到的 IP 与上次上报的 IP 相同时，跳过本次请求，并打印 `系统IP未发生变化: <IP>, 忽略同步操作`
- 当请求失败（网络错误或服务端返回非 `2xx` 状态码）时，仅在控制台打印错误日志，不退出程序、不立即重试，等待下一个 `--interval` 周期再次执行

### 3. service

`service` 相当于 `sync` + `subscribe`，使用同一 URL 和同一个执行周期，同时上报本机 IP 并更新本地 hosts。

#### 用法

```bash
hosts service <url> --interval <seconds> [--add-systemd]
hosts service [<url>] --del-systemd
```

#### 参数说明

- `<url>`：前台运行和 `--add-systemd` 时必填，删除服务时可省略；接口须支持 `PUT` 接收同步 JSON，以及 `GET` 返回 hosts 文本
- `--interval`：可选，同步和订阅间隔，单位秒，默认 `300`，前台运行和添加服务时必须大于 `0`；删除服务时忽略
- `--add-systemd`：可选布尔参数，默认关闭；创建、启用并启动 `switch-hosts.service`，已有服务则显示状态和日志
- `--del-systemd`：可选布尔参数，默认关闭；停止、禁用并删除 `switch-hosts.service`，无需 URL
- `--add-systemd` 和 `--del-systemd` 互斥，只能传递一个；两个参数均仅支持 Linux，其他系统会在控制台报错并返回非零退出码

#### 前台运行

```bash
sudo ./hosts service https://example.com/api/hosts --interval 60
```

- 启动后立即执行一次，之后按指定间隔循环；按 `Ctrl+C` 停止
- 同步使用系统主机名、`192.168.0.0/16` 网段和 `PUT` 方法，请求体与 `sync` 相同；本命令不提供 `--host-name`、`--ip-prefix` 或 `--method` 参数
- 每个周期先尝试上报，再通过 `GET` 拉取同一 URL 的 hosts 内容；IP 未变化时仅跳过上报，没有匹配 IP 或上报失败时仍执行订阅
- 订阅使用 URL 的 SHA-256 摘要作为区块名称，更新和备份规则与 `subscribe` 相同
- 网络错误或非 `2xx` 响应只打印错误日志，等待下一周期；上报成功后才记录上次 IP，确保失败的上报能够在下一周期重试

#### Linux systemd 服务

首次安装需以 root 身份运行，且系统必须安装并运行 systemd，提供 `systemctl` 和 `journalctl`：

```bash
sudo ./hosts service https://example.com/api/hosts --interval 60 --add-systemd
```

如果服务不存在，命令会：

1. 获取当前命令的可执行文件绝对路径，直接将其写入服务文件的 `ExecStart`，不复制程序
2. 创建 `/etc/systemd/system/switch-hosts.service`，使用 `User=root` 和 `Group=root`，以便写入 `/etc/hosts` 和 `/etc/hosts_backup`
3. 执行 `systemctl daemon-reload` 及 `systemctl enable --now switch-hosts.service`，立即启动并设为开机启用，然后显示状态和日志

服务内部运行前台 `service` 命令，沿用创建时的 URL 和间隔。服务在网络上线后启动，进程异常退出后自动重启；日志写入 journal。[systemd 服务配置说明](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml)

如果 `switch-hosts.service` 已存在，再次执行 `--add-systemd` 只显示是否启用、是否正在运行、加载状态及最近 50 条日志，不覆盖配置或程序，不启用或重启服务；本次传入的 URL 和间隔不会改变已有配置。已有服务位于 systemd 的其他服务目录时也会被识别。

手动管理服务：

```bash
sudo systemctl status switch-hosts.service
sudo journalctl --unit=switch-hosts.service --lines=50 --no-pager
sudo systemctl stop switch-hosts.service
sudo systemctl disable switch-hosts.service
```

如需修改 URL 或间隔，编辑服务文件的 `ExecStart` 后执行 `sudo systemctl daemon-reload` 和 `sudo systemctl restart switch-hosts.service`。更新程序时，先停止服务，在 `ExecStart` 引用的原路径替换新版本并确保执行权限，再启动服务。

创建服务前请将编译后的程序放在长期保留的路径，保证 root 可以执行。服务直接使用该路径，移动、删除程序或清理其构建目录会影响后续启动；`go run` 生成的临时可执行文件在命令结束后可能被删除，创建服务建议使用 `go build` 生成的程序。

安装或启动失败会返回具体错误；若服务文件已创建但加载或启动失败，该文件会保留，可修复后手动启用并启动。查看已有服务无需创建权限，但日志读取受系统权限限制，建议使用 `sudo`。

#### 删除 systemd 服务

以 root 身份执行，两个参数互斥；删除时无需 URL，若提供 URL 或 `--interval`，删除操作会忽略它们：

```bash
sudo ./hosts service --del-systemd
```

命令会停止 `switch-hosts.service`，禁用开机启动，移除 `/etc/systemd/system/switch-hosts.service`，再执行 `systemctl daemon-reload`。服务文件位于其他 systemd 目录时，会通过 `FragmentPath` 定位并删除对应的 `switch-hosts.service` 文件。服务不存在时打印提示并成功退出；任一步骤失败则返回具体错误。

删除服务后，原可执行文件和已写入的 hosts 内容仍会保留。

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

仓库根目录提供了 `build.sh`，会为所有工具生成以下目标平台的二进制文件：

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`
- `windows/amd64`
- `windows/arm64`

在仓库根目录执行：

```bash
./build.sh
```

本工具的生成结果位于 `switch-hosts-cli/build/` 目录，例如：

```text
switch-hosts-cli/build/switch-hosts-cli-linux-amd64
switch-hosts-cli/build/switch-hosts-cli-darwin-arm64
switch-hosts-cli/build/switch-hosts-cli-windows-amd64.exe
```

### 构建脚本说明

`build.sh` 的行为包括：

- 清理各工具旧的 `build/` 目录
- 使用 `CGO_ENABLED=0` 进行静态构建
- 使用 `-trimpath -ldflags="-s -w"` 减小二进制体积

## 发布流程

仓库包含 GitHub Actions 工作流 `.github/workflows/release.yml`，在推送到 `master` 分支时会自动：

- 安装 Go 环境
- 执行 `./build.sh`
- 将各工具 `build/` 下产物上传到 GitHub Release

## 注意事项

- `subscribe` 和 `service` 会直接修改系统 hosts 文件，建议以管理员权限运行
- 写入 hosts 前虽然会生成备份，但仍建议先在测试环境验证
- `--interval` 必须大于 `0`
- 远程接口异常、网络错误或返回非 `2xx` 时，仅在控制台打印错误日志，不退出程序，等待下一个周期重试

## 许可证

如需补充许可证信息，请在仓库中增加对应的 `LICENSE` 文件。
