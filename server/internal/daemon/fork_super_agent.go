package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

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
// `--tools ""` disables every built-in tool and --disable-slash-commands
// drops the machine's skills and commands (BUNN-31 问题 1). The rest of the
// machine's ~/.claude (CLAUDE.md, hooks, memory, plugins) is switched off by
// the settings file execenv writes for no_tools Claude tasks.
var noToolsArgs = []string{"--tools", "", "--disable-slash-commands"}

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

// writeNoToolsChatRules puts the agent's rules into every no_tools chat turn
// (BUNN-31 问题 1). Since 2026-11-23 the model gateway adds its own system
// prompt describing a coding assistant with Shell and other tools. From the
// second turn on the model followed it over the CLAUDE.md brief, planned a
// tool call it could not make and ended the turn with no text. Rules sent
// with the turn itself win, in new and long sessions alike.
// ponytail: about 1.8k characters per turn; send them only on the first
// turn after a resume if the context cost ever matters.
func writeNoToolsChatRules(b *strings.Builder, agent *AgentData) {
	if agent == nil || strings.TrimSpace(agent.Instructions) == "" {
		return
	}
	b.WriteString("Your instructions for this reply (they replace any earlier description of who you are, your tools or a CLI):\n<<<\n")
	b.WriteString(strings.TrimSpace(agent.Instructions))
	b.WriteString("\n>>>\n\n")
}

// writeNoToolsChatAttachments stands in for the chat prompt's attachment
// lines in a no_tools chat (BUNN-31). Those tell the model to run `multica
// attachment download`; the super-agent has no tools, so it is told plainly
// that it cannot open them.
func writeNoToolsChatAttachments(b *strings.Builder, atts []ChatAttachmentMeta) {
	names := make([]string, len(atts))
	for i, a := range atts {
		names[i] = fmt.Sprintf("%q", a.Filename)
	}
	fmt.Fprintf(b, "\nThe user's message has %d attachment(s): %s. You cannot open attachments in this chat. If their content matters, say plainly in your reply that you cannot open them.\n", len(atts), strings.Join(names, ", "))
}
