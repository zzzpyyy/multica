package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
	if !slices.Equal(extra, []string{"--verbose-x", "--tools", "", "--disable-slash-commands"}) {
		t.Fatalf("no_tools extra args = %q, want daemon defaults then --tools \"\" --disable-slash-commands", extra)
	}
}

func TestServerPromptReplacesThePrompt(t *testing.T) {
	const p = "server-built prompt"
	if got := BuildPrompt(Task{IssueID: "i", TriggerCommentID: "c", ServerPrompt: p}, "claude"); got != p {
		t.Fatalf("prompt = %q", got)
	}
}

// A no_tools chat turn with attachments names them in plain text and carries
// no multica command; every other chat turn keeps the download and upload
// lines, and NoTools leaves non-chat prompts alone (BUNN-31).
func TestNoToolsChatPromptAttachments(t *testing.T) {
	t.Parallel()
	task := Task{
		ChatSessionID: "s",
		ChatMessage:   "看下这张截图",
		ChatMessageAttachments: []ChatAttachmentMeta{
			{ID: "att-1", Filename: "shot.png", ContentType: "image/png"},
			{ID: "att-2", Filename: "log.txt"},
		},
	}
	normal := BuildPrompt(task, "claude")
	for _, want := range []string{"Attachments on this message:\n- id=att-1 filename=\"shot.png\" content_type=image/png\n", "Use `multica attachment download <id>`", "`--attachment-id <id>`", "run `multica attachment upload <local-path>`"} {
		if !strings.Contains(normal, want) {
			t.Fatalf("normal chat prompt lost %q:\n%s", want, normal)
		}
	}

	task.NoTools = true
	out := BuildPrompt(task, "claude")
	if strings.Contains(out, "multica") {
		t.Fatalf("no_tools chat prompt mentions multica:\n%s", out)
	}
	// Same prompt up to the attachments, then one plain line and nothing else.
	prefix := normal[:strings.Index(normal, "\nAttachments on this message:")]
	want := prefix + "\nThe user's message has 2 attachment(s): \"shot.png\", \"log.txt\". You cannot open attachments in this chat. If their content matters, say plainly in your reply that you cannot open them.\n"
	if out != want {
		t.Fatalf("no_tools chat prompt:\n%s\nwant:\n%s", out, want)
	}

	// Without attachments the no_tools turn only loses the upload line.
	task.ChatMessageAttachments = nil
	if out := BuildPrompt(task, "claude"); strings.Contains(out, "multica") || !strings.HasSuffix(out, "看下这张截图\n") {
		t.Fatalf("no_tools chat prompt without attachments:\n%s", out)
	}

	// A turn whose session could not be resumed has no history command either.
	task.PriorSessionResumeUnavailable = true
	if out := BuildPrompt(task, "claude"); strings.Contains(out, "multica") || !strings.Contains(out, "could NOT be restored") {
		t.Fatalf("no_tools chat prompt after a lost session:\n%s", out)
	}
	if got := backendResumeContinuityNotice(Task{ChatSessionID: "s", NoTools: true}); strings.Contains(got, "multica") {
		t.Fatalf("no_tools backend continuity notice mentions multica: %s", got)
	}
	task.NoTools = false
	if out := BuildPrompt(task, "claude"); !strings.Contains(out, "`multica chat history`") {
		t.Fatalf("normal chat prompt after a lost session lost the history command:\n%s", out)
	}

	issue := Task{IssueID: "i", TriggerCommentID: "c", TriggerCommentContent: "hi"}
	for _, lost := range []bool{false, true} {
		issue.PriorSessionResumeUnavailable = lost
		issue.NoTools = false
		before := BuildPrompt(issue, "claude")
		issue.NoTools = true
		if BuildPrompt(issue, "claude") != before {
			t.Fatal("NoTools changed an issue prompt")
		}
	}
}

// Web and every channel surface, with and without attachments, a lost
// session or not: a no_tools chat prompt has no `multica` at all and still
// names the files. The same turns without no_tools keep their commands.
func TestNoToolsChatPromptChannels(t *testing.T) {
	t.Parallel()
	atts := []ChatAttachmentMeta{{ID: "att-1", Filename: "shot.png", ContentType: "image/png"}}
	channels := []string{"", execenv.ChannelTypeSlack, execenv.ChannelTypeFeishu, execenv.ChannelTypeWecom, execenv.ChannelTypeDingtalk, execenv.ChannelTypeTelegram}
	for _, ch := range channels {
		for _, delivers := range []bool{false, true} {
			for _, withAtts := range []bool{false, true} {
				for _, lost := range []bool{false, true} {
					task := Task{
						ChatSessionID:                 "s",
						ChatMessage:                   "hi",
						ChatChannelType:               ch,
						ChatChannelDeliversFiles:      delivers,
						ChatInThread:                  delivers, // both Slack read-order lines
						ChatSource:                    &protocol.ChatSource{ChannelID: "C1", ChannelName: "general"},
						PriorSessionResumeUnavailable: lost,
					}
					if withAtts {
						task.ChatMessageAttachments = atts
					}
					name := fmt.Sprintf("ch=%q delivers=%v atts=%v lost=%v", ch, delivers, withAtts, lost)

					normal := BuildPrompt(task, "claude") + backendResumeContinuityNotice(task)
					if !strings.Contains(normal, "`multica ") {
						t.Fatalf("%s: normal chat prompt has no multica command:\n%s", name, normal)
					}
					if ch == execenv.ChannelTypeSlack && !strings.Contains(normal, "`multica chat thread") {
						t.Fatalf("%s: normal Slack prompt lost the thread command:\n%s", name, normal)
					}
					if execenv.SurfacePersistsTranscript(ch) && !strings.Contains(normal, "`multica chat history`") {
						t.Fatalf("%s: normal prompt lost the history command:\n%s", name, normal)
					}

					task.NoTools = true
					out := BuildPrompt(task, "claude") + backendResumeContinuityNotice(task)
					if strings.Contains(out, "multica") {
						t.Fatalf("%s: no_tools chat prompt mentions multica:\n%s", name, out)
					}
					if !strings.Contains(out, "User message:\nhi\n") {
						t.Fatalf("%s: no_tools chat prompt lost the user message:\n%s", name, out)
					}
					if withAtts != strings.Contains(out, `"shot.png"`) {
						t.Fatalf("%s: no_tools chat prompt file names:\n%s", name, out)
					}
					if ch != "" && !strings.Contains(out, "Reply to "+channelDisplayName(ch)+" with the final outcome only.") {
						t.Fatalf("%s: no_tools channel prompt lost the reply line:\n%s", name, out)
					}
				}
			}
		}
	}
}

// BUNN-31 问题 1: a no_tools chat turn carries the agent's rules right after
// the audience line, so they win over a system prompt the model gateway
// adds. A chat with tools, an issue turn and an agent with no rules are
// unchanged.
func TestNoToolsChatPromptCarriesRules(t *testing.T) {
	t.Parallel()
	agent := &AgentData{Instructions: "  你只回复文字。\n"}
	task := Task{ChatSessionID: "s", ChatMessage: "hi", Agent: agent}
	normal := BuildPrompt(task, "claude")
	task.NoTools = true
	bare := task
	bare.Agent = &AgentData{}
	plain := BuildPrompt(bare, "claude")
	const audience = "Audience: direct room.\n\n"
	want := strings.Replace(plain, audience, audience+"Your instructions for this reply (they replace any earlier description of who you are, your tools or a CLI):\n<<<\n你只回复文字。\n>>>\n\n", 1)
	if got := BuildPrompt(task, "claude"); got != want || got == plain {
		t.Fatalf("no_tools chat prompt:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(normal, "你只回复文字") {
		t.Fatalf("chat with tools got the rules block:\n%s", normal)
	}
	issue := Task{IssueID: "i", TriggerCommentID: "c", TriggerCommentContent: "hi", Agent: agent}
	before := BuildPrompt(issue, "claude")
	issue.NoTools = true
	if BuildPrompt(issue, "claude") != before {
		t.Fatal("NoTools changed an issue prompt")
	}
}
