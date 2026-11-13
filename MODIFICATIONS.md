# MODIFICATIONS

本仓库 `zzzpyyy/multica` 是 [multica-ai/multica](https://github.com/multica-ai/multica) v0.8.1（`b1ea927`）的 fork。

改过的上游文件，能写注释的都在开头几行写了一句：

```
Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
```

写不了注释的文件（JSON 等），以及每次都会被重新生成、写了也会被覆盖的文件（`server/pkg/db/generated/`、`pnpm-lock.yaml`），列在下表。能写注释的文件一律写文件头，不能只登记在这里。每个改动点的目的和对应验收，见 [README.md](README.md#改动登记) 的改动登记表。`scripts/fork/check-modified-headers.sh` 检查每个改过的上游文件，要么有这句说明，要么在下表里。

下表第一列是路径，`*` 是通配符，以 `/` 结尾表示整个目录。

| 文件 | 改动目的 |
|---|---|
| `server/pkg/db/generated/` | sqlc 生成的代码，每次 `make sqlc` 都会覆盖 |
| `packages/core/package.json` | B：导出 `./chat/super-agent`；F0：导出 `./fork`（仓库常量） |
| `packages/views/locales/*/settings.json` | C：「发送」设置的说明只管发一条消息的输入框；F0：桌面端 CLI 来源检查结果和"检查更新"的文案，删掉自动更新开关的文案 |
| `packages/views/locales/*/issues.json` | A：「聚合子任务」开关、一句话进度、展开列表的文案；D：实例列表、排队位置和原因的文案 |
| `packages/views/locales/*/chat.json` | B：super-agent 开通、计划卡片、消息定位失败的文案 |
