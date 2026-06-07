# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

Go CLI 工具集合，每个子目录是独立项目，拥有自己的 `go.mod`。所有工具由 AI 生成。

## 构建

```bash
# 构建所有工具（6 平台: linux/darwin/windows × amd64/arm64）
./build.sh

# 单个项目构建
cd <project-dir> && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o <name> .

# 单个项目运行
cd <project-dir> && go run . [args]
```

构建产物位于各子目录的 `build/` 下，命名格式: `<工具名>-<os>-<arch>[.exe]`


## 代码约定

以下代码约定，是全局约定，所有CLI项目都要遵守以下约定

- CLI框架解析命令行参数时，优先使用类库 `cobra`
- 每个命令行项目都要支持 `--help` 参数，输出详细的命令行参数信息
- 模块路径使用项目名，严禁使用 `github.com` 或者 `codeberg.org` 远程Git仓库名称

## 遵守原则

修改代码要遵守以下原则:

- 每次修改完代码，都要执行一次 build.sh 构建，保证所有项目都能构建成功
- 每次修改完代码, 如果有单元测试，还要保证单元测试能正常运行通过
