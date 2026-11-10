package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// A no_tools task gets no MCP server from anywhere — the machine's
// ~/.claude.json, Remote MCP, plugin hooks — and no tools, even with a saved
// `--tools Bash`. The same fake HOME without no_tools shows the inputs are
// real, so the test would catch a leak.
func TestNoToolsTaskGetsNoServersAndNoTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"),
		[]byte(`{"mcpServers":{"local-shell":{"command":"sh","args":["-c","true"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	task := Task{
		Agent:                &AgentData{McpConfig: json.RawMessage(`{}`), CustomArgs: []string{"--tools", "Bash"}},
		RemoteMCPConnections: []remotemcp.Connection{{ContributionID: "c1"}},
		PluginHookTools:      []PluginHookTool{{HookKey: "h1"}},
	}

	// Control: without no_tools every source reaches the launch.
	cfg, err := resolveTaskMcpConfig(task, "claude", nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "local-shell") {
		t.Fatalf("control: machine MCP server not merged: %s", cfg)
	}
	if len(taskRemoteMCPConnections(task)) != 1 || len(taskPluginHookTools(task)) != 1 {
		t.Fatal("control: remote MCP / plugin hooks dropped")
	}
	if _, custom := taskLaunchArgs(task, nil, task.Agent.CustomArgs); !slices.Contains(custom, "Bash") {
		t.Fatal("control: custom args dropped")
	}

	task.NoTools = true
	cfg, err = resolveTaskMcpConfig(task, "claude", json.RawMessage(`{"mcpServers":{"broker":{}}}`), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		McpServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(cfg, &doc); err != nil || doc.McpServers == nil || len(doc.McpServers) != 0 {
		t.Fatalf("no_tools MCP config = %s, want an empty managed mcpServers map", cfg)
	}
	if taskRemoteMCPConnections(task) != nil || taskPluginHookTools(task) != nil {
		t.Fatal("no_tools task still starts Remote MCP or plugin hook servers")
	}
	extra, custom := taskLaunchArgs(task, []string{"--verbose-x"}, task.Agent.CustomArgs)
	if custom != nil {
		t.Fatalf("no_tools kept custom args %v", custom)
	}
	if n := len(extra); n < 2 || extra[n-2] != "--tools" || extra[n-1] != "" || extra[0] != "--verbose-x" {
		t.Fatalf("no_tools extra args = %q, want daemon defaults then --tools \"\"", extra)
	}
}

func TestServerPromptReplacesThePrompt(t *testing.T) {
	const p = "server-built prompt"
	if got := BuildPrompt(Task{IssueID: "i", TriggerCommentID: "c", ServerPrompt: p}, "claude"); got != p {
		t.Fatalf("prompt = %q", got)
	}
}
