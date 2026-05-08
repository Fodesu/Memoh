package orchestrationbus

import (
	"errors"
	"strings"
	"time"
)

// EnvelopeVersion identifies the wire schema. Bumped only when an incompatible
// change is made; consumers may use it to negotiate behaviour during rollouts.
const EnvelopeVersion = 1

// RunEventEnvelope is the wire format for committed orchestration run events
// dispatched by the outbox. Field semantics mirror orchestration.RunEvent so
// callers can convert in either direction without losing identity.
type RunEventEnvelope struct {
	SchemaVersion    int            `json:"schema_version"`
	EventID          string         `json:"event_id"`
	RunID            string         `json:"run_id"`
	TaskID           string         `json:"task_id,omitempty"`
	AttemptID        string         `json:"attempt_id,omitempty"`
	CheckpointID     string         `json:"checkpoint_id,omitempty"`
	Seq              uint64         `json:"seq"`
	AggregateType    string         `json:"aggregate_type"`
	AggregateID      string         `json:"aggregate_id"`
	AggregateVersion uint64         `json:"aggregate_version"`
	Type             string         `json:"type"`
	CausationEventID string         `json:"causation_event_id,omitempty"`
	CorrelationID    string         `json:"correlation_id,omitempty"`
	IdempotencyKey   string         `json:"idempotency_key,omitempty"`
	Payload          map[string]any `json:"payload,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	PublishedAt      time.Time      `json:"published_at"`
}

// Validate ensures the envelope carries enough identity to be routed. We
// intentionally tolerate empty TaskID / AttemptID since some run-level events
// do not target a specific aggregate child.
func (e RunEventEnvelope) Validate() error {
	if strings.TrimSpace(e.RunID) == "" {
		return errors.New("orchestrationbus: run event envelope missing run_id")
	}
	if strings.TrimSpace(e.EventID) == "" {
		return errors.New("orchestrationbus: run event envelope missing event_id")
	}
	if strings.TrimSpace(e.Type) == "" {
		return errors.New("orchestrationbus: run event envelope missing type")
	}
	if e.Seq == 0 {
		return errors.New("orchestrationbus: run event envelope missing seq")
	}
	return nil
}

// AttemptFactEnvelope is the wire format for facts emitted by workerd / verifyd
// while an attempt is in flight (heartbeat, partial output, error, completion).
// Facts are advisory observations: the kernel is still the durable source of
// truth, but the bus delivers them so the planner / blackboard can react
// immediately without polling Postgres.
type AttemptFactEnvelope struct {
	SchemaVersion  int            `json:"schema_version"`
	FactID         string         `json:"fact_id"`
	RunID          string         `json:"run_id"`
	TaskID         string         `json:"task_id,omitempty"`
	AttemptID      string         `json:"attempt_id"`
	ClaimEpoch     int64          `json:"claim_epoch"`
	ClaimToken     string         `json:"claim_token,omitempty"`
	EnvSessionID   string         `json:"env_session_id,omitempty"`
	EnvLeaseEpoch  int64          `json:"env_lease_epoch,omitempty"`
	Type           string         `json:"type"`
	Payload        map[string]any `json:"payload,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	ObservedAt     time.Time      `json:"observed_at"`
}

// Validate checks that an attempt fact carries enough identity to be dispatched
// and matched against an active claim.
func (e AttemptFactEnvelope) Validate() error {
	if strings.TrimSpace(e.RunID) == "" {
		return errors.New("orchestrationbus: attempt fact envelope missing run_id")
	}
	if strings.TrimSpace(e.AttemptID) == "" {
		return errors.New("orchestrationbus: attempt fact envelope missing attempt_id")
	}
	if strings.TrimSpace(e.FactID) == "" {
		return errors.New("orchestrationbus: attempt fact envelope missing fact_id")
	}
	if strings.TrimSpace(e.Type) == "" {
		return errors.New("orchestrationbus: attempt fact envelope missing type")
	}
	if e.ClaimEpoch <= 0 {
		return errors.New("orchestrationbus: attempt fact envelope missing claim_epoch")
	}
	return nil
}
