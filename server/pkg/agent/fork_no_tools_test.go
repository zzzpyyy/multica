package agent

import (
	"encoding/json"
	"log/slog"
	"slices"
	"testing"
)

// Fork (B): what the daemon hands Claude for a no_tools task launches with
// strict MCP (no inherited servers) and `--tools ""` last.
func TestBuildClaudeArgsForNoToolsTask(t *testing.T) {
	args := buildClaudeArgs(ExecOptions{
		McpConfig: json.RawMessage(`{"mcpServers":{}}`),
		ExtraArgs: []string{"--tools", ""},
	}, slog.Default())
	if !slices.Contains(args, "--strict-mcp-config") {
		t.Fatalf("missing --strict-mcp-config: %q", args)
	}
	if n := len(args); args[n-2] != "--tools" || args[n-1] != "" {
		t.Fatalf("args do not end with --tools \"\": %q", args)
	}
}
