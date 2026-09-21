package botsetup

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/felinics/memoh/internal/botworkspace"
	"github.com/felinics/memoh/internal/reconcile"
	"github.com/felinics/memoh/internal/redact"
)

// WorkspaceReader is the slice of the workspace reconciler the precondition
// check uses: a step runs only against a running workspace.
type WorkspaceReader interface {
	Get(ctx context.Context, botID string) (botworkspace.Workspace, error)
}

// Deps are the services the steps call into.
type Deps struct {
	Settings   SettingsApplier
	Grants     GrantsApplier
	Workspaces WorkspaceReader
}

// Options tune the reconciler. Zero values take the reconcile defaults.
type Options struct {
	reconcile.Options
	// StepTimeout bounds one step's execution.
	StepTimeout time.Duration
	// WaitInterval is how long a row waits when its workspace is not running
	// yet; waiting consumes no retry budget.
	WaitInterval time.Duration
}

func (o Options) withDefaults() Options {
	o.Options = o.WithDefaults()
	if o.StepTimeout <= 0 {
		o.StepTimeout = 5 * time.Minute
	}
	if o.WaitInterval <= 0 {
		o.WaitInterval = 10 * time.Second
	}
	return o
}

// Service is the setup reconciler plus its intent API.
type Service struct {
	repo Repository
	deps Deps
	log  *slog.Logger
	opts Options
	now  func() time.Time
	rnd  func() float64

	loop    *reconcile.Loop[Setup]
	backoff reconcile.Backoff
	broker  *reconcile.Broker[Event]
	steps   map[string]Executor
}

// New builds a Service. The loop starts with Start.
func New(repo Repository, deps Deps, log *slog.Logger, opts Options) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{
		repo:   repo,
		deps:   deps,
		log:    log.With(slog.String("component", "botsetup")),
		opts:   opts.withDefaults(),
		now:    time.Now,
		rnd:    rand.Float64,
		broker: reconcile.NewBroker[Event](64),
	}
	ropts := s.opts.Options
	ropts.Now = func() time.Time { return s.now() }
	ropts.Rand = func() float64 { return s.rnd() }
	ropts.KeyField = "bot_id"
	s.backoff = ropts.Backoff()
	s.loop = reconcile.NewLoop(repo, func(st Setup) string { return st.BotID }, s.reconcileOne, s.log, ropts)
	s.steps = s.executors()
	return s
}

// MaxAttempts is the fast retry budget.
func (s *Service) MaxAttempts() int32 { return s.opts.MaxAttempts }

// ─── Intent API ──────────────────────────────────────────────────────────────

// EnsureSetup records what the bot should end up with and wakes the loop. A
// spec that manages no step is rejected.
func (s *Service) EnsureSetup(ctx context.Context, botID, requestedBy string, spec Spec) (Setup, error) {
	if len(spec.Steps()) == 0 {
		return Setup{}, ErrEmptySpec
	}
	st, err := s.repo.Upsert(ctx, strings.TrimSpace(botID), strings.TrimSpace(requestedBy), spec)
	if err != nil {
		return Setup{}, err
	}
	s.loop.Kick()
	return st, nil
}

// Retry asks for another pass over the same spec; steps already done stay
// done.
func (s *Service) Retry(ctx context.Context, botID string) (Setup, error) {
	st, err := s.repo.Retry(ctx, strings.TrimSpace(botID))
	if err != nil {
		return Setup{}, err
	}
	s.loop.Kick()
	return st, nil
}

// Get returns the row with its steps.
func (s *Service) Get(ctx context.Context, botID string) (Setup, error) {
	return s.repo.Get(ctx, strings.TrimSpace(botID))
}

// Subscribe streams step transitions and the final outcome to an in-process
// listener; Await is the reliable way to learn the outcome.
func (s *Service) Subscribe(botID string) (<-chan Event, func()) { return s.broker.Subscribe(botID) }

// Await blocks until the setup has a final answer for an intent at least as
// new as generation.
func (s *Service) Await(ctx context.Context, botID string, generation int64) (Setup, error) {
	return reconcile.Await(ctx, 400*time.Millisecond,
		func(ctx context.Context) (Setup, error) { return s.repo.Get(ctx, botID) },
		func(st Setup) bool { return st.DesiredGeneration >= generation && st.Final(s.opts.MaxAttempts) },
	)
}

// Start launches the loop; Stop waits for in-flight steps.
func (s *Service) Start(ctx context.Context) error { return s.loop.Start(ctx) }
func (s *Service) Stop(ctx context.Context) error  { return s.loop.Stop(ctx) }

// ReconcileOnce runs a single pass synchronously (tests, admin tooling).
func (s *Service) ReconcileOnce(ctx context.Context) (int, error) { return s.loop.ReconcileOnce(ctx) }

// ─── Reconciliation ──────────────────────────────────────────────────────────

func (s *Service) reconcileOne(ctx context.Context, st Setup) {
	log := s.log.With(slog.String("bot_id", st.BotID), slog.Int64("generation", st.DesiredGeneration))
	if st.State == StateDone && st.ObservedGeneration >= st.DesiredGeneration {
		s.loop.Release(ctx, st.BotID)
		return
	}
	// Precondition: the workspace must be running. Waiting is not a failure
	// and consumes no retry budget; the row simply comes back later.
	if !s.workspaceRunning(ctx, st.BotID) {
		if _, err := s.writeObserved(ctx, st, ObservedWrite{
			State: StateWaiting, ObservedGeneration: st.ObservedGeneration, Attempts: st.Attempts,
			NextAttemptAt: s.now().Add(s.opts.WaitInterval), ReleaseLease: true,
		}); err != nil {
			log.WarnContext(ctx, "defer setup failed", slog.Any("error", err))
			s.loop.Release(ctx, st.BotID)
		}
		return
	}
	cur, err := s.writeObserved(ctx, st, ObservedWrite{
		State: StateRunning, ObservedGeneration: st.ObservedGeneration, Attempts: st.Attempts,
		NextAttemptAt: s.now(), ReleaseLease: false,
	})
	if err != nil {
		log.WarnContext(ctx, "enter running failed", slog.Any("error", err))
		s.loop.Release(ctx, st.BotID)
		return
	}
	for _, name := range StepOrder {
		step, ok := cur.StepByName(name)
		if !ok || step.Status == StatusDone || step.Status == StatusSkipped {
			continue
		}
		exec := s.steps[name]
		if exec == nil {
			// A step this build cannot run yet: leave it pending and stop
			// here; a later build picks it up.
			log.InfoContext(ctx, "setup step has no executor yet", slog.String("step", name))
			if _, err := s.writeObserved(ctx, cur, ObservedWrite{
				State: StateWaiting, ObservedGeneration: cur.ObservedGeneration, Attempts: cur.Attempts,
				NextAttemptAt: s.now().Add(s.opts.SlowRetryInterval), ReleaseLease: true,
			}); err != nil {
				s.loop.Release(ctx, cur.BotID)
			}
			return
		}
		cur = s.setStep(ctx, cur, Step{Step: name, Status: StatusRunning, Generation: cur.DesiredGeneration, Attempts: step.Attempts})
		opCtx, cancel := s.loop.LeasedContext(ctx, cur.BotID, s.opts.StepTimeout)
		note, err := exec(opCtx, cur)
		cancel()
		if err != nil {
			s.fail(ctx, log, cur, name, step.Attempts, err)
			return
		}
		cur = s.setStep(ctx, cur, Step{Step: name, Status: StatusDone, Generation: cur.DesiredGeneration, Attempts: step.Attempts, LastError: note})
		log.InfoContext(ctx, "setup step done", slog.String("step", name))
	}
	final, err := s.writeObserved(ctx, cur, ObservedWrite{
		State: StateDone, ObservedGeneration: cur.DesiredGeneration, Attempts: 0,
		NextAttemptAt: s.now(), ReleaseLease: true,
	})
	if err != nil {
		log.WarnContext(ctx, "record done failed", slog.Any("error", err))
		s.loop.Release(ctx, cur.BotID)
		return
	}
	s.broker.Publish(final.BotID, Event{Type: EventReady, Setup: &final})
	log.InfoContext(ctx, "bot setup complete")
}

func (s *Service) fail(ctx context.Context, log *slog.Logger, st Setup, name string, stepAttempts int32, err error) {
	var step *StepError
	retryable := true
	if errors.As(err, &step) {
		retryable = step.Retryable
	}
	attempts, next := s.backoff.Spend(st.Attempts, retryable)
	status := StatusRetrying
	if attempts >= s.opts.MaxAttempts {
		status = StatusFailed
	}
	message := sanitize(err)
	log.ErrorContext(ctx, "setup step failed", slog.String("step", name), slog.Bool("retryable", retryable),
		slog.Int("attempt", int(attempts)), slog.Time("next_attempt_at", next), slog.Any("error", err))
	cur := s.setStep(ctx, st, Step{Step: name, Status: status, Generation: st.DesiredGeneration, Attempts: stepAttempts + 1, LastError: message})
	final, werr := s.writeObserved(ctx, cur, ObservedWrite{
		State: StateFailed, ObservedGeneration: cur.DesiredGeneration, Attempts: attempts,
		NextAttemptAt: next, ReleaseLease: true,
	})
	if werr != nil {
		log.WarnContext(ctx, "record failure failed", slog.Any("error", werr))
		s.loop.Release(ctx, cur.BotID)
		return
	}
	if !final.RetryPending(s.opts.MaxAttempts) {
		s.broker.Publish(final.BotID, Event{Type: EventError, Step: name, Status: status, Error: message, Setup: &final})
	}
}

// setStep persists a step transition, publishes it and returns the setup with
// the step updated in place.
func (s *Service) setStep(ctx context.Context, st Setup, step Step) Setup {
	wctx, cancel := reconcile.WriteContext(ctx, s.opts.WriteTimeout)
	defer cancel()
	step.UpdatedAt = s.now()
	if err := s.repo.WriteStep(wctx, st.BotID, step); err != nil {
		s.log.WarnContext(ctx, "write setup step failed", slog.String("bot_id", st.BotID), slog.String("step", step.Step), slog.Any("error", err))
	}
	replaced := false
	for i := range st.Steps {
		if st.Steps[i].Step == step.Step {
			st.Steps[i] = step
			replaced = true
		}
	}
	if !replaced {
		st.Steps = append(st.Steps, step)
	}
	s.broker.Publish(st.BotID, Event{Type: EventStep, Step: step.Step, Status: step.Status, Error: step.LastError, Setup: &st})
	return st
}

func (s *Service) workspaceRunning(ctx context.Context, botID string) bool {
	if s.deps.Workspaces == nil {
		return true
	}
	w, err := s.deps.Workspaces.Get(ctx, botID)
	if err != nil {
		return false
	}
	return w.Observed == botworkspace.ObservedRunning
}

func (s *Service) writeObserved(ctx context.Context, st Setup, w ObservedWrite) (Setup, error) {
	w.BotID = st.BotID
	w.Owner = s.opts.Owner
	w.ExpectedVersion = st.Version
	wctx, cancel := reconcile.WriteContext(ctx, s.opts.WriteTimeout)
	defer cancel()
	return s.repo.WriteObserved(wctx, w)
}

const maxErrorRunes = 4096

// sanitize masks credentials an upstream error may quote and bounds the size
// of what is persisted as last_error.
func sanitize(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "setup step failed"
	}
	msg = redact.Diagnostic(msg)
	if r := []rune(msg); len(r) > maxErrorRunes {
		msg = string(r[:maxErrorRunes])
	}
	return msg
}
