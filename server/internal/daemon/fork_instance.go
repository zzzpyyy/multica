package daemon

import (
	"fmt"
	"strings"
)

// Fork (zzzpyyy/multica), D: role instances.

// TaskInstance is the claim response's `instance` field.
type TaskInstance struct {
	BatchID  string             `json:"batch_id"`
	Index    int                `json:"index"`
	Total    int                `json:"total"`
	Split    string             `json:"split"`
	Siblings []TaskInstancePeer `json:"siblings"`
}

type TaskInstancePeer struct {
	Index int    `json:"index"`
	Split string `json:"split"`
}

// buildInstanceBlock tells an instance its split, its siblings' splits, and
// what it must leave alone (plan 2.3, D4).
func buildInstanceBlock(task Task) string {
	in := task.Instance
	if in == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Instance]\n")
	fmt.Fprintf(&b, "You are instance %d of %d that were started in parallel on this issue. Do only your split:\n\n%s\n\n", in.Index, in.Total, in.Split)
	if len(in.Siblings) > 0 {
		b.WriteString("The other instances are doing these splits at the same time; do not do their work:\n")
		for _, s := range in.Siblings {
			fmt.Fprintf(&b, "- instance %d: %s\n", s.Index, s.Split)
		}
		b.WriteString("\n")
	}
	b.WriteString("Rules for an instance:\n")
	b.WriteString("- Do not change the issue's status, assignee or parent issue; the server refuses it. The issue's owner merges the results.\n")
	b.WriteString("- Do not create sub-issues or start other agents for your split.\n")
	b.WriteString("- When done, post one comment on this issue with your result. If you changed code, name the branch you worked on.\n\n")
	return b.String()
}
