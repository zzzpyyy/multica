package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Fork (B): a no-tools chat has no intermediate tool commentary. Some gateways
// append a second answer (even an older turn's answer) to the same response;
// Claude Code's result then names only that last block. Keep one entire block,
// including its action fence, for both the transcript and completion output.
// Text is held until completion so a replay is never briefly shown as new.
// Ordinary tool-enabled runs retain the upstream streaming/result behavior.
type claudeChatReply struct {
	texts []string
}

func (r *claudeChatReply) selectText(output, resumeID string, snapshot *claudeUsageSnapshot) (string, error) {
	if len(r.texts) < 2 {
		return output, nil // single-block and empty replies keep existing behavior
	}
	prior := make(map[[32]byte]struct{})
	if resumeID != "" {
		if snapshot == nil {
			return "", fmt.Errorf("cannot check multi-block chat reply: prior Claude transcript unavailable")
		}
		f, err := os.Open(snapshot.path)
		if err != nil {
			return "", fmt.Errorf("read prior Claude replies: %w", err)
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !os.SameFile(snapshot.fileInfo, info) || info.Size() < snapshot.offset {
			return "", fmt.Errorf("cannot check multi-block chat reply: prior Claude transcript changed")
		}
		// The snapshot boundary predates this process. Never mistake this turn's
		// newly appended blocks for a previous reply. No history is rewritten.
		scanner := newAgentStreamScanner(io.NewSectionReader(f, 0, snapshot.offset))
		for scanner.Scan() {
			var row struct {
				Type        string          `json:"type"`
				IsSidechain bool            `json:"isSidechain"`
				Message     json.RawMessage `json:"message"`
			}
			if json.Unmarshal(scanner.Bytes(), &row) != nil {
				return "", fmt.Errorf("cannot check multi-block chat reply: malformed prior Claude transcript")
			}
			if row.Type != "assistant" || row.IsSidechain {
				continue
			}
			var message claudeMessageContent
			if json.Unmarshal(row.Message, &message) != nil {
				return "", fmt.Errorf("cannot check multi-block chat reply: malformed prior Claude reply")
			}
			if message.Model == "<synthetic>" {
				continue
			}
			for _, block := range message.Content {
				if block.Type == "text" {
					prior[sha256.Sum256([]byte(block.Text))] = struct{}{}
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read prior Claude replies: %w", err)
		}
	}
	// The first new block carrying an action wins, so a fresh "I'll create it"
	// ahead of the card cannot hide the card; otherwise the first new block.
	first := -1
	for i, text := range r.texts {
		if _, replay := prior[sha256.Sum256([]byte(text))]; replay {
			continue
		}
		if _, _, ok := SplitSuperAction(text); ok {
			return text, nil
		}
		if first < 0 {
			first = i
		}
	}
	if first < 0 {
		// All blocks repeat history: no output and, crucially, no action to execute.
		return "", nil
	}
	return r.texts[first], nil
}

const superActionFence = "```super-action\n"

// SplitSuperAction separates a trailing super-action fence from the visible
// reply. Only a fence at the very end counts; anything else stays visible.
// The server stores cards with this same parse, so a block selected here for
// its action is exactly a block the server turns into that action.
func SplitSuperAction(output string) (visible, raw string, ok bool) {
	normalized := strings.ReplaceAll(output, "\r\n", "\n")
	trimmed := strings.TrimRight(normalized, " \t\n")
	if !strings.HasSuffix(trimmed, "\n```") && trimmed != strings.TrimSuffix(superActionFence, "\n")+"```" {
		return output, "", false
	}
	withoutClose := strings.TrimSuffix(trimmed, "```")
	withoutClose = strings.TrimSuffix(withoutClose, "\n")
	start := strings.LastIndex(withoutClose, "\n"+superActionFence)
	switch {
	case start >= 0:
		visible, raw = withoutClose[:start], withoutClose[start+1+len(superActionFence):]
	case strings.HasPrefix(withoutClose, superActionFence):
		raw = strings.TrimPrefix(withoutClose, superActionFence)
	default:
		return output, "", false
	}
	if strings.Contains(raw, "\n```") { // the fence closed earlier: not the trailing block
		return output, "", false
	}
	return strings.TrimRight(visible, " \t\n"), raw, true
}
