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
}
