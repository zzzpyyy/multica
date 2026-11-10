# MODIFICATIONS

本仓库 `zzzpyyy/multica` 是 [multica-ai/multica](https://github.com/multica-ai/multica) v0.8.1（`b1ea927`）的 fork。

改过的上游文件，能写注释的都在开头几行写了一句：

```
Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
```

写不了注释的文件（JSON 等），以及每次都会被重新生成、写了也会被覆盖的文件，列在下表。每个改动点的目的和对应验收，见 [README.md](README.md#改动登记) 的改动登记表。`scripts/fork/check-modified-headers.sh` 检查每个改过的上游文件，要么有这句说明，要么在下表里。

下表第一列是路径，`*` 是通配符，以 `/` 结尾表示整个目录。

| 文件 | 改动目的 |
|---|---|
| `server/pkg/db/generated/` | sqlc 生成的代码，每次 `make sqlc` 都会覆盖 |
| `server/pkg/db/queries/agent.sql` | D：领取逻辑让加开的实例和主实例并行，排序先给优先的实例、再给没有运行中实例的任务；重试、续用会话、待处理名额都区分实例行。sqlc 会把文件开头的注释抄进第一个查询的 Go 文档，所以不写文件头 |
| `server/pkg/db/queries/issue.sql` | D：删任务时一起删掉它的实例批次。不写文件头，原因同上 |
| `server/pkg/db/queries/workspace_delete.sql` | D：删工作区时一起删掉实例批次。不写文件头，原因同上 |
