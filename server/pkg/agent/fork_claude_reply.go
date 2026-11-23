package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	for _, text := range r.texts {
		if _, replay := prior[sha256.Sum256([]byte(text))]; !replay {
			return text, nil
		}
	}
	// All blocks repeat history: no output and, crucially, no action to execute.
	return "", nil
}
