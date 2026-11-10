package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// Fork (zzzpyyy/multica), B: the super-agent runs with no tools at all. The
// server marks its tasks no_tools; this daemon is what enforces it, because
// an empty mcp_config from the server would otherwise still be merged with
// the machine's ~/.claude.json servers, Remote MCP and plugin hook tools.

// noToolsMcpConfig is a managed MCP config with no servers: buildClaudeArgs
// adds --strict-mcp-config for it, so Claude loads nothing else.
var noToolsMcpConfig = json.RawMessage(`{"mcpServers":{}}`)

// noToolsArgs is appended to a no_tools task's launch args; Claude Code's
// `--tools ""` disables every built-in tool.
var noToolsArgs = []string{"--tools", ""}

func taskRemoteMCPConnections(task Task) []remotemcp.Connection {
	if task.NoTools {
		return nil
	}
	return task.RemoteMCPConnections
}

func taskPluginHookTools(task Task) []PluginHookTool {
	if task.NoTools {
		return nil
	}
	return task.PluginHookTools
}

// resolveTaskMcpConfig is the MCP config a task launches with: the agent's
// config merged over the runtime's local servers, plus the task's Remote MCP
// and plugin-hook servers. A no_tools task gets noToolsMcpConfig whatever the
// agent, the machine or the server sent.
func resolveTaskMcpConfig(task Task, provider string, remoteMCPConfig json.RawMessage, log *slog.Logger) (json.RawMessage, error) {
	if task.NoTools {
		return noToolsMcpConfig, nil
	}
	if task.Agent == nil {
		return nil, nil
	}
	effective := task.Agent.McpConfig
	if merged, err := mergeRuntimeAndAgentMcpConfig(provider, task.Agent.McpConfig); err != nil {
		log.Warn("mcp_config: runtime merge failed; using agent configuration only", "provider", provider, "error", err)
	} else {
		effective = merged
	}
	if len(remoteMCPConfig) > 0 {
		merged, err := mergeTaskRemoteMCPConfig(effective, remoteMCPConfig)
		if err != nil {
			return nil, fmt.Errorf("merge Remote MCP broker configuration: %w", err)
		}
		effective = merged
	}
	return effective, nil
}

// taskLaunchArgs returns the (extra, custom) args a task launches with. A
// no_tools task drops the agent's custom_args, so a saved `--tools Bash`
// cannot turn tools back on, and ends with `--tools ""`.
func taskLaunchArgs(task Task, extra, custom []string) ([]string, []string) {
	if !task.NoTools {
		return extra, custom
	}
	out := append(append([]string{}, extra...), noToolsArgs...)
	return out, nil
}
