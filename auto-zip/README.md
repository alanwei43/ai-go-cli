# auto-zip

一个用于定时对指定目录进行 zip 打包的命令行工具。

## 功能特性

- 启动时立即打包指定目录一次，随后根据 `--cron` 参数决定定时自动打包
- 支持密码加密（兼容标准 unzip）
- 支持 cron 表达式调度
- Linux 支持安装、启用和删除 systemd 系统服务
- 仅打包不压缩（Store 模式）
- 仅需支持 Linux/macOS

## 使用方法

```
auto-zip [directory] --target <save-path> [options]
auto-zip --del-systemd <service-name>
```

### 参数说明

| 参数 | 必填 | 说明 |
|------|------|------|
| `[directory]` | 打包/安装服务时 | 需要打包的目录路径，支持绝对路径和相对路径 |
| `--target, -t` | 打包/安装服务时 | zip 文件存放路径 |
| `--password, -p` | 否 | 密码文件路径，文件内容作为 zip 压缩密码 |
| `--cron` | 否 | cron 表达式，格式: `秒 分 时 日 月 周`。启动时立即打包一次，随后按表达式执行；默认每天当前时间执行一次 |
| `--add-systemd <service-name>` | 否 | 仅 Linux：创建、安装、启用并启动 systemd 服务；必须配合目录和 `--target` |
| `--del-systemd <service-name>` | 否 | 仅 Linux：单独使用，禁用、停止并删除本工具创建的服务及相关配置 |
| `--help, -h` | 否 | 显示详细用法和参数 |

程序在前台持续运行，按系统本地时区调度。参数校验通过后，**启动时立即打包一次**，即使传入的 `--cron` 尚未到执行时间；首次打包结束后继续按 cron 调度。未传入 `--cron` 时，后续默认每天在**启动时的时分秒**执行。启动日志会显示调度表达式和下一次定时执行时间。按 `Ctrl+C` 或发送 `SIGTERM` 可退出；正在运行的 `zip` 会被终止，并清理临时文件。

源路径必须是已有目录，目标目录不存在时自动创建。目标目录不能与源目录相同；目标目录位于源目录内部时，自动排除整个目标目录，避免把历史归档和临时文件再次打包。

同一时间只执行一次打包；打包期间错过的调度不补跑。打包失败会输出错误，并等待下一次调度。ZIP 完整生成后才保存为最终文件名，同名文件不会被覆盖。

### systemd 服务管理（仅 Linux）

仅 `--add-systemd` 和 `--del-systemd` 需要 sudo/root 权限，同时要求使用 systemd 的 Linux 系统和可用的 `systemctl`。普通定时打包以及 `--target`、`--password`、`--cron`、`--help` 不要求 sudo；只需对相应文件和目录具备正常读写权限。服务名以英文字母或数字开头，只允许字母、数字、点、下划线和连字符，可带或不带 `.service` 后缀。

```bash
# 校验打包参数，安装、启用开机启动并立即启动服务；当前命令随后退出
sudo /usr/local/bin/auto-zip ./note --target ./archives/note \
  --password ./password.txt --cron "0 0 10 * * ?" --add-systemd note-backup

# 查看状态和日志
systemctl status note-backup.service
journalctl -u note-backup.service

# 单独执行删除：先禁用并停止，再删除服务文件及附加配置
sudo /usr/local/bin/auto-zip --del-systemd note-backup
```

`--add-systemd` 不在当前进程中启动打包调度，而是写入 `/etc/systemd/system/<service-name>.service`，执行 `systemctl daemon-reload` 和 `systemctl enable --now`。安装时会校验源目录、密码文件及 cron，并在需要时创建目标目录。

服务执行当前 `auto-zip` 程序，参数与移除 `--add-systemd <service-name>` 后的命令等价：源目录、目标目录、密码文件都按安装时的当前目录转换为绝对路径，保留传入的 cron 和密码文件选项。路径中的中文、空格、引号、`$` 和 `%` 会按 systemd 语法处理。服务以 root 运行，工作目录为 `/`。使用位于固定路径的已编译程序安装服务；不要通过 `go run` 安装，临时可执行文件可能随后消失。

systemd 对程序自身的路径有限制：`auto-zip` 可执行文件路径不能包含引号、反斜杠或控制字符；这些限制不影响源目录、目标目录和密码文件参数中的引号或反斜杠。

服务每次启动时都会立即打包一次，然后按 cron 调度。未传入 `--cron` 时，服务每次启动都按该次启动的时分秒设定每日调度；要固定每日执行时间，请显式传入 `--cron`。

服务配置 `Restart=on-failure`、`RestartSec=1h`，进程失败退出后等待 1 小时自动重启。正常退出或主动停止不触发自动重启。单次打包失败仍由程序按下一次 cron 调度重试。

两个管理参数不能同时使用；`--del-systemd` 不接受目录或打包参数。安装不会覆盖已有同名服务。如果启用或启动失败，服务文件保留，命令会报错，可检查日志或用 `--del-systemd` 清理。删除会移除启用链接、服务文件和 `<service-name>.service.d/` 附加配置，并重新加载 systemd；不删除源文件、ZIP 归档或程序。为避免误删，删除入口仅处理带有本工具标记的服务文件；文件不存在时无需重复删除。

安装时通过 `systemctl list-unit-files --type=service --full --no-legend --no-pager` 获取完整服务列表，再精确匹配服务名，检查包括系统、厂商和运行时目录中的同名服务。部分 systemd 版本在按名称查询不存在的服务时会返回退出码 1 且没有错误输出；安装检查避免使用这种查询方式。服务列表查询真正失败时仍会终止安装并报错。

### Cron 表达式格式

使用 6 字段格式（支持秒）：

```
秒 分 时 日 月 周
└──┴──┴──┴──┴──┴──
 0-59 0-59 0-23 1-31 1-12 0-6 (0=周日)

特殊字符:
* : 任意值
, : 值列表
- : 范围
/ : 间隔
? : 日或周的不指定
```

## 使用示例

```bash
# 立即打包 ./note 目录一次，之后每天上午 10 点打包
auto-zip ./note --password ./password.txt --target ./archives/note --cron "0 0 10 * * ?"

# 立即打包 ./blog 目录一次，之后每天当前时间打包（默认行为）
auto-zip ./blog --target /data/archives

# 立即打包一次，之后每 5 分钟打包
auto-zip ./data --target ./backup --cron "0 */5 * * * *"

# 立即打包一次，之后每周一凌晨 2 点打包
auto-zip ./project --target ./backup --cron "0 0 2 * * 1"
```

## 输出文件命名

打包生成的 zip 文件名格式：

```
{目录名}_{年-月-日-时-分-秒}.zip
```

示例：`note_2026-03-21-10-00-00.zip`

ZIP 内保留源目录名称、子目录、隐藏文件和空目录。符号链接保存为链接，不跟随其指向的文件或目录。源目录是文件系统根目录时，文件名前缀使用 `root`，ZIP 内直接存放根目录的内容。

## 密码保护

使用 `--password` 参数指定密码文件：

启动时读取文件内容，去除末尾的 CR/LF 换行，保留密码前后的空格；空密码或包含 NUL 字符的密码会报错。运行期间修改密码文件需重启程序才能生效。

```bash
# 创建密码文件
echo "your_password" > password.txt

# 使用密码打包
auto-zip ./note --password ./password.txt --target ./archives
```

解压带密码的 zip 文件：

```bash
unzip -P your_password note_2026-03-21-10-00-00.zip
```

## 压缩方式

程序使用 Store 模式（不压缩），仅将文件打包存储，底层使用系统原生 `zip -r0` 命令进行压缩，不要安装第三方类型压缩。

密码使用系统 `zip -P` 的标准 ZIP 加密，兼容 `unzip -P`。程序直接调用系统命令，不使用第三方 Go 压缩库。忽略 `ZIP` 和 `ZIPOPT` 环境变量，以保持既定的打包方式。

## 开发

### 环境要求

- Go 1.21 或更高版本
- 运行平台：Linux/macOS，需在 `PATH` 中提供系统 `zip`（支持 `-P` 加密）
- 测试还需要系统 `unzip`，用于验证加密归档的兼容性

### 依赖

| 库 | 用途 |
|---|---|
| github.com/spf13/cobra | 命令行参数及帮助 |
| github.com/robfig/cron/v3 | cron 调度 |

### 构建

```bash
# 安装依赖
go mod tidy

# 本地构建
go build -o auto-zip .

# 跨平台构建
GOOS=linux GOARCH=amd64 go build -o auto-zip-linux .
GOOS=darwin GOARCH=amd64 go build -o auto-zip-macos .

# 单元及系统 zip/unzip 集成测试
go test ./...
```
