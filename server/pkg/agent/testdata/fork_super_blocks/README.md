# Multi-block Claude response regressions

Captured response frames for BUNN-31, 2026-11-23:

- `60bd7db4.jsonl`: blocks 0/1/2 are a create_issues card, thinking, and a replay of the previous B10 directory reply.
- `38e43fc3.jsonl`: blocks 0/1/2/3 are thinking, the directory reply, thinking, and the same directory reply again.

Extracted only `type`, `message`, `apiBlockIndex`, `requestRef` from the assistant rows of the recorded session. The model payloads, block order, request references, message IDs, and usage are retained. Public fixtures replace names and one source-message UUID; request bodies, filesystem paths and credentials are not included. Names map consistently to fictitious counterparts (`前端甲` is the card assignee). The regression test derives the CLI terminal result from the last text block, as Claude Code did.

`internal/service/fork_super_blocks_db_test.go` replays these frames through a fake executable into the real Claude backend, then the real chat completion writer. It checks one emitted block, the persisted reply and linked card. Reverting the `claude.go` changes fails both named cases: `60bd7db4` loses the card; `38e43fc3` emits the directory twice.

```sh
cd server
DATABASE_URL="$TEST_DATABASE_URL" go test ./internal/service -run '^TestSuperChatResponseBlocks$' -count=1 -v
go test ./pkg/agent -run '^TestClaudeChatReplyHistoryGate$' -count=1
```
