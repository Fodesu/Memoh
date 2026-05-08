// Package orchestrationbus defines the wire contract for the Memoh orchestration
// event bus. Stage 1 of the orchestration roadmap (see PLAN.md) replaces the
// Postgres polling fan-out for committed run events with a NATS JetStream-backed
// outbox, and introduces a fact-ingress channel so workerd / verifyd can publish
// progress without lease-fenced writes for every signal.
//
// Concrete bus implementations live alongside this contract:
//
//   - InMemoryBus: in-process pub/sub used by tests and single-process deployments
//     where NATS is not configured.
//   - JetStreamBus: production backend backed by NATS JetStream.
//
// All implementations satisfy the Bus interface so callers (orchestration kernel,
// outbox dispatcher, workerd, verifyd) can stay agnostic of the deployment shape.
package orchestrationbus

import (
	"fmt"
	"strings"
)

// Stream names. JetStream streams are created with these names when the bus
// starts up; the in-memory bus uses them only as logical labels.
const (
	StreamRunEvents    = "MEMOH_RUN_EVENTS"
	StreamAttemptFacts = "MEMOH_ATTEMPT_FACTS"
)

// Subject roots. The orchestration bus uses a flat subject namespace under each
// root so subscribers can filter by run / attempt / fact type without parsing
// payloads.
const (
	subjectRootRunEvent    = "memoh.orch.run.event"
	subjectRootAttemptFact = "memoh.orch.attempt.fact"
)

// Wildcard subject patterns for stream and consumer filters.
const (
	SubjectAllRunEvents    = subjectRootRunEvent + ".>"
	SubjectAllAttemptFacts = subjectRootAttemptFact + ".>"
)

// RunEventSubject builds the subject for a committed run event:
//
//	memoh.orch.run.event.<run_id>.<event_type>
//
// run_id segments are sanitised so they cannot escape the run root via dots.
// event_type defaults to "unknown" when empty so consumers can still filter.
func RunEventSubject(runID, eventType string) string {
	return fmt.Sprintf("%s.%s.%s",
		subjectRootRunEvent,
		sanitizeToken(runID),
		sanitizeEventType(eventType),
	)
}

// RunEventRunSubject is the subject pattern that matches every event for a
// given run, useful for per-run subscriptions.
func RunEventRunSubject(runID string) string {
	return fmt.Sprintf("%s.%s.>", subjectRootRunEvent, sanitizeToken(runID))
}

// AttemptFactSubject builds the subject for an attempt fact:
//
//	memoh.orch.attempt.fact.<run_id>.<attempt_id>.<fact_type>
func AttemptFactSubject(runID, attemptID, factType string) string {
	return fmt.Sprintf("%s.%s.%s.%s",
		subjectRootAttemptFact,
		sanitizeToken(runID),
		sanitizeToken(attemptID),
		sanitizeEventType(factType),
	)
}

// AttemptFactRunSubject is the wildcard for every fact under one run.
func AttemptFactRunSubject(runID string) string {
	return fmt.Sprintf("%s.%s.>", subjectRootAttemptFact, sanitizeToken(runID))
}

// sanitizeToken replaces characters that conflict with NATS subject tokens
// (dots, whitespace, wildcards) so caller-supplied IDs cannot widen the subject
// scope or break consumer filters.
func sanitizeToken(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return "_"
	}
	mapper := func(r rune) rune {
		switch r {
		case '.', '*', '>', ' ', '\t', '\r', '\n':
			return '_'
		}
		return r
	}
	return strings.Map(mapper, in)
}

// sanitizeEventType applies the same rules as sanitizeToken but defaults to
// "unknown" when the input is empty, so subscribers always see a non-empty
// trailing segment.
func sanitizeEventType(in string) string {
	cleaned := sanitizeToken(in)
	if cleaned == "_" {
		return "unknown"
	}
	return cleaned
}
