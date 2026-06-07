# GitHub Release Downloader

GitHub Release 文件下载 CLI 工具

## 使用

基本语法:

```shell
gh-release-downloader [repository] [version]
```

### 参数说明

- `repository` 是必须参数，是GitHub仓库唯一标识，假如GitHub仓库地址为 `https://github.com/coder/code-server`, 那 `repository` 就传 `coder/code-server`
- `version` 是可选参数，标识 GitHub Relaese 中的版本号

- 如果只传了 `repository`，表示查看该仓库下的最近10条release版本号
- 如果同时传了 `repository` 和 `version` 表示下载指定版本下的所有release产物到当前目录

示例:

```shell
gh-release-downloader coder/code-server # 列出仓库 coder/code-server 最近10条 release 版本号

gh-release-downloader coder/code-server v0.0.2 # 把GitHub版本号为 v0.0.2 的所有产物下载到当前目录
```