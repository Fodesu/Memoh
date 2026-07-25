package application

import (
	"github.com/memohai/memoh/internal/agent/decision"
	"github.com/memohai/memoh/internal/runtimefence"
)

// RuntimeDecisionTarget is the canonical Decision scope plus its durable
// execution policy. Only native_continuation may create a replacement run;
// live_waiter and unknown require the original owner.
type RuntimeDecisionTarget struct {
	Decision     runtimefence.PreservedDecision
	ResumePolicy decision.ResumePolicy
	// HasLocalWaiter reports that a live waiter for this decision is blocked
	// in this process, so a durable commit wakes it without any runtime run
	// or continuation. It reflects the prepare-time registry; the commit
	// layer re-derives waiter presence before acting on it.
	HasLocalWaiter bool
}
