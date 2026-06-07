# batch-rename-hash

递归处理指定目录下的普通文件，把每个文件重命名为其内容的 SHA-256 hash 值（保留原扩展名），并在该目录根目录生成 `file_map.txt`。

## 使用

```bash
go run . /path/to/target
```

也可以使用构建后的二进制：

```bash
./batch-rename-hash-linux-amd64 /path/to/target
```

如果需要保留原文件，同时复制一份以 hash 命名的新文件：

```bash
./batch-rename-hash-linux-amd64 /path/to/target --keep-old-file
```

显示详细日志：

```bash
./batch-rename-hash-linux-amd64 /path/to/target -v
```

`file_map.txt` 格式：

```
源文件完整路径: /path/to/original/file.txt
新文件完整路径: /path/to/original/abc123...def.txt
文件大小: 1024

源文件完整路径: /path/to/another/image.png
新文件完整路径: /path/to/another/xyz789...abc.png
文件大小: 2048
```

如果同一目录中多个文件内容完全相同，它们会得到相同的新文件名。此时程序会停止并报错，避免覆盖文件。

## 构建

```bash
chmod +x build.sh
./build.sh
```

构建产物会写入 `dist/`：

- `batch-rename-hash-darwin-arm64`
- `batch-rename-hash-windows-amd64.exe`
- `batch-rename-hash-linux-amd64`
