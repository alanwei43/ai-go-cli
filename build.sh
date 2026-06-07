#!/bin/bash

# 构建所有 Go CLI 程序
# 产物存放在各子目录的 build/ 下，包含 Linux/Windows/macOS 的 ARM 和 x86 架构

set -e

PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "windows/arm64"
  "darwin/amd64"
  "darwin/arm64"
)

# 查找所有包含 go.mod 的子目录
for mod_file in */go.mod; do
  dir=$(dirname "$mod_file")
  name=$dir
  build_dir="$dir/build"

  echo "==> Building $name ..."

  # 清理并重建输出目录
  rm -rf "$build_dir"
  mkdir -p "$build_dir"

  for platform in "${PLATFORMS[@]}"; do
    IFS="/" read -r goos goarch <<< "$platform"

    # Windows 可执行文件加 .exe 后缀
    if [ "$goos" = "windows" ]; then
      output="${name}-${goos}-${goarch}.exe"
    else
      output="${name}-${goos}-${goarch}"
    fi

    echo "    $goos/$goarch -> $output"
    (cd "$dir" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -o "build/$output" .)
  done

  echo "    Done: $build_dir/"
  echo
done

echo "All builds completed."
