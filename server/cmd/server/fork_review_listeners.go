package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork (A rules 4–6): deliver only once the run and all automatic retries
// have ended. The database predicate and the 30-second sweep are authoritative
// when a terminal event races, is lost, or announces a pending retry.
func registerForkReviewListeners(bus *events.Bus, h *handler.Handler) {
	settle := func(e events.Event) {
		p, _ := e.Payload.(map[string]any)
		if retry, _ := p["retry_pending"].(bool); retry {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.SettleSubIssueReviews(ctx); err != nil {
			slog.Warn("sub-issue review settlement deferred to sweep", "error", err)
		}
	}
	for _, kind := range []string{protocol.EventTaskCompleted, protocol.EventTaskFailed, protocol.EventTaskCancelled} {
		bus.Subscribe(kind, settle)
	}
}
