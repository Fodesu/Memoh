package application

import (
	"github.com/memohai/memoh/internal/agent/decision"
	"github.com/memohai/memoh/internal/runtimefence"
)

// RuntimeDecisionTarget is the canonical Decision scope plus its continuation
// mode. Only durable may create a replacement run;
// local_waiter and unknown require the original owner.
type RuntimeDecisionTarget struct {
	Decision         runtimefence.PreservedDecision
	ContinuationMode decision.ContinuationMode
	// HasLocalWaiter reports that a local waiter for this decision is blocked
	// in this process, so a durable commit wakes it without any runtime run
	// or continuation. It reflects the prepare-time registry; the commit
	// layer re-derives waiter presence before acting on it.
	HasLocalWaiter bool
}
