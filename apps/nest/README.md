基于 Multica 二次开发：本目录是 [Multica](https://github.com/multica-ai/multica)（fork 自上游 v0.8.1）的一个新 Web 前端，连 fork 的服务端。

它不依赖、不 import、不拷贝 Multica 的界面代码（`apps/web`、`apps/desktop`、`apps/mobile`、`packages/views`、`packages/ui`），也不用 `packages/core`；`src/boundary.test.ts` 守着这一条。仓库根目录的 LICENSE、NOTICE 不变。

## 运行

需要 Node 22 以上，以及一台 fork 服务端。

```bash
pnpm install
pnpm --filter nest build                                 # 产物在 apps/nest/dist
NEST_API_ORIGIN=http://<服务端>:8080 PORT=3000 node apps/nest/server.mjs
```

`server.mjs` 只用 Node 自带的模块，运行时不需要 `node_modules`。它提供页面，并把 `/api`、`/auth`、`/ws`、`/uploads` 原样转给服务端（保留 Host 头），所以浏览器只和这一个地址打交道，服务端不用改配置。Nest 要从地址的根路径提供，服务端写进评论和推送的相对链接才能在 Nest 里打开。

开发时：

```bash
NEST_API_ORIGIN=http://<服务端>:8080 pnpm --filter nest dev
```

## 检查

```bash
pnpm --filter nest test        # 纯函数、链接分类、按键、转发规则、许可证边界
pnpm --filter nest typecheck
```

产品名只写在 `src/config.ts` 一处；改名改这一行再构建。
