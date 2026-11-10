package scheduler

import (
	"context"
	"time"
)

// Fork (zzzpyyy/multica), D: backstop for instance batches whose terminal task
// event was lost (the event bus is in-memory).
type InstanceBatchSettler interface {
	SweepUnsettledBatches(context.Context) error
}

func InstanceBatchSettleSweepJob(settler InstanceBatchSettler) JobSpec {
	return JobSpec{
		Name: "instance_batch_settle_sweep", Cadence: 30 * time.Second, CatchUpMode: CatchUpLatestOnly, CatchUpWindow: time.Hour,
		RunTimeout: 45 * time.Second, StaleTimeout: time.Minute, HeartbeatInterval: 10 * time.Second,
		AllowStaleReentry: true, MaxAttempts: 1, Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			return HandlerResult{}, settler.SweepUnsettledBatches(ctx)
		},
	}
}

// Fork, B: answer-on-your-behalf questions whose run never started or
// vanished are re-enqueued; after three attempts they are pushed (rule 20).
type SuperAgentQuestionSweeper interface {
	SweepSuperAgentQuestions(context.Context) error
}

func SuperAgentQuestionSweepJob(sweeper SuperAgentQuestionSweeper) JobSpec {
	return JobSpec{
		Name: "super_agent_question_sweep", Cadence: 30 * time.Second, CatchUpMode: CatchUpLatestOnly, CatchUpWindow: time.Hour,
		RunTimeout: 45 * time.Second, StaleTimeout: time.Minute, HeartbeatInterval: 10 * time.Second,
		AllowStaleReentry: true, MaxAttempts: 1, Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, _ HandlerInput) (HandlerResult, error) {
			return HandlerResult{}, sweeper.SweepSuperAgentQuestions(ctx)
		},
	}
}
