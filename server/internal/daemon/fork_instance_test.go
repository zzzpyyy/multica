package daemon

import (
	"strings"
	"testing"
)

func TestInstanceBlockInPrompt(t *testing.T) {
	task := Task{IssueID: "issue-1", Instance: &TaskInstance{
		Index: 2, Total: 3, Split: "list page",
		Siblings: []TaskInstancePeer{{Index: 1, Split: "home page"}, {Index: 3, Split: "detail page"}},
	}}
	prompt := BuildPrompt(task, "claude")
	for _, want := range []string{"[Instance]", "instance 2 of 3", "list page", "instance 1: home page", "instance 3: detail page", "Do not change the issue's status, assignee or parent"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
	if strings.Contains(BuildPrompt(Task{IssueID: "issue-1"}, "claude"), "[Instance]") {
		t.Error("normal run got an instance block")
	}
}

func TestForkDaemonAdvertisesForkCapability(t *testing.T) {
	for name, caps := range map[string]string{"ws": daemonClientCapabilities(), "http": daemonHTTPClientCapabilities()} {
		if !strings.Contains(caps, "multica-fork-v1") {
			t.Errorf("%s capabilities %q lack multica-fork-v1", name, caps)
		}
	}
}
