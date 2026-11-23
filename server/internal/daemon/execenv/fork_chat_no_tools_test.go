package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

// Fork (B, BUNN-31 问题 1): a no_tools Claude task launches with a settings
// file that turns off the machine's hooks, auto memory, plugins and every
// CLAUDE.md but the brief, on Prepare and on Reuse. Other tasks keep theirs.
func TestClaudeNoToolsSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	mustWrite(t, filepath.Join(home, ".claude", "settings.json"),
		`{"env":{"ANTHROPIC_BASE_URL":"x"},"enabledPlugins":{"p1@m":true,"p2@m":false}}`)
	real := t.TempDir()
	root := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, root); err != nil {
		t.Fatal(err)
	}
	task := TaskContextForEnv{ChatSessionID: "s", NoTools: true}
	params := PrepareParams{WorkspacesRoot: root, WorkspaceID: "w", TaskID: "t-no-tools", Provider: "claude", Task: task}
	env, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)
	reused := Reuse(ReuseParams{WorkspacesRoot: root, WorkDir: env.WorkDir, Provider: "claude", Task: task}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}
	realWork, _ := filepath.EvalSymlinks(env.WorkDir)
	want := map[string]any{
		"disableAllHooks":   true,
		"autoMemoryEnabled": false,
		"claudeMdExcludes":  []any{"!{" + filepath.Join(env.WorkDir, "CLAUDE.md") + "," + filepath.Join(realWork, "CLAUDE.md") + "}"},
		"enabledPlugins":    map[string]any{"p1@m": false, "p2@m": false},
	}
	for name, e := range map[string]*Environment{"prepare": env, "reuse": reused} {
		if e.ClaudeSettingsPath != filepath.Join(env.RootDir, claudeNoToolsSettingsFile) {
			t.Fatalf("%s settings path = %q", name, e.ClaudeSettingsPath)
		}
		var got map[string]any
		data, err := os.ReadFile(e.ClaudeSettingsPath)
		if err != nil || json.Unmarshal(data, &got) != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s settings = %s, %v; want %v", name, data, err, want)
		}
	}

	// Not symlinked: one plain negation.
	if p, err := prepareClaudeNoToolsSettings(t.TempDir(), real); err != nil {
		t.Fatal(err)
	} else if data, _ := os.ReadFile(p); !strings.Contains(string(data), `"claudeMdExcludes":["!`+filepath.Join(real, "CLAUDE.md")+`"]`) {
		t.Fatalf("plain workdir settings = %s", data)
	}

	// A chat that has tools gets no such file.
	params.TaskID, params.Task.NoTools = "t-tools", false
	other, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer other.Cleanup(true)
	if other.ClaudeSettingsPath != "" {
		t.Fatalf("tools chat got settings %q", other.ClaudeSettingsPath)
	}
}
