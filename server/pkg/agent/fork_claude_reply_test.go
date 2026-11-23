package agent

import (
	"os"
	"path/filepath"
	"testing"
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
