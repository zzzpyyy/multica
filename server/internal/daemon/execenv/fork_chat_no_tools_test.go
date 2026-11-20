package execenv

import (
	"encoding/json"
	"strings"
	"testing"
)

// Fork (B, BUNN-31 问题 5): a no_tools chat brief carries no multica command
// and no sentence about running commands, uploading or reading/writing files;
// every other brief is the one buildMetaSkillContentSlim builds without
// NoTools.
func TestChatBriefNoTools(t *testing.T) {
	t.Parallel()
	full := TaskContextForEnv{
		AgentID:                          "a",
		AgentName:                        "Eve",
		AgentInstructions:                "Reply in one line.",
		RequestingUserName:               "Bob",
		RequestingUserProfileDescription: "Product owner.",
		WorkspaceContext:                 "Be brief.",
		Repos:                            []RepoContextForEnv{{URL: "https://example.com/r.git", Ref: "dev"}},
		ProjectID:                        "p",
		ProjectTitle:                     "P",
		ProjectResources:                 []ProjectResourceForEnv{{ID: "r", ResourceType: "github_repo", ResourceRef: json.RawMessage(`{"url":"https://example.com/r.git"}`)}},
		AgentSkills:                      []SkillContextForEnv{{Name: platformSkillName, BuiltinName: platformSkillName, Description: "d", Content: "c"}},
	}
	chat := full
	chat.ChatSessionID = "8d154f4b-8a7d-4f10-99ef-c1a16e2e7868"

	normal := buildMetaSkillContent("claude", chat)
	for _, want := range []string{"## Available Commands", "full access to the `multica` CLI", "## Important: Always Use the `multica` CLI", "run `multica attachment upload"} {
		if !strings.Contains(normal, want) {
			t.Fatalf("normal chat brief lost %q", want)
		}
	}

	chat.NoTools = true
	out := buildMetaSkillContent("claude", chat)
	for _, banned := range []string{"multica ", "`multica", "MULTICA_CLI_PATH", "## Available Commands", "## Background Task Safety", "## Repositories", "## Project Context", "## Skills", "## Important", "attachment", "upload", "file", "--"} {
		if strings.Contains(out, banned) {
			t.Fatalf("no_tools chat brief contains %q:\n%s", banned, out)
		}
	}
	for _, want := range []string{"**You are: Eve**", "Reply in one line.", "Product owner.", "Be brief.", "## Issue Body Formatting", "- You have no tools in this chat", "## Output\n\nThis is a chat session."} {
		if !strings.Contains(out, want) {
			t.Fatalf("no_tools chat brief lost %q:\n%s", want, out)
		}
	}

	// NoTools changes no other kind: an answer run is an issue task.
	for name, ctx := range map[string]TaskContextForEnv{
		"issue":        withIssue(full),
		"quick-create": withQuickCreate(full),
		"autopilot":    withAutopilot(full),
	} {
		want := buildMetaSkillContent("claude", ctx)
		ctx.NoTools = true
		if got := buildMetaSkillContent("claude", ctx); got != want {
			t.Fatalf("NoTools changed the %s brief", name)
		}
	}
}

func withIssue(c TaskContextForEnv) TaskContextForEnv       { c.IssueID = "i"; return c }
func withQuickCreate(c TaskContextForEnv) TaskContextForEnv { c.QuickCreatePrompt = "q"; return c }
func withAutopilot(c TaskContextForEnv) TaskContextForEnv   { c.AutopilotRunID = "r"; return c }
