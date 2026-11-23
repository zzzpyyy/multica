package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (B): replay the actual response frames through the Claude backend and
// the chat completion writer, not a mock of the block selector. Only names and
// the source-message UUID in these public fixtures have been anonymized.
func TestSuperChatResponseBlocks(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("../../pkg/agent/testdata/fork_super_blocks", name+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	b3, b10 := read("60bd7db4"), read("38e43fc3")
	texts := func(stream string) []string {
		var out []string
		for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
			var row struct {
				Message struct {
					Content []struct{ Type, Text string }
				}
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				t.Fatal(err)
			}
			for _, b := range row.Message.Content {
				if b.Type == "text" {
					out = append(out, b.Text)
				}
			}
		}
		return out
	}
	card, directory := texts(b3)[0], texts(b10)[0]
	frame := func(text string) string {
		b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{
			"role": "assistant", "model": "test-model", "content": []map[string]string{{"type": "text", "text": text}},
		}})
		return string(b) + "\n"
	}
	for _, tc := range []struct {
		name, stream, prior, want, kind string
		normal                          bool
	}{
		{"60bd7db4", b3, b10, card, "create_issues", false},
		{"38e43fc3", b10, "", directory, "", false},
		{"repeat-forward-after-card", frame(card) + frame(fence(`{"type":"forward","issue":"SAT-1"}`)), frame(fence(`{"type":"forward","issue":"SAT-1"}`)), card, "create_issues", false},
		{"repeat-status-before-card", frame(fence(`{"type":"set_status","issue":"SAT-1","status":"done"}`)) + frame(card), frame(fence(`{"type":"set_status","issue":"SAT-1","status":"done"}`)), card, "create_issues", false},
		{"all-repeat-direct", frame(fence(`{"type":"forward","issue":"SAT-1"}`)) + frame(fence(`{"type":"set_status","issue":"SAT-1","status":"done"}`)), frame(fence(`{"type":"forward","issue":"SAT-1"}`)) + frame(fence(`{"type":"set_status","issue":"SAT-1","status":"done"}`)), "", "", false},
		{"all-repeat", b10, b10, "", "", false},
		{"single-block-unchanged", frame(directory), frame(directory), directory, "", false},
		{"ordinary-run-unchanged", b3, b10, directory, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("fake Claude executable uses a POSIX shell")
			}
			f := newSuperFixture(t)
			f.fx.Exec(t, `UPDATE agent SET name='前端甲' WHERE workspace_id=$1 AND description='前端' AND system_key IS NULL`, f.fx.WorkspaceID)
			f.turn("给前端甲建个任务：看板卡片支持拖拽排序 76758，完成标准是刷新页面顺序不变，先别跑")
			root := t.TempDir()
			config := filepath.Join(root, "config")
			priorDir := filepath.Join(config, "projects", "project")
			if err := os.MkdirAll(priorDir, 0o700); err != nil {
				t.Fatal(err)
			}
			priorPath := filepath.Join(priorDir, "session.jsonl")
			if err := os.WriteFile(priorPath, []byte("{\"type\":\"user\",\"message\":{\"content\":\"user string content\"}}\n"+tc.prior), 0o600); err != nil {
				t.Fatal(err)
			}
			blocks := texts(tc.stream)
			result, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "session_id": "session", "result": blocks[len(blocks)-1]})
			stream := tc.stream + string(result) + "\n"
			streamPath := filepath.Join(root, "response.jsonl")
			if err := os.WriteFile(streamPath, []byte(stream), 0o600); err != nil {
				t.Fatal(err)
			}
			fake := filepath.Join(root, "claude")
			// Append the live frames before publishing them, as Claude Code does.
			// A selector reading past its pre-run boundary would reject itself.
			if err := os.WriteFile(fake, []byte("#!/bin/sh\nIFS= read -r _\ncat \"$REPLAY\" >> \"$PRIOR\"\ncat \"$REPLAY\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			backend, err := agent.New("claude", agent.Config{ExecutablePath: fake, Logger: slog.Default(), Env: map[string]string{
				"IS_SANDBOX": "1", "CLAUDE_CONFIG_DIR": config, "REPLAY": streamPath, "PRIOR": priorPath,
			}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := backend.Execute(context.Background(), "test", agent.ExecOptions{NoToolsChat: !tc.normal, ResumeSessionID: "session", Cwd: root, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			var emitted []string
			for msg := range session.Messages {
				if msg.Type == agent.MessageText {
					emitted = append(emitted, msg.Content)
				}
			}
			r := <-session.Result
			if r.Status != "completed" || r.Output != tc.want {
				t.Fatalf("completion = %s %q (%s); want %q", r.Status, r.Output, r.Error, tc.want)
			}
			if !tc.normal {
				wantCount := 1
				if tc.want == "" {
					wantCount = 0
				}
				if len(emitted) != wantCount || (wantCount == 1 && emitted[0] != tc.want) {
					t.Fatalf("stream contains discarded/replayed blocks: %q", emitted)
				}
			} else if len(emitted) != len(blocks) {
				t.Fatal("ordinary run lost streamed text")
			}
			payload, _ := json.Marshal(protocol.TaskCompletedPayload{Output: r.Output})
			message, err := f.svc.writeChatCompletionOutcome(context.Background(), f.svc.Queries, f.task, payload)
			if err != nil || message == nil {
				t.Fatalf("store outcome: %v, %v", message, err)
			}
			wantText, _, _ := splitSuperAction(tc.want)
			if wantText == "" {
				wantText = chatNoResponseFallback
			}
			if message.Content != wantText {
				t.Fatalf("stored/Nest reply = %q; want %q", message.Content, wantText)
			}
			var kind, content string
			var stored cardPayload
			if tc.kind == "" {
				if n := f.fx.Count(t, `SELECT count(*) FROM super_agent_action WHERE chat_message_id=$1`, message.ID); n != 0 {
					t.Fatalf("replayed action stored: %d", n)
				}
			} else {
				var raw []byte
				f.fx.QueryRow(t, `SELECT a.kind, a.payload, m.content FROM super_agent_action a JOIN chat_message m ON m.id=a.chat_message_id WHERE m.id=$1`, message.ID).Scan(&kind, &raw, &content)
				if err := json.Unmarshal(raw, &stored); err != nil {
					t.Fatal(err)
				}
				if kind != tc.kind || content != "请确认下面的计划。" || len(stored.Items) != 1 || stored.Items[0].Assignee.Name != "前端甲" || stored.Items[0].Status != "backlog" || stored.Items[0].Title != "看板卡片支持拖拽排序 76758" || stored.Items[0].Done != "拖动看板卡片后刷新页面，看板卡片的顺序不变" {
					t.Fatalf("card does not match selected reply: %s %s %+v", kind, content, stored)
				}
			}
		})
	}
}
