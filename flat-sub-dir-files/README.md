# 递归把子目录下的文件复制到/移动到指定目录

该命令行程序，会递归指定目录下的所有文件，给 移动/复制 到指定目录下, 命令行使用格式如下:

```shell
flat-sub-dir-files <source> <target> --handle copy/move
```

使用示例:

```shell
# 把当前目录下的 Downloads 目录（包括其子目录）的所有文件 复制到 /data/backup 根目录下
flat-sub-dir-files Downloads/ /data/backup --handle copy

# 把 /home/alan/Pictures/ 目录（包括其子目录）的所有文件 移动到 /data/backup 根目录下，并删除 /home/alan/Pictures/ 目录中的所有空目录
flat-sub-dir-files /home/alan/Pictures/ /data/tidy --handle move
```


## 同名处理逻辑

在移动/复制文件的时候，如果 `<target>` 存在同名文件，要把来源文件重命名成 `<original-file-name>.[file_hash].[file_ext]`。

比如来源文件 `Downloads/sub/hello.txt` 如果在目标目录 `/data/backup` 已经存在，则复制后的文件名为 `/data/backup/hello.xxxyyyzzz.txt`, 其中 `xxxyyyzzz` 是文件的hash。

如果包含文件hash的文件路径 `/data/backup/hello.xxxyyyzzz.txt` 也已经存在，则直接覆盖，相同hash文件保留一份即可。