# auto-zip

一个用于定时对指定目录或 txt 清单中的文件/目录进行 zip 打包的命令行工具。

## 功能特性

- 启动时立即打包指定目录或 txt 清单中的所有路径到一个 ZIP，随后根据 `--cron` 参数决定定时自动打包
- 支持密码加密（兼容标准 unzip）
- 支持通过 `--zip-name` 自定义 ZIP 文件名前缀，文件名附带完整 ZIP 内容的 SHA-256
- 支持通过 `--max-count` 限制归档数量，打包和哈希重命名完成后，超限时自动删除最旧的同前缀历史 ZIP
- 支持 cron 表达式调度
- Linux 支持安装、启用和删除 systemd 系统服务
- 仅打包不压缩（Store 模式）
- 仅需支持 Linux/macOS

## 使用方法

```
auto-zip [directory|list.txt] --target <save-path> [options]
auto-zip --del-systemd <service-name>
```

### 参数说明

| 参数 | 必填 | 说明 |
|------|------|------|
| `[directory\|list.txt]` | 打包/安装服务时 | 需要打包的目录或 `.txt` 清单路径，支持绝对路径和相对路径；其他类型文件会报错 |
| `--target, -t` | 打包/安装服务时 | zip 文件存放路径 |
| `--password, -p` | 否 | 密码文件路径，文件内容作为 zip 压缩密码 |
| `--zip-name, -n <prefix>` | 否 | ZIP 文件名前缀，默认使用源目录名或 txt 文件名（去除扩展名）；生成 `<prefix>_年月日时分秒_hash.zip` |
| `--max-count, -c <number>` | 否 | 最多保留的 ZIP 数量；非负整数，默认 `0` 表示不限制；打包和哈希重命名完成后，统计包含新归档在内的 ZIP 数量，超限时删除最旧的同前缀历史 ZIP |
| `--cron` | 否 | cron 表达式，格式: `秒 分 时 日 月 周`。启动时立即打包一次，随后按表达式执行；默认每天当前时间执行一次 |
| `--add-systemd <service-name>` | 否 | 仅 Linux：创建、安装、启用并启动 systemd 服务；必须配合目录或 txt 清单和 `--target` |
| `--del-systemd <service-name>` | 否 | 仅 Linux：单独使用，禁用、停止并删除本工具创建的服务及相关配置 |
| `--help, -h` | 否 | 显示详细用法和参数 |

程序在前台持续运行，按系统本地时区调度。参数校验通过后，**启动时立即打包一次**，即使传入的 `--cron` 尚未到执行时间；首次打包结束后继续按 cron 调度。未传入 `--cron` 时，后续默认每天在**启动时的时分秒**执行。启动日志会显示调度表达式和下一次定时执行时间。按 `Ctrl+C` 或发送 `SIGTERM` 可退出；正在运行的 `zip` 会被终止，并清理临时文件。

源路径必须是已有目录或扩展名为 `.txt` 的普通文件（扩展名不区分大小写）。目录按原有方式直接打包；txt 作为路径清单，将各行指定的文件/目录合并打包到同一个 ZIP，而非将 txt 本身作为打包对象。

清单规则：

- 每行一个文件或目录路径，支持绝对路径；相对路径以 **txt 所在目录**为基准，普通运行和 systemd 服务行为一致。
- 使用 UTF-8 文本，支持 UTF-8 BOM、LF/CRLF 换行和末行无换行；忽略空行，去除每行首尾空白，路径中间的空格原样保留。不添加引号，不展开通配符、`~` 或环境变量。
- 启动时校验清单，每次打包前重新读取，修改清单后下一次打包生效。空清单、路径不存在或路径类型不支持时会报错；路径错误包含行号，本次打包失败，不生成部分归档或清理历史归档。
- ZIP 内保留相对于所有条目共同父目录的路径，避免不同目录中的同名文件互相覆盖。例如清单包含 `/data/note` 和 `/data/config/app.yaml`，ZIP 中保存 `note/...` 和 `config/app.yaml`。符号链接保存为链接，不跟随其指向的内容。

目标目录不存在时自动创建。目标目录不能与任何待打包的源目录相同；位于源目录内部时，自动排除整个目标目录，避免把历史归档和临时文件再次打包。

同一时间只执行一次打包；打包期间错过的调度不补跑。打包失败会输出错误，并等待下一次调度。ZIP 完整生成后，流式读取生成文件的全部字节计算 SHA-256，再保存为带哈希的最终文件名；计算哈希失败时清理临时文件，不保存最终归档。同名文件不会被覆盖。

传入 `--max-count N`（`N > 0`）时，启动后的首次打包及后续每次定时打包都会先完成 ZIP 打包、计算 SHA-256 并保存为最终文件名，然后作为最后一步扫描目标目录，统计包含新归档在内的同前缀 ZIP 数量。仅当数量超过 `N` 时，删除超出的最旧历史 ZIP，直至最多保留 `N` 个；本次新生成的归档始终保留。按文件名中的时间戳从旧到新删除，与文件修改时间无关；程序重启后也会统计已有归档。清理仅处理目标目录直接包含、名称严格符合 `<当前前缀>_年月日时分秒_hash.zip`（有效时间戳及 64 位小写十六进制哈希）的普通文件，匹配时先按字面值校验 `<当前前缀>_`，再对剩余部分使用正则 `^([0-9]{14})_[0-9a-f]{64}\.zip$` 校验，并验证日期时间有效。前缀可以包含下划线或正则特殊字符，均按普通字符匹配。旧版不带哈希的 ZIP、其他前缀、其他命名的文件、子目录和符号链接不参与统计，也不会删除。多个任务共用目标目录时，可使用不同的 `--zip-name` 分别保留归档。

打包、计算哈希或保存最终文件名失败时，不执行数量清理，已有归档保持不变。清理失败时保留已生成的新归档，并输出包含新归档路径的错误，之后继续按调度执行；此时归档数量可能暂时超过上限，已删除的历史归档不会恢复。同一前缀每秒只保存一个归档；相同前缀和时间戳且符合上述严格格式的已有归档会在打包前检查并报错。未传入该参数或使用 `--max-count 0` 时不清理历史归档。

### systemd 服务管理（仅 Linux）

仅 `--add-systemd` 和 `--del-systemd` 需要 sudo/root 权限，同时要求使用 systemd 的 Linux 系统和可用的 `systemctl`。普通定时打包以及 `--target`、`--password`、`--zip-name`、`--max-count`、`--cron`、`--help` 不要求 sudo；只需对相应文件和目录具备正常读写权限。服务名以英文字母或数字开头，只允许字母、数字、点、下划线和连字符，可带或不带 `.service` 后缀。

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

`--add-systemd` 不在当前进程中启动打包调度，而是写入 `/etc/systemd/system/<service-name>.service`，执行 `systemctl daemon-reload` 和 `systemctl enable --now`。安装时会校验源目录或 txt 清单、密码文件、ZIP 文件名前缀、归档数量上限及 cron，并在需要时创建目标目录。

服务执行当前 `auto-zip` 程序，参数与移除 `--add-systemd <service-name>` 后的命令等价：源路径、目标目录、密码文件都按安装时的当前目录转换为绝对路径，保留传入的 cron、密码文件、`--zip-name` 和 `--max-count` 选项。路径和文件名前缀中的中文、空格、引号、`$` 和 `%` 会按 systemd 语法处理。服务以 root 运行，工作目录为 `/`。使用位于固定路径的已编译程序安装服务；不要通过 `go run` 安装，临时可执行文件可能随后消失。

systemd 对程序自身的路径有限制：`auto-zip` 可执行文件路径不能包含引号、反斜杠或控制字符；这些限制不影响源路径、目标目录和密码文件参数中的引号或反斜杠。

服务每次启动时都会立即打包一次，然后按 cron 调度。未传入 `--cron` 时，服务每次启动都按该次启动的时分秒设定每日调度；要固定每日执行时间，请显式传入 `--cron`。

服务配置 `Restart=on-failure`、`RestartSec=1h`，进程失败退出后等待 1 小时自动重启。正常退出或主动停止不触发自动重启。单次打包失败仍由程序按下一次 cron 调度重试。

两个管理参数不能同时使用；`--del-systemd` 不接受源路径或打包参数。安装不会覆盖已有同名服务。如果启用或启动失败，服务文件保留，命令会报错，可检查日志或用 `--del-systemd` 清理。删除会移除启用链接、服务文件和 `<service-name>.service.d/` 附加配置，并重新加载 systemd；不删除源文件、ZIP 归档或程序。为避免误删，删除入口仅处理带有本工具标记的服务文件；文件不存在时无需重复删除。

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

# 将 backup.txt 中每行指定的文件/目录合并打包，之后每天当前时间重复打包
# 例如 backup.txt 位于 /data，内容为：
# note
# config/app.yaml
# /var/log/my-app
auto-zip /data/backup.txt --target /data/archives

# 立即打包 ./blog 目录一次，之后每天当前时间打包（默认行为）
auto-zip ./blog --target /data/archives

# 立即打包一次，之后每 5 分钟打包
auto-zip ./data --target ./backup --cron "0 */5 * * * *"

# 指定 ZIP 文件名前缀，生成 rustfs-s3_年月日时分秒_hash.zip
auto-zip ./data --target ./backup --zip-name rustfs-s3 --cron "0 0 10 * * ?"

# 最多保留 7 个同前缀 ZIP；每次打包和哈希重命名完成后，超限时删除最旧的历史归档
auto-zip ./data --target ./backup --max-count 7 --cron "0 0 10 * * ?"

# 使用短参数指定前缀和保留数量，等同于 --zip-name rustfs-s3 --max-count 7
auto-zip ./data -t ./backup -n rustfs-s3 -c 7

# 立即打包一次，之后每周一凌晨 2 点打包
auto-zip ./project --target ./backup --cron "0 0 2 * * 1"
```

## 输出文件命名

打包生成的 zip 文件名格式（未指定 `--zip-name` 时）：

```
{源目录名或清单名（去除扩展名）}_{年月日时分秒}_{hash}.zip
```

txt 清单模式的 ZIP 文件名前缀按以下优先级选择：

1. 显式指定的 `--zip-name`（或 `-n`）参数。
2. txt 清单的文件名（去除 `.txt` 扩展名），不使用清单中条目的名称；文件名仅为 `.txt` 时使用 `list`。

例如，`auto-zip backup.txt -t ./archives --zip-name daily` 生成 `daily_年月日时分秒_hash.zip`；省略 `--zip-name` 则生成 `backup_年月日时分秒_hash.zip`。

示例（`<hash>` 代表完整的 64 位小写十六进制 SHA-256）：`note_20260321100000_<hash>.zip`

传入 `--zip-name rustfs-s3` 后，文件名为 `rustfs-s3_20260321100000_<hash>.zip`。参数仅指定前缀；时间戳使用系统本地时区，格式为 `YYYYMMDDhhmmss`，不含连字符。哈希基于打包完成后的 ZIP 文件本身计算（加密归档使用加密后的文件字节），不是源目录内容的哈希。时间戳、下划线分隔的哈希和 `.zip` 扩展名由程序自动追加。前缀支持中文、空格等普通文件名字符，原样保留；不能是空字符串或全空白，也不能包含 `/`、`\` 或控制字符（如换行、制表符、NUL）。

直接打包目录时，ZIP 内保留源目录名称、子目录、隐藏文件和空目录；清单模式保留各条目相对于共同父目录的路径。`--zip-name` 只改变归档文件名。符号链接保存为链接，不跟随其指向的文件或目录。源目录是文件系统根目录时，默认文件名前缀使用 `root`，也可用 `--zip-name` 指定；ZIP 内直接存放根目录的内容。

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
unzip -P your_password note_20260321100000_*.zip
```

## 压缩方式

**打包必须使用系统原生 `zip` 命令，禁止安装、引入或使用任何第三方 ZIP 包或压缩库。** 此规则同时适用于目录模式和 txt 清单模式。系统未提供 `zip` 时直接报错，需要通过操作系统包管理器安装系统 `zip` 工具。

程序使用 Store 模式（不压缩），通过系统 `zip -r0` 命令将文件打包存储。

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
