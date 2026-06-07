# Go CLI

由 AI 生成的 Go CLI 工具集合。

## 构建

```bash
# 构建所有工具（Linux/Windows/macOS, amd64/arm64）
./build.sh

# 产物位于各子目录的 build/ 下，命名格式: <工具名>-<os>-<arch>[.exe]
```

---

## 项目列表

- [auto-zip](./auto-zip/) — 定时对指定目录进行 zip 打包，支持密码加密和 cron 调度
- [batch-rename-hash](./batch-rename-hash/) — 将文件重命名为其内容的 SHA-256 哈希值（保留扩展名）
- [github-release-downloader](./github-release-downloader/) — 下载指定 GitHub 仓库 Release 的产物文件
- [switch-hosts-cli](./switch-hosts-cli/) — 订阅远程 hosts 配置并更新本机 hosts，或定时上报本机内网 IP