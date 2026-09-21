// Package botsetup applies a bot's post-create setup (settings, access
// grants, later the Agent install) as a declarative intent: POST /bots records
// what the bot should end up with, and a reconciler built on
// internal/reconcile applies it step by step once the workspace is running.
// A client that disconnects, refreshes or retries never leaves a
// half-configured bot, and never applies a step twice.
package botsetup

import (
	"errors"
	"time"

	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/settings"
)

// Row states of a setup intent.
const (
	StatePending = "pending" // not started for the latest intent
	StateWaiting = "waiting" // precondition (running workspace) not met yet
	StateRunning = "running" // a step is executing
	StateDone    = "done"    // every step done or skipped
	StateFailed  = "failed"  // a step failed; retried until the budget is spent
)

// Steps, in execution order.
const (
	StepSettings = "settings"
	StepGrants   = "grants"
	StepAgent    = "agent"
	StepClaim    = "claim"
	StepInstall  = "install"
	StepEnable   = "enable"
)

// StepOrder is the order steps run in; a step never starts before the ones
// ahead of it are done or skipped.
var StepOrder = []string{StepSettings, StepGrants, StepAgent, StepClaim, StepInstall, StepEnable}

// Step statuses.
const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusRetrying = "retrying"
	StatusFailed   = "failed"
	StatusDone     = "done"
	StatusSkipped  = "skipped"
)

// Spec is what the bot should end up with. Fields left empty are not
// managed: the corresponding steps are skipped.
type Spec struct {
	// Settings is applied through the settings service; only the fields set
	// in the request are compared and written.
	Settings *settings.UpsertRequest `json:"settings,omitempty"`
	// Grants are workspace user access grants to create; ones that already
	// exist are left alone.
	Grants []bots.CreateUserGrantRequest `json:"grants,omitempty"`
	// Agent is the direct Agent (Codex / Claude Code) to create, authorize,
	// install and enable. Reserved for the agent/claim/install/enable steps;
	// not executed yet.
	Agent *AgentSpec `json:"agent,omitempty"`
}

// AgentSpec describes the direct Agent to set up for the bot.
type AgentSpec struct {
	Runtime         string `json:"runtime"`
	AuthorizationID string `json:"authorization_id,omitempty"`
	AppID           string `json:"app_id,omitempty"`
	Revision        string `json:"revision,omitempty"`
	DependencyID    string `json:"dependency_id,omitempty"`
}

// Steps lists the steps this spec manages, in execution order.
func (s Spec) Steps() []string {
	var out []string
	if s.Settings != nil {
		out = append(out, StepSettings)
	}
	if len(s.Grants) > 0 {
		out = append(out, StepGrants)
	}
	return out
}

// Manages reports whether step is part of this spec.
func (s Spec) Manages(step string) bool {
	for _, st := range s.Steps() {
		if st == step {
			return true
		}
	}
	return false
}

// Step is where one setup step stands.
type Step struct {
	Step       string    `json:"step"`
	Status     string    `json:"status"`
	Generation int64     `json:"generation"`
	Attempts   int32     `json:"attempts"`
	LastError  string    `json:"last_error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Setup is one bot's setup row with its steps.
type Setup struct {
	BotID              string
	TeamID             string
	DesiredGeneration  int64
	Spec               Spec
	RequestedBy        string
	State              string
	ObservedGeneration int64
	Attempts           int32
	NextAttemptAt      time.Time
	LeaseOwner         string
	LeaseUntil         time.Time
	Version            int64
	UpdatedAt          time.Time
	Steps              []Step
}

// StepByName returns the step record, if any.
func (s Setup) StepByName(name string) (Step, bool) {
	for _, st := range s.Steps {
		if st.Step == name {
			return st, true
		}
	}
	return Step{}, false
}

// Settled reports whether the reconciler has answered the latest intent.
func (s Setup) Settled() bool {
	return s.ObservedGeneration >= s.DesiredGeneration && (s.State == StateDone || s.State == StateFailed)
}

// RetryPending reports whether a failed setup is still inside its fast retry
// budget; same meaning as botworkspace.Workspace.RetryPending.
func (s Setup) RetryPending(maxAttempts int32) bool {
	return s.State == StateFailed && s.Attempts < maxAttempts
}

// Final reports whether the observation is the last word on the current
// intent: settled and not about to be retried soon.
func (s Setup) Final(maxAttempts int32) bool {
	return s.Settled() && !s.RetryPending(maxAttempts)
}

// Event is what Subscribe delivers: a step transition, or the final outcome.
type Event struct {
	Type   string
	Step   string
	Status string
	Error  string
	Setup  *Setup
}

// Event types.
const (
	EventStep  = "setup_step"
	EventReady = "ready"
	EventError = "error"
)

// StepError classifies a step failure. Retryable failures consume the fast
// budget one attempt at a time; others spend it at once.
type StepError struct {
	Retryable bool
	Err       error
}

func (e *StepError) Error() string { return e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }

// Errors.
var (
	ErrNotFound        = errors.New("bot setup not found")
	ErrVersionConflict = errors.New("bot setup changed concurrently")
	ErrEmptySpec       = errors.New("bot setup spec manages no step")
)
