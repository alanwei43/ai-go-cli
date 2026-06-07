# auto-zip

一个用于定时对指定目录进行 zip 打包的命令行工具。

## 功能特性

- 定时自动打包指定目录
- 支持密码加密（兼容标准 unzip）
- 支持 cron 表达式调度
- 仅打包不压缩（Store 模式）
- 跨平台支持（Windows/macOS/Linux）

## 使用方法

```
auto-zip [directory] --target <save-path> [options]
```

### 参数说明

| 参数 | 必填 | 说明 |
|------|------|------|
| `[directory]` | 是 | 需要打包的目录路径，支持绝对路径和相对路径 |
| `--target, -t` | 是 | zip 文件存放路径 |
| `--password` | 否 | 密码文件路径，文件内容作为 zip 压缩密码 |
| `--cron` | 否 | cron 表达式，格式: `秒 分 时 日 月 周`。默认每天当前时间执行一次 |

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
# 每天上午 10 点打包 ./note 目录
auto-zip ./note --password ./password.txt --target ./archives/note --cron "0 0 10 * * ?"

# 每天当前时间打包 ./blog 目录（默认行为）
auto-zip ./blog --target /data/archives

# 每 5 分钟打包一次
auto-zip ./data --target ./backup --cron "0 */5 * * * *"

# 每周一凌晨 2 点打包
auto-zip ./project --target ./backup --cron "0 0 2 * * 1"
```

## 输出文件命名

打包生成的 zip 文件名格式：

```
{目录名}_{年-月-日-时-分-秒}.zip
```

示例：`note_2026-03-21-10-00-00.zip`

## 密码保护

使用 `--password` 参数指定密码文件：

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

程序使用 Store 模式（不压缩），仅将文件打包存储。适用于：

- 文件本身已是压缩格式（如 .jpg, .mp4, .zip）
- 需要快速打包/解压
- CPU 资源受限场景

## 开发

### 环境要求

- Go 1.21 或更高版本

### 依赖

| 库 | 用途 |
|---|---|
| github.com/spf13/cobra | 命令行解析 |
| github.com/robfig/cron/v3 | cron 调度 |
| github.com/yeka/zip | 带密码的 zip 支持 |

### 构建

```bash
# 安装依赖
go mod tidy

# 本地构建
go build -o auto-zip .

# 跨平台构建
GOOS=linux GOARCH=amd64 go build -o auto-zip-linux .
GOOS=darwin GOARCH=amd64 go build -o auto-zip-macos .
GOOS=windows GOARCH=amd64 go build -o auto-zip.exe .
```

## 许可证

MIT License

## Bug

有个bug，加密模式下，中文文件名是乱码。