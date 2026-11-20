<!-- Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md. -->

> [!IMPORTANT]
> **本仓库基于 Multica（[multica-ai/multica](https://github.com/multica-ai/multica)）二次开发**，fork 自上游 v0.8.1（`b1ea927`），不是官方版本。
> 上游的 [LICENSE](LICENSE) 和 [NOTICE](NOTICE) 原样保留；界面上的 Multica 名称和 Logo 未改。
> 和上游相比加了：父任务嵌套看板、super-agent 对话式建任务和跟进、同一任务上加开多个智能体实例、Enter 发送。
> 安装、连接方式和改动登记见下面的 [Fork 说明](#fork-说明)。下文其余内容是上游原文，其中官方云、Homebrew 的部分不适用于本 fork。

<div align="center">

<picture>
  <source media="(prefers-reduced-motion: reduce) and (prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <source media="(prefers-reduced-motion: reduce)" srcset="docs/assets/logo-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.gif">
  <img alt="Multica" src="docs/assets/logo-light.gif" width="240">
</picture>

**Agents that show up on the board.**

Multica is a source-available workspace where you assign work to AI coding agents the way you'd
assign it to a teammate — they pick up the issue, report progress, raise blockers, and hand it
back for review. Self-hostable, works with the agent CLIs you already use, no lock-in.

[![CI](https://github.com/multica-ai/multica/actions/workflows/ci.yml/badge.svg)](https://github.com/multica-ai/multica/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/releases)
[![GitHub stars](https://img.shields.io/github/stars/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/stargazers)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.gg/W8gYBn226t)

<p align="center">
  <a href="https://www.star-history.com/multica-ai/multica">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=rank&amp;theme=dark" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=rank" />
      <img alt="Star History Rank" src="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=rank" />
    </picture>
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=trending&amp;theme=dark" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=trending" />
      <img alt="GitHub Trending Repository of the Day" src="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=trending" />
    </picture>
  </a>
</p>

[Website](https://multica.ai) · [Docs](https://multica.ai/docs) · [Quickstart](https://multica.ai/docs/cloud-quickstart) · [Download](https://multica.ai/download) · [Vision](VISION.md) · [Self-Hosting](SELF_HOSTING.md) · [Discord](https://discord.gg/W8gYBn226t) · [X](https://x.com/MulticaAI)

**English | [简体中文](README.zh.md) | [日本語](README.ja.md)**

</div>

<p align="center">
  <img src="apps/docs/public/images/docs/workspace-overview.webp" alt="A Multica board where agents and their human teammates are moving work across columns" width="100%">
</p>

<p align="center">
  <sub><em>Your next 10 hires won't be human.</em></sub>
</p>

---

## What is Multica?

You already run Claude Code, Codex, and three other agents. Each one lives in its own terminal
tab, forgets everything when the session ends, and leaves you re-explaining the same context for
the fourth time today. The more agents you add, the more of your day goes to babysitting them.

Multica puts those agents and your teammates in one workspace. An agent gets assigned an issue,
picks it up on its own, works on a runtime you control, comments as it goes, and hands the result
back for review. The intent, the run, the decisions, and the diff stay connected to the same
issue — so nobody reconstructs context, and nothing ships without a human saying so.

---

## Build the team.

*Claude Code, Codex, Cursor, Kimi — you don't pick one. You hire them all.*

<p align="center">
  <img src="docs/assets/readme/team.webp" alt="An agent's page in Multica: Builder is online and working on an issue, next to its recent runs, runtime, and success rate" width="100%">
</p>

- **[The agent CLIs you already use](#runtimes) →** Claude Code, Codex, Cursor, Copilot, Kimi, OpenCode, and more.
- **[Agents as teammates](https://multica.ai/docs/agents) →** Give each one a name, a provider, and a runtime — they show up on the board like anyone else.
- **[Squads](https://multica.ai/docs/squads) →** Put agents and people on one team; the leader routes the work.
- **[Skills](https://multica.ai/docs/skills) →** Turn a solved problem into a playbook every agent reuses.
- **[Your own runtime](https://multica.ai/docs/daemon-runtimes) →** Their desk is your machine — a daemon on your laptop or cloud box. Code never leaves it.

## Hand off the work.

*It starts as three rough sentences in an issue. It ends as a pull request.*

<p align="center">
  <img src="docs/assets/readme/issue.webp" alt="An issue handed to an agent: Frontend opened a pull request, asked the Reviewer agent for a review, and moved the issue to In Review" width="100%">
</p>

- **[Assign an issue](https://multica.ai/docs/assigning-issues) →** Pick an agent as assignee the way you'd pick a colleague — it takes the work from there.
- **[Autopilots](https://multica.ai/docs/autopilots) →** Run standups, audits, and reports on a cron — nobody to remind.
- **[Chat](https://multica.ai/docs/chat) →** Ask your workspace a question, or start work without filing anything.
- **[Projects](https://multica.ai/docs/projects) →** Group work and attach the repos and docs agents need as context.

## Stay in the loop.

*Which agent touched this? What did it run? What did it cost? Open the run.*

<p align="center">
  <img src="docs/assets/readme/run.webp" alt="A live run transcript: every tool call and command, timestamped, with a failing test's output open" width="100%">
</p>

- **[Execution log](https://multica.ai/docs/tasks) →** Replay every tool call, command, and error, timestamped.
- **Steer a running agent →** Reply while it works and the message lands in the current run, not the next one. Claude Code, Codex, Grok, and Kiro today.
- **Usage analytics →** See what each run cost, per agent and per issue.
- **[Review gates](https://multica.ai/docs/issues) →** Work lands in review, not in main. You decide what ships.
- **[Inbox](https://multica.ai/docs/inbox) →** Get pinged when an agent needs a call, not for every step.
- **[Retries and timeouts](https://multica.ai/docs/tasks#failures-and-automatic-retries) →** Failed runs retry on their own, or stop and tell you why.

## Make it yours.

*Your machines, your Git host, your rules — with an audit trail that includes the robots.*

- **[Self-host everything](SELF_HOSTING.md) →** Docker Compose or Helm, on your own infrastructure.
- **[Your Git host](https://multica.ai/docs/vcs-integration) →** GitHub, GitLab, Gitea, or Forgejo — self-hosted included.
- **[Workspaces](https://multica.ai/docs/workspaces) →** Separate agents, issues, and settings per team.
- **[Roles](https://multica.ai/docs/members-roles) and [access scopes](https://multica.ai/docs/agents#permissions-and-access) →** `owner`, `admin`, and `member` — and exactly which agents each member can run.
- **[Security model](https://multica.ai/docs/security-model) →** What an agent can reach, and what it can't.
- **[Slack, Feishu/Lark, DingTalk, WeCom, and Telegram](https://multica.ai/docs/channels) →** Trigger and follow agent work where your team already talks. New Feishu/Lark connections are open to mainland-China Feishu only for now; DingTalk, WeCom, and Telegram are [community-maintained](https://multica.ai/docs/community-maintained).
- **Web, [desktop](https://multica.ai/docs/desktop-app), and [mobile](https://multica.ai/docs/mobile-app) →** The same workspace on macOS, Windows, Linux, iPhone, and iPad — the iOS app builds from source today, not yet on the App Store.
- **[CLI and API](https://multica.ai/docs/cli) →** Every surface is scriptable. Agents drive Multica through the same CLI you do.

---

## Fork 说明

### 安装

- **CLI**：`curl -fsSL https://raw.githubusercontent.com/zzzpyyy/multica/fork/scripts/install.sh | bash`。只从本仓库的 [Releases](https://github.com/zzzpyyy/multica/releases) 下载，不走 Homebrew。版本号形如 `0.8.1-fork.1`。
- **CLI 没有默认服务端**：`multica setup` 和 `multica setup cloud` 会直接报错，请用 `multica setup self-host --server-url <你的服务端>`。没配服务端时 `multica login --token` 也会报错，不会把令牌发到官方云。
- **macOS 桌面端**：从 Releases 下载对应芯片的安装包。安装包没有用 Apple 开发者证书签名，第一次打开会被拦下，到"系统设置 → 隐私与安全性"里点"仍要打开"。安装包里不带任何服务端地址，要先写好 `~/.multica/desktop.json`：

  ```json
  {"schemaVersion":1,"apiUrl":"https://<服务端>","wsUrl":"wss://<服务端>/ws","appUrl":"https://<Web 地址>"}
  ```

  缺这个文件时桌面端只显示配置错误页，不会连官方云。
- **Nest**：新写的 Web 前端，在 [`apps/nest`](apps/nest/README.md)，运行方式见它的 README。它不依赖、不引用 Multica 的界面代码。

### 发版

打 `v<上游版本>-fork.<n>` 的 tag 并推送，`.github/workflows/release.yml` 会构建 CLI 和 macOS 安装包，扫描解开后的安装包（`scripts/fork/check-public.sh --artifacts`），通过后发到本仓库的 Releases，并标为 latest。

### 公开仓库的检查

- `scripts/fork/check-public.sh`：扫描相对上游新增的所有提交（包括后来被删掉的内容和提交信息），以及解开后的安装包，不许出现内部域名、内网 IP、服务端地址和访问令牌。服务端地址的规则放在仓库外的 `~/.config/multica-fork/public-patterns`，每行一条正则。
- `scripts/fork/check-modified-headers.sh`：改过的上游文件，开头几行要有 `Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.` 这句说明；写不了注释的文件（JSON）和会被重新生成的文件，列进 [MODIFICATIONS.md](MODIFICATIONS.md)。
- 两个脚本在 CI 里每次推送都跑。推送前的钩子还会检查每个新提交的作者和提交者邮箱，装法：`git config core.hooksPath scripts/fork/git-hooks`。
- 自测：`bash scripts/fork/check.test.sh`。

### D 组验收脚本

`scripts/fork/d-acceptance.mjs` 只用 CLI 跑 D1–D12、D14、D15，每条打印 PASS/FAIL 和它核对的证据。需要一个测试环境：两个跑在同一个 Claude 运行环境上的智能体 TB、TL，CLI 指向测试服务端，例如 `MULTICA="multica --profile test" node scripts/fork/d-acceptance.mjs all`。它会建任务、起运行，不要对正式环境跑。合并上游以后重跑一遍。

### 合并上游

合并一个新的上游版本时，在同一个提交里把 `scripts/fork/lib.sh` 的 `FORK_BASELINE` 改成那个版本的 commit。然后对照下面的登记表逐个核对改动点，重跑两个检查脚本和全部验收。

另外两处测试覆盖不到，每次合并都要人工核对：

- **评论触发的入队路径**：子任务验收时扣住交付评论，靠的是 `server/internal/handler/comment.go` 的 `enqueueCommentAgentTriggers`，现在发评论、运行结束补投、改删评论重投三条路径都经过它。合并后查一遍 `computeCommentAgentTriggers` 的所有调用点，确认算出的触发都交给了 `enqueueCommentAgentTriggers`；上游新加的入队路径要接过来。
- **迁移 555 的 CHECK**：9009 把 `issue_wakeup.system_rule` 的 CHECK 换成 `('child_done', 'child_review')`。上游以后重写这个 CHECK 时，它的迁移会因为库里已有 `child_review` 行而失败。合并时在上游那个迁移的列表里加上 `child_review`，并按 G8 给那个文件加文件头。

### 改动登记

新增的文件不登记。"验收"列是本 fork 需求文档里的验收编号。

| 文件 | 目的 | 验收 |
|---|---|---|
| `README.md` | 开头的说明和本节 | G6 |
| `pnpm-lock.yaml` | 加上新前端 `apps/nest` 的依赖（Nest 本身是新增的目录，不登记） | N1 |
| `.github/workflows/ci.yml` | 每次推送都跑 CI，加上 `fork-checks` 任务 | G7、G8 |
| `.github/workflows/release.yml` | 只发 CLI 和 macOS 桌面端；删掉 Docker、Helm、Homebrew；tag 只认 `-fork.N`；发布前扫描安装包 | G7、M1、M5–M7 |
| `.goreleaser.yml` | 删掉 `brews`；`prerelease: false`、`make_latest: true` | M5–M7 |
| `server/internal/cli/update.go`、`update_source_test.go`、`update_test.go` | CLI 升级、daemon 自动升级、服务端下发的升级都查本仓库的 Releases（常量 `server/internal/cli/fork.go`）；`x.y.z-fork.N` 算正式版本，按 (x, y, z, N) 比较新旧 | M2、M7 |
| `server/cmd/multica/cmd_agent.go`、`cmd_auth.go`、`cmd_setup.go`、`cmd_login_test.go` | 删掉官方云默认地址；`setup`、`setup cloud` 报错；没配服务端时 `login --token` 报错 | M2 |
| `scripts/install.sh`、`install.ps1`、`install.test.sh` | 仓库地址改成本仓库；删掉 Homebrew 分支 | M2、M7 |
| `server/pkg/db/queries/agent.sql` | 领取：实例行不受同任务互斥，普通运行只和普通运行互斥；排序依次是优先的实例、没有运行中实例的任务、上游原有顺序。重试保留批次、实例不续用会话；续用会话、待处理名额、合并评论、延后任务的提升都不把实例行算进去 | D1、D2、D6–D8、D12、D16 |
| `server/pkg/db/queries/issue.sql`、`workspace_delete.sql` | 删任务、删工作区时删掉实例批次（新表没有外键） | D |
| `server/internal/service/task.go` | 领取后如果这个角色还有排队行和名额，再唤醒一次 daemon；实例的重试不看普通运行的待处理名额；认得新的唯一索引名 | D1、D2 |
| `server/cmd/migrate/main.go` | 登记 9002–9004 的并发索引清理 | D |
| `server/cmd/server/router.go` | 注册 `POST`/`GET /api/issues/{id}/instances`、`POST /api/agents/super-agent`、计划卡片的三个接口；注册 `GET /api/issues/attention`（一句话进度的"待你验收""待你回复"） | D1、D5、D9、D11、D14、D15、B1、A2、A6、A7、A20–A23、A34、A37 |
| `server/internal/handler/issue_table_query.go`、`issue_table_facets.go` | 表格查询的 `filters.aggregate_sub_issues`：父任务符合这个查询的其他条件（并且状态在 `parent_statuses` 里）时，子任务不返回；行、分组计数、总数和状态分面都按聚合算，其他分面不聚合 | A1、A9、A15–A19 |
| `server/cmd/server/main.go` | 注册实例批次的结算监听和 30 秒兜底扫描 | D3、D10、D14 |
| `server/internal/handler/chat.go` | super-agent 的会话不能新建第二段、归档、删除（409 `super_agent_session_locked`） | B1 |
| `server/internal/handler/handler.go`、`issue.go`、`autopilot.go`、`squad.go`、`issue_wakeup.go`、`task_lifecycle.go` | 指派、自动化负责人、加小队成员、建唤醒规则、重跑碰到 super-agent 返回 400 `super_agent_not_assignable` | G4、G5 |
| `server/internal/handler/comment.go`（`computeCommentAgentTriggers`） | 评论路由汇合处去掉 super-agent：@ 它、回复它的评论都照常发出，但不起运行 | G4、G5 |
| `server/internal/handler/daemon.go`（super-agent） | 领取 super-agent 的任务要求 `multica-fork-v1`；清空工具相关字段、设 `no_tools`、用内置说明，对话轮次前面拼上服务端上下文，代答轮次给 `server_prompt`；`CompleteTask` 提交后执行转达、改状态和代答结果 | B1、B12、B19、G3、R1、R4–R6、R11 |
| `server/internal/service/task.go`（super-agent） | 触发器拒绝的入队当作"没有入队"；委派失败不交给 super-agent 接手；super-agent 的运行不补兜底评论；对话回复写入前剥掉 `super-action` 围栏，校验后和回复在同一个事务里写进 `super_agent_action` | B2–B11、B15–B18、D13、D16、G4、G5 |
| `server/internal/daemon/daemon.go`、`prompt.go`、`types.go`（super-agent） | `no_tools` 的任务不合并本机 `~/.claude.json`、Remote MCP 和插件钩子，丢掉 `custom_args`，加 `--tools ""`；`server_prompt` 不为空时直接作为提示词 | B12、G3 |
| `server/internal/daemon/execenv/execenv.go`、`runtime_config_sections.go`、`server/internal/daemon/daemon.go`（`NoTools`） | `no_tools` 的对话简报只留标题、Agent Identity、Requesting User、Workspace Context、Issue Body Formatting、对话 Workflow（“没有工具，只回文字”）和 Output 第一句，不出现 `multica` 命令，也没有讲调用命令、上传文件、读写文件的句子（`fork_chat_no_tools.go`）。每轮提示词（`server/internal/daemon/prompt.go`、`fork_super_agent.go`）：`no_tools` 的对话不写 `multica attachment download`、`--attachment-id`、`multica attachment upload`，渠道对话也不写 `multica chat history`、`multica chat thread` 那几行，带附件时改成一句纯文字（几个附件、文件名、这个对话里打不开）；会话续不上时用不提 `multica chat history` 的那段说明。其他对话和任务运行的简报、提示词不变 | B3、B14 |
| `server/internal/service/task.go`、`server/internal/handler/handler.go`（`SuperCanAssign`） | super-agent 的工作区目录只列会话成员能派活的智能体和小队（小队按 leader 算），判断就是建任务时的 `canInvokeAgent` | B2、B10 |
| `server/cmd/server/notification_listeners.go` | 智能体 @ 了开通 super-agent 的成员时，这条评论不进该成员的收件箱（`mentioned` 和订阅者的 `new_comment` 都不发），转给 super-agent 代答 | F3、R2 |
| `server/cmd/server/main.go`（super-agent） | 注册推送和代答的监听、代答问题的 30 秒兜底扫描 | F1–F9、R2、R3、R7–R9、R12、G5 |
| `server/internal/handler/agent.go`、`daemon.go` | 领取响应加 `instance` 字段；没声明 `multica-fork-v1` 的 daemon 领到实例行时，这次领取失败（`fork_daemon_required`） | D2、D3 |
| `server/internal/handler/issue.go`、`issue_move.go` | 实例运行改状态、负责人、父任务时返回 403 `instance_run_readonly`（单个、批量、拖动三处） | D4 |
| `server/internal/handler/comment.go` | 实例发的评论不走隐式兜底，不唤醒负责人或 leader | D3 |
| `server/internal/daemon/types.go`、`client.go`、`prompt.go`、`daemon.go` | daemon 声明 `multica-fork-v1`，认 `instance` 字段，提示词里加实例块 | D2–D4 |
| `server/internal/daemon/execenv/execenv.go`、`local_worktree.go`、`local_worktree_test.go` | worktree 模式下实例用按运行生成的分支，不共用任务分支 | D2 |
| `server/internal/service/task_channel_media_race_test.go`、`server/internal/handler/workspace_delete_manifest_test.go` | 唯一索引改名；新表 `agent_task_batch` 归入删工作区的清单 | D |
| `server/internal/service/issue_wakeup_system.go`、`issue_wakeup_join.go`、`issue_wakeup.go` | 第二条平台规则 `child_review`（子任务验收唤醒）：两条规则不同的地方按规则名查表（`systemRuleSpecs`，在 `fork_sub_issue_review.go`）；父任务在 backlog 时保留验收输入；开跑前、并入运行时、领取时丢掉已经过期的验收输入；不做每小时暂停，不发收件箱 | A24、A25、A29–A31、A34、A35、A38 |
| `server/pkg/db/queries/workspace_wakeup.sql` | 唤醒列表里的平台规则只列 `child_done`：`child_review` 没有开关 | A24 |
| `server/pkg/db/queries/issue.sql`、`workspace_delete.sql`（验收） | 删任务、删工作区时删掉 `fork_sub_issue_review`、`fork_deferred_comment_trigger`（新表没有外键） | A24–A28 |
| `server/cmd/migrate/main.go`（验收） | 登记 9010、9011 的并发索引 | A24 |
| `server/internal/handler/comment.go`（`enqueueCommentAgentTriggers`） | 交付运行发给验收智能体的评论（含回到 leader 的隐式路由）先扣住，运行结束后并进验收唤醒或放行 | A24、A33、A36 |
| `server/cmd/server/main.go`（验收） | 注册验收结算的监听和 30 秒兜底扫描 | A24、A33 |
| `server/internal/handler/agent.go`、`daemon.go`（`owner_is_you`）、`server/internal/daemon/types.go`、`prompt.go` 及测试 | 领取响应的 `parent_issue.owner_is_you`：这次运行的智能体同时是子任务和父任务的负责人（小队按 leader 算）；[Parent issue] 块据此多一句；验收唤醒的提示不再说"成员可以管理" | A35 |
| `server/internal/daemon/execenv/runtime_config_sections.go`、`runtime_config_test.go` | 简报状态规则最后一句：子任务的 done 由父任务负责人在验收唤醒里设置，顶层任务的 done 留给人；验收人就是交付人时直接置 done | A32、A35 |
| `server/internal/handler/squad_briefing.go`、`squad_parent_status_contract_test.go` | 小队协议第 6 条：顶层任务的 done 留给人，子任务的 done 由父任务负责人验收后设置 | A32 |
| `server/internal/service/builtin_skills/multica-platform/references/sub-issues.md` | 平台说明补上验收唤醒，leader 不用再给子任务建"等它进入 in_review"的唤醒 | A32 |
| `server/internal/handler/fork_issue_attention.go`（按验收人分） | `in_review` 只列等你验收的（顶层任务、没人能来验收的子任务），`in_review_by_others` 列别人验收的子任务和验收人；规则只在 `service.ResolveSubIssueReviewer` 里写一次 | A2、A31、A34、N32、N34、N35 |
| `packages/core/shortcuts/definitions.ts`、`store.test.ts` | 「发送」默认改成 Enter | C1–C10 |
| `packages/views/editor/extensions/submit-shortcut.ts`、`index.ts`、`content-editor.tsx`、`title-editor.tsx` 及测试 | 提交键分两类：发一条消息（跟随设置，设为 Enter 时 ⌘+Enter 也发）、长文本（固定 ⌘+Enter）；按住提交键不重复提交、不插空行 | C1–C17 |
| `packages/views/modals/create-issue.tsx`、`feedback.tsx`、`run-confirm.tsx`、`issues/components/wakeup-create.tsx`、`squads/components/squad-detail-page.tsx`、`settings/components/issue-statuses-tab.tsx`、`quick-actions/components/quick-action-dialog.tsx` 及测试 | 长文本类输入框和运行确认弹窗固定 ⌘+Enter；状态说明补输入法判断；快捷操作的提示词新增 ⌘+Enter 提交 | C11、C12、C14–C17 |
| `packages/core/issues/stores/view-store.ts`、`views/issues/components/issues-header.tsx`、`issues/surface/use-issue-surface-controller.ts` | 看板「显示」菜单的「嵌套子任务」开关，默认开；开着时看板只取顶层任务 | A1、A15 |
| `packages/views/issues/components/board-card.tsx`、`board-view.tsx`、`packages/core/issues/index.ts` | 卡片上的子任务摘要和展开列表（新组件在 `issues/components/nested-board/`），"展开"、进度环和"含 N 个已取消"在一句话进度下面的那一行，放不下时折到下一行，不隐藏；卡片上的"<角色> 运行 m · 排队 q" | A2–A5、A12、A13、D5 |
| `packages/views/issues/components/list-row.tsx` 及测试 | 列表行上的进度环、"含 N 个已取消"和展开；一句话进度在行下面单独占一行或几行，只在分段之间换行 | A16 |
| `packages/views/modals/feedback.tsx`（GitHub 链接）及测试 | 删掉反馈弹窗里指向上游 Issues 的链接：本仓库没开 Issues。反馈照常提交到服务端 | — |
| `packages/views/issues/components/issue-detail.tsx` | 子任务进度按 done + cancelled 计数；实例列表 | A2、D5、D9 |
| `packages/core/issues/ws-updaters.ts` 及测试 | 建子任务时刷新批量子任务查询；改父任务时在子任务缓存里也找旧父任务 | A8、A9 |
| `packages/core/realtime/use-realtime-sync.ts` 及测试 | 回到前台、网络恢复时刷新任务数据；`task:*`、`daemon:register` 刷新全部实例查询；推送消息写入缓存并刷新未读；`chat:done` 刷新计划卡片 | A11、M4、D5、D9、F7、B2、B7 |
| `packages/core/api/client.ts`、`schemas.ts`、`issues/queries.ts`、`types/index.ts` | 实例、super-agent、计划卡片的接口（契约第 3 节） | D、B |
| `packages/views/issues/components/comment-card.tsx` | 实例发的评论标"实例 i/k · 分工" | D3 |
| `packages/views/layout/global-shortcuts.tsx` 及测试 | ⌘+J 打开 super-agent 那段会话 | B1 |
| `packages/views/chat/`（`chat-page.tsx`、`chat-window.tsx`、`chat-message-list.tsx`、`chat-session-header.tsx`、`chat-thread-list.tsx`、`use-chat-controller.ts`、`lib/quick-actions.ts`）及测试 | super-agent 的开通卡片、只有一段会话、不出现在新对话的选择里；计划卡片；流式输出时剥掉 `super-action` 围栏；`?message=` 定位并高亮 | B1–B7、B14、B15、B18、D13、D16、F7 |
| `packages/views/issues/components/pickers/assignee-picker.tsx`、`editor/extensions/mention-suggestion.tsx`、`modals/quick-create-issue.tsx`、`modals/create-squad.tsx`、`autopilots/components/pickers/agent-picker.tsx`、`workflows/handoff-form.tsx` | 负责人、@、通过智能体创建、小队成员、自动化、交接的候选里不出现 super-agent | B 规则 7、G4 |
| `apps/desktop/src/main/runtime-config-loader.ts`、`shared/runtime-config.ts`、`renderer/src/App.tsx` 及测试 | 正式版缺 `~/.multica/desktop.json` 时显示配置错误页，不连官方云；删掉内置的官方云地址 | M2、M3 |
| `apps/desktop/src/main/daemon-manager.ts`、`shared/daemon-types.ts`、`renderer/src/components/daemon-settings-tab.tsx` 及测试 | 4 个 CLI 来源都只接受带 `-fork.` 的版本；都没有时不启动 daemon，设置页列出每个来源的结果 | M7、M8 |
| `apps/desktop/src/main/cli-bootstrap.ts` | 回退下载改成本仓库的 Releases（常量 `apps/desktop/src/shared/fork.ts`） | M7 |
| `apps/desktop/electron-builder.yml` | `publish` 改成本仓库 | M5、M6 |
| `apps/desktop/src/main/updater.ts`、`shared/updater-types.ts`、`preload/`、`renderer/src/components/updates-settings-tab.tsx` 及测试；删掉 `updater-preferences.ts` 和它的测试 | 不签名就不自动检查更新；"检查更新"打开本仓库的 Releases 页面；自动更新开关一起删掉 | M5、M6 |
| `packages/views/runtimes/components/update-section.tsx`、`common/cli-install-command.tsx`、`packages/core/runtimes/commands.ts` 及测试 | "有新版本"查本仓库；安装命令用本仓库的脚本（常量 `packages/core/fork.ts`）；删掉官方云地址 | M2、P7、P8 |

## Get started

- **Cloud** — sign up at **[multica.ai](https://multica.ai)**. No terminal required.
- **Desktop** — download **[Multica Desktop](https://multica.ai/download)** for macOS, Windows, or
  Linux. It connects the computer it runs on as a runtime automatically.
- **Self-host** — run the whole stack on your own infrastructure; see below.

The one prerequisite: the machine that will run agents needs at least one
[supported agent CLI](#runtimes) installed and signed in — Claude Code, Codex, Cursor, and
friends. Multica drives them; it doesn't ship them.

<details>
<summary><b>Self-hosting the whole thing</b></summary>

<br/>

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
multica setup self-host
```

On Windows, set `$env:MULTICA_MODE="with-server"`, then run the PowerShell installer:
`irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex`.

This pulls the official images from GHCR and requires Docker. See the
[Self-Hosting Guide](SELF_HOSTING.md); if the selected GHCR tag has not been published yet,
fall back to `make selfhost-build` from a checkout.

A self-hosted server sends one anonymous, deployment-level snapshot a day: the release version
plus bucketed counts, with no names, content, or IDs. Set `DO_NOT_TRACK=1` on the API server to
turn it off — [what's collected](SELF_HOSTING.md#anonymous-deployment-telemetry).

</details>

### Your first agent in five minutes

**1. Connect a computer.** A *runtime* is any machine agents can work on — your laptop, or a
cloud box. Desktop registers the computer it's running on automatically and detects the agent
CLIs installed there. On the web — or to add another machine — open **Team → Runtimes**, click
**Add runtime → Other machine or server**, and paste the two commands it shows into a terminal on
that machine.

**2. Create an agent.** Open **Team** in the sidebar and click **+** (**New agent**). Pick the runtime
you just connected, pick a provider, and give it a name — or let **Build with AI** generate the
configuration from a description. That name is how it shows up on the board and in comments.

**3. Assign it something.** File an issue and set the agent as assignee. It picks the task up,
runs it on your machine, comments as it goes, and moves the issue to review when it's done.

Full walkthrough: [Quickstart](https://multica.ai/docs/cloud-quickstart) · [Tutorial](https://multica.ai/docs/tutorial)

---

## Runtimes

Multica does not ship a model. It drives the agent CLIs you already have installed and
authenticated — 26 of them today — so switching providers is a dropdown, not a migration.

| Provider | CLI | Provider | CLI |
| --- | --- | --- | --- |
| Claude Code | `claude` | OpenAI Codex | `codex` |
| Cursor Agent | `cursor-agent` | GitHub Copilot CLI | `copilot` |
| OpenCode | `opencode` | OpenClaw | `openclaw` |
| Hermes | `hermes` | Pi | `pi` |
| Antigravity | `agy` | CodeBuddy | `codebuddy` |
| DevEco Code | `deveco` | Grok | `grok` |
| Kimi | `kimi` | Kiro CLI | `kiro-cli` |
| Qoder CLI | `qodercli` | Qoder CN | `qoderclicn` |
| Qwen Code | `qwen` | QwenPaw | `qwenpaw` |
| Reasonix | `reasonix` | Trae CLI | `traecli` |
| DeepSeek Harness | `dsh` | Oh-My-Pi | `omp` |
| MiniMax Code | `mcode` | Dim | `dim` |
| Huawei Cloud CodeArts | `codearts` | ZeroClaw | `zeroclaw` |
| ZCode | `zcode` / ZCode Desktop | | |

Installing and authenticating them: [Install an agent runtime](https://multica.ai/docs/install-agent-runtime) ·
[Providers](https://multica.ai/docs/providers)

---

## Documentation

| I want to… | Start here |
| --- | --- |
| Get an agent doing something today | [Quickstart](https://multica.ai/docs/cloud-quickstart) · [Tutorial](https://multica.ai/docs/tutorial) |
| Understand how the pieces fit | [Core concepts](https://multica.ai/docs/concepts) · [How Multica works](https://multica.ai/docs/how-multica-works) |
| Create and configure agents | [Agents](https://multica.ai/docs/agents) · [Create an agent](https://multica.ai/docs/agents-create) · [Skills](https://multica.ai/docs/skills) |
| Get work to an agent | [Triggering agents](https://multica.ai/docs/triggering-agents) · [Assigning issues](https://multica.ai/docs/assigning-issues) · [Mentions](https://multica.ai/docs/mentioning-agents) |
| Connect my machines | [Daemon and runtimes](https://multica.ai/docs/daemon-runtimes) · [Install an agent runtime](https://multica.ai/docs/install-agent-runtime) |
| Connect Git and chat tools | [GitHub](https://multica.ai/docs/github-integration) · [Self-hosted Git](https://multica.ai/docs/vcs-integration) · [Channels](https://multica.ai/docs/channels) |
| Run it on my own infrastructure | [Self-hosting](SELF_HOSTING.md) · [Security model](https://multica.ai/docs/security-model) · [Environment variables](https://multica.ai/docs/environment-variables) |
| Script it | [CLI reference](https://multica.ai/docs/cli) · [CLI and daemon guide](CLI_AND_DAEMON.md) · [Auth tokens](https://multica.ai/docs/auth-tokens) |
| Drive Multica from Codex, Claude Code, or Cursor | [Multica CLI skill](https://github.com/multica-ai/multica-cli) |
| Work out why an agent is stuck | [Runs](https://multica.ai/docs/tasks) · [Troubleshooting](https://multica.ai/docs/troubleshooting) |

The docs are also available in [简体中文](https://multica.ai/docs/zh), [日本語](https://multica.ai/docs/ja),
[한국어](https://multica.ai/docs/ko), and [Français](https://multica.ai/docs/fr).

---

## Architecture

```
   Web (browser)        Desktop (Electron)        iPhone · iPad (Expo)
         │                      │                          │
         ▼                      │                          │
  ┌──────────────┐              │                          │
  │   Next.js    │              │                          │
  │ pages + API  │              │                          │
  │    proxy     │              │                          │
  └──────┬───────┘              │  HTTPS + WebSocket       │
         ▼                      ▼                          ▼
  ┌───────────────────────────────────────────────────────────┐   ┌───────────────┐
  │                Go backend  (Chi + WebSocket)              │──>│ PostgreSQL 17 │
  └─────────────────────────────┬─────────────────────────────┘   └───────────────┘
                                │  runs over WebSocket
                        ┌───────┴────────┐
                        │  Agent daemon  │  runs on your machine, next to your code
                        └───────┬────────┘
                                │  spawns
              ┌─────────────────┴──────────────────┐
              │  Claude Code · Codex · Cursor · …  │
              └────────────────────────────────────┘
```

| Layer | Stack |
| --- | --- |
| Web | Next.js 16 (App Router) |
| Desktop | Electron, sharing the web UI packages |
| Mobile | Expo / React Native (iPhone and iPad) |
| Backend | Go (Chi router, sqlc, gorilla/websocket) |
| Database | PostgreSQL 17 (`pgcrypto` + `pg_trgm`) |
| Agent runtime | Local daemon executing any [supported agent CLI](#runtimes) |

---

## Development

Contributors: start with the [Contributing Guide](CONTRIBUTING.md).

**Prerequisites:** [Node.js](https://nodejs.org/) 22, [pnpm](https://pnpm.io/) 10.28.2, [Go](https://go.dev/) 1.26.9, [Docker](https://www.docker.com/)

```bash
make dev
```

`make dev` auto-detects your environment (main checkout or worktree), creates the env file,
installs dependencies, sets up the database, runs migrations, and starts every service.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow, worktree support, testing, and
troubleshooting. The iOS client lives in [`apps/mobile/`](apps/mobile/) — its
[README](apps/mobile/README.md) covers building it onto your own device.

We release most weekdays, so `main` moves quickly — pull often.

---

## Why "Multica"?

**Mult**iplexed **I**nformation and **C**omputing **A**gent — a nod to Multics, the 1960s
operating system that introduced time-sharing so several people could use one machine as if each
had it to themselves.

Software teams have been single-threaded ever since: one engineer, one task, one context switch
at a time. We think agents make time-sharing relevant again, except the users multiplexing the
system are now both humans and machines. A small team shouldn't feel small.

The longer argument, and where we think this goes: **[VISION.md](VISION.md)**.

---

## License

[Multica License](LICENSE) — the complete Apache License 2.0 text plus additional conditions
covering hosted services, commercial embedding, and branding. Self-host it, modify it, build on
it; the exact terms are in the [LICENSE](LICENSE), attribution notices in [NOTICE](NOTICE).
