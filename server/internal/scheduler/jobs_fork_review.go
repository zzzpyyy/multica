package scheduler

import (
	"context"
	"time"
)

// Fork (A rules 4–6): durable settlement also covers a person's direct status
// change and terminal task events lost during a server restart.
type SubIssueReviewSettler interface {
	SettleSubIssueReviews(context.Context) error
}

func SubIssueReviewSweepJob(settler SubIssueReviewSettler) JobSpec {
	return JobSpec{
		Name: "sub_issue_review_sweep", Cadence: 30 * time.Second, CatchUpMode: CatchUpLatestOnly, CatchUpWindow: time.Hour,
		RunTimeout: 45 * time.Second, StaleTimeout: time.Minute, HeartbeatInterval: 10 * time.Second,
		AllowStaleReentry: true, MaxAttempts: 1, Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			return HandlerResult{}, settler.SettleSubIssueReviews(ctx)
		},
	}
}
