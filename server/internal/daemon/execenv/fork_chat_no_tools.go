package execenv

import "strings"

// Fork (B, BUNN-31 问题 5): the brief of a no_tools chat (the super-agent). It
// launches with no tools, so it gets none of the brief's CLI, command, upload
// or file text: no CLI header line, Background Task Safety, Available
// Commands, Repositories, Project Context, Skills, Always Use CLI, or Output
// file delivery. A model resuming a long session on another model followed
// that text, looked for `multica`, found no tools and ended the turn with no
// reply. Every other brief is built by buildMetaSkillContentSlim unchanged.
// Changing this text keeps the session: Claude Code re-reads the changed
// CLAUDE.md on resume.
//
// ponytail: Project Context is dropped whole (its resources are checkout
// pointers); no super-agent session has a project. Add title/description
// back if the super-agent ever gets one.
func buildNoToolsChatBrief(ctx TaskContextForEnv) string {
	var b strings.Builder
	b.WriteString("# Multica Agent Runtime\n\n")
	writeAgentIdentity(&b, ctx)
	writeRequestingUser(&b, ctx)
	writeWorkspaceContext(&b, ctx)
	writeIssueBodyFormatting(&b)
	writeWorkflowHeader(&b)
	b.WriteString("**You are in chat mode.**\n\n")
	b.WriteString("- You have no tools in this chat. Reply to the user's message with text only, as Agent Identity says; the server carries out any action block after the user confirms.\n")
	b.WriteString("- Keep responses concise and direct\n\n")
	b.WriteString("## Output\n\n")
	b.WriteString("This is a chat session. Your reply is delivered directly to the chat window the user is reading.\n\n")
	return b.String()
}
