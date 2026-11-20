package execenv

import (
	"strings"
	"testing"
)

// Fork (B, BUNN-31 问题 5): a no_tools chat brief must not promise the
// multica CLI; a normal chat brief keeps it.
func TestChatWorkflowNoTools(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{ChatSessionID: "8d154f4b-8a7d-4f10-99ef-c1a16e2e7868", AgentID: "a", AgentName: "Eve"}
	const cli = "full access to the `multica` CLI"

	normal := buildMetaSkillContent("claude", ctx)
	if !strings.Contains(normal, cli) {
		t.Fatal("normal chat brief lost its CLI workflow")
	}
	ctx.NoTools = true
	out := buildMetaSkillContent("claude", ctx)
	if strings.Contains(out, cli) || strings.Contains(out, "use the appropriate CLI commands") {
		t.Fatal("no_tools chat brief still tells the model to use the CLI")
	}
	if !strings.Contains(out, "**You are in chat mode.**\n\n- You have no tools in this chat") {
		t.Fatal("no_tools chat brief does not say it has no tools")
	}
	// Only the workflow differs, so a resumed session re-reads one section.
	if strings.Replace(normal, workflowChatTools, "", 1) != strings.Replace(out, workflowChatNoTools, "", 1) {
		t.Fatal("no_tools changed more than the chat workflow")
	}
}

const workflowChatTools = "- Respond conversationally and helpfully to the user's message\n" +
	"- You have full access to the `multica` CLI to look up issues, workspace info, members, agents, etc.\n" +
	"- If asked about issues, use `multica issue list --output json` or `multica issue get <id> --output json`\n" +
	"- If asked about the workspace, use `multica workspace get --output json`\n" +
	"- If asked to perform actions (create issues, update status, etc.), use the appropriate CLI commands\n" +
	"- If the task requires code changes, use `multica repo checkout <url>` to get the code first. Use `--ref <branch-or-sha>` when you need an exact revision\n"

const workflowChatNoTools = "- You have no tools in this chat: no `multica` CLI, no shell, no files. Reply to the user's message with text only, as Agent Identity says; the server carries out any action block after the user confirms.\n"
