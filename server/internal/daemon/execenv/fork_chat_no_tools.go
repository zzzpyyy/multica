package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// claudeNoToolsSettingsFile is the --settings file of a no_tools Claude task.
const claudeNoToolsSettingsFile = "claude-no-tools-settings.json"

// prepareClaudeNoToolsSettings writes the --settings file of a no_tools
// Claude task (BUNN-31 问题 1). The daemon runs with the machine's HOME, so
// Claude Code would load the user's ~/.claude CLAUDE.md, hooks, auto memory
// and plugins into the super-agent's context. Flag settings rank above user
// settings, so this file turns them off for this process only; ~/.claude is
// not touched, and credentials, base URL and model still come from it.
// claudeMdExcludes keeps only the brief (it is matched with symlinks left
// in, so a symlinked workdir lists both paths in one negated pattern; two
// negations would exclude everything). Skills and slash commands go with
// --disable-slash-commands (daemon noToolsArgs).
// ponytail: plugins are read from the daemon's own Claude config dir; an
// agent custom_env CLAUDE_CONFIG_DIR would point elsewhere. The super-agent
// has none.
func prepareClaudeNoToolsSettings(envRoot, workDir string) (string, error) {
	brief := filepath.Join(workDir, "CLAUDE.md")
	keep := "!" + brief
	if real, err := filepath.EvalSymlinks(workDir); err == nil && real != workDir {
		keep = "!{" + brief + "," + filepath.Join(real, "CLAUDE.md") + "}"
	}
	plugins := map[string]bool{}
	if dir, err := agent.ClaudeConfigDir(nil, workDir); err == nil {
		var user struct {
			EnabledPlugins map[string]json.RawMessage `json:"enabledPlugins"`
		}
		if data, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil && json.Unmarshal(data, &user) == nil {
			for id := range user.EnabledPlugins {
				plugins[id] = false
			}
		}
	}
	data, err := json.Marshal(map[string]any{
		"disableAllHooks":   true,
		"autoMemoryEnabled": false,
		"claudeMdExcludes":  []string{keep},
		"enabledPlugins":    plugins,
	})
	if err != nil {
		return "", err
	}
	path := filepath.Join(envRoot, claudeNoToolsSettingsFile)
	return path, os.WriteFile(path, data, 0o600)
}

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
