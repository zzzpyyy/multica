package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// Fork (B): a missing/unreadable history cannot authorize replayed actions.
func TestClaudeChatReplyHistoryGate(t *testing.T) {
	single := claudeChatReply{texts: []string{"one"}}
	if got, err := single.selectText("terminal result", "resumed", nil); err != nil || got != "terminal result" {
		t.Fatalf("single block changed: %q, %v", got, err)
	}
	multiple := claudeChatReply{texts: []string{"one", "two"}}
	if got, err := multiple.selectText("two", "", nil); err != nil || got != "one" {
		t.Fatalf("fresh multi block = %q, %v", got, err)
	}
	card := "plan\n\n```super-action\n{}\n```"
	withCard := claudeChatReply{texts: []string{"one", card, "two"}}
	if got, err := withCard.selectText("two", "", nil); err != nil || got != card {
		t.Fatalf("action block not preferred: %q, %v", got, err)
	}
	if got, err := multiple.selectText("two", "resumed", nil); err == nil || got != "" {
		t.Fatalf("missing history was trusted: %q, %v", got, err)
	}
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(path, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &claudeUsageSnapshot{path: path, fileInfo: info, offset: info.Size()}
	if got, err := multiple.selectText("two", "resumed", snapshot); err == nil || got != "" {
		t.Fatalf("corrupt history was trusted: %q, %v", got, err)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	if got, err := multiple.selectText("two", "resumed", snapshot); err == nil || got != "" {
		t.Fatalf("truncated history was trusted: %q, %v", got, err)
	}
}

// Single-block completion and transcript semantics stay upstream-compatible,
// including failures and a terminal result that differs from the text frame.
func TestClaudeChatSingleBlockCompatibility(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake executable")
	}
	for _, isError := range []bool{false, true} {
		for _, noTools := range []bool{false, true} {
			root := t.TempDir()
			path := filepath.Join(root, "claude")
			result := "success"
			errorFlag := "false"
			wantStatus, wantOutput := "completed", "terminal"
			if isError {
				result, errorFlag, wantStatus, wantOutput = "error", "true", "failed", ""
			}
			script := "#!/bin/sh\nIFS= read -r _\ncat <<'RESPONSE'\n" +
				`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"visible"}]}}` + "\n" +
				`{"type":"result","subtype":"` + result + `","is_error":` + errorFlag + `,"result":"terminal"}` + "\nRESPONSE\n"
			writeTestExecutable(t, path, []byte(script))
			b := &claudeBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{"IS_SANDBOX": "1"}}}
			session, err := b.Execute(context.Background(), "test", ExecOptions{NoToolsChat: noTools, Timeout: time.Second * 5})
			if err != nil {
				t.Fatal(err)
			}
			var emitted []string
			for msg := range session.Messages {
				if msg.Type == MessageText {
					emitted = append(emitted, msg.Content)
				}
			}
			r := <-session.Result
			if r.Status != wantStatus || r.Output != wantOutput || !slices.Equal(emitted, []string{"visible"}) {
				t.Fatalf("noTools=%v error=%v: output %q, status %s, emitted %q", noTools, isError, r.Output, r.Status, emitted)
			}
		}
	}
}
