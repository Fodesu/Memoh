package botworkspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Backend performs the workspace operations for one runtime (containerd,
// docker, apple, Cloud builtin). Every method is idempotent.
type Backend interface {
	// Provision brings the bot's workspace to running: image, container,
	// start, bridge readiness, template bootstrap. An empty image means "the
	// backend's default for this bot". Failures are *StepError.
	Provision(ctx context.Context, botID, image string, progress func(ProgressEvent)) error
	// Teardown removes the workspace; a missing workspace is success. With
	// preserve the data is exported before deletion and a failed export aborts
	// the teardown.
	Teardown(ctx context.Context, botID string, preserve bool) error
	// Inspect reports the backend's view of the workspace.
	Inspect(ctx context.Context, botID string) (Inspection, error)
	// HasPreservedData reports whether an exported data archive exists.
	HasPreservedData(botID string) bool
}

// BotStatusWriter derives bots.status from the workspace state. Implemented by
// the bots service; injected as an interface so this package does not import
// it.
type BotStatusWriter interface {
	SetBotStatusFromWorkspace(ctx context.Context, botID, status string) error
}

// Options tune the reconciler. Zero values take the defaults.
type Options struct {
	Owner            string
	Interval         time.Duration
	Lease            time.Duration
	DriftInterval    time.Duration
	BackoffBase      time.Duration
	BackoffCap       time.Duration
	MaxAttempts      int32
	Batch            int32
	Concurrency      int
	ProvisionTimeout time.Duration
	TeardownTimeout  time.Duration
	WriteTimeout     time.Duration
}

func (o Options) withDefaults() Options {
	if o.Owner == "" {
		host, _ := os.Hostname()
		o.Owner = strings.TrimSpace(host) + "/" + uuid.NewString()[:8]
	}
	if o.Interval <= 0 {
		o.Interval = 15 * time.Second
	}
	if o.Lease <= 0 {
		o.Lease = 2 * time.Minute
	}
	if o.DriftInterval <= 0 {
		o.DriftInterval = 5 * time.Minute
	}
	// Retry schedule: 10s, 20s, 40s, 80s, 90s, 90s (±20% jitter), about 5.5
	// minutes from the first failure to parking. One attempt is itself
	// expensive (image pull, container start, bridge wait), so it does not
	// start as fast as a cheap API retry; the cap matches the longest an
	// upstream operation is expected to stay in flight (the Cloud control
	// plane's 90s connect deadline) and the direction Kubernetes took for
	// CrashLoopBackOff (KEP-4603 lowers the default cap to 60s).
	if o.BackoffBase <= 0 {
		o.BackoffBase = 10 * time.Second
	}
	if o.BackoffCap <= 0 {
		o.BackoffCap = 90 * time.Second
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 6
	}
	if o.Batch <= 0 {
		o.Batch = 20
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 4
	}
	if o.ProvisionTimeout <= 0 {
		o.ProvisionTimeout = 15 * time.Minute
	}
	if o.TeardownTimeout <= 0 {
		o.TeardownTimeout = 5 * time.Minute
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 15 * time.Second
	}
	return o
}

// Service is the reconciler plus the intent API.
type Service struct {
	repo    Repository
	backend Backend
	log     *slog.Logger
	opts    Options
	now     func() time.Time
	// rnd feeds the backoff jitter; tests pin it.
	rnd func() float64

	statusMu sync.RWMutex
	status   BotStatusWriter

	subsMu sync.Mutex
	subs   map[string]map[chan ProgressEvent]struct{}

	kick      chan struct{}
	lastDrift time.Time

	runMu   sync.Mutex
	running map[string]struct{}

	stop   chan struct{}
	done   chan struct{}
	startd sync.Once
	stopd  sync.Once
}

// New builds a Service. The loop starts with Start.
func New(repo Repository, backend Backend, log *slog.Logger, opts Options) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		repo:    repo,
		backend: backend,
		log:     log.With(slog.String("component", "botworkspace")),
		opts:    opts.withDefaults(),
		now:     time.Now,
		rnd:     rand.Float64,
		subs:    make(map[string]map[chan ProgressEvent]struct{}),
		kick:    make(chan struct{}, 1),
		running: make(map[string]struct{}),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// SetBotStatusWriter wires bots.status derivation (setter injection avoids an
// import cycle with the bots service).
func (s *Service) SetBotStatusWriter(w BotStatusWriter) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.status = w
}

// Owner is this instance's lease owner id.
func (s *Service) Owner() string { return s.opts.Owner }

// ─── Intent API ──────────────────────────────────────────────────────────────

// EnsurePresent records that the bot should have a running workspace built
// from image (empty keeps the previous / default image) and wakes the loop.
func (s *Service) EnsurePresent(ctx context.Context, botID, image string) (Workspace, error) {
	w, err := s.repo.Upsert(ctx, botID, DesiredPresent, strings.TrimSpace(image), false)
	if err != nil {
		return Workspace{}, err
	}
	s.Kick()
	return w, nil
}

// RequestAbsent records that the bot's workspace should be removed. preserve
// exports the data archive before deletion.
func (s *Service) RequestAbsent(ctx context.Context, botID string, preserve bool) (Workspace, error) {
	w, err := s.repo.Upsert(ctx, botID, DesiredAbsent, "", preserve)
	if err != nil {
		return Workspace{}, err
	}
	s.Kick()
	return w, nil
}

// Get returns the row.
func (s *Service) Get(ctx context.Context, botID string) (Workspace, error) {
	return s.repo.Get(ctx, botID)
}

// Kick wakes the loop for an immediate pass.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Subscribe streams progress and terminal events for a bot to an in-process
// listener. Events are dropped when the listener falls behind; Await is the
// reliable way to learn the outcome.
func (s *Service) Subscribe(botID string) (<-chan ProgressEvent, func()) {
	ch := make(chan ProgressEvent, 64)
	s.subsMu.Lock()
	if s.subs[botID] == nil {
		s.subs[botID] = make(map[chan ProgressEvent]struct{})
	}
	s.subs[botID][ch] = struct{}{}
	s.subsMu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.subsMu.Lock()
			if set, ok := s.subs[botID]; ok {
				delete(set, ch)
				if len(set) == 0 {
					delete(s.subs, botID)
				}
			}
			s.subsMu.Unlock()
		})
	}
}

func (s *Service) publish(botID string, ev ProgressEvent) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for ch := range s.subs[botID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Await blocks until the workspace has a final answer for an intent at least
// as new as generation, polling the repository. A failure that still has an
// automatic retry scheduled is not final: Await keeps waiting so a transient
// error that recovers on the next attempt never reaches the caller as a
// failure. It works across Server instances.
func (s *Service) Await(ctx context.Context, botID string, generation int64) (Workspace, error) {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		w, err := s.repo.Get(ctx, botID)
		if err != nil {
			return Workspace{}, err
		}
		if w.DesiredGeneration >= generation && w.Final() {
			return w, nil
		}
		select {
		case <-ctx.Done():
			return w, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Observe refreshes the observation for one bot from the backend (after a
// user-driven start or stop). It never provisions or tears down.
func (s *Service) Observe(ctx context.Context, botID string) (Workspace, error) {
	w, err := s.repo.ClaimOne(ctx, botID, s.opts.Owner, s.opts.Lease)
	if err != nil {
		return Workspace{}, err
	}
	defer s.release(ctx, botID)
	switch w.Observed {
	case ObservedProvisioning, ObservedRemoving:
		return w, nil
	}
	insp, err := s.backend.Inspect(ctx, botID)
	if err != nil {
		return w, err
	}
	observed := ObservedAbsent
	switch {
	case insp.Exists && insp.Running:
		observed = ObservedRunning
	case insp.Exists:
		observed = ObservedStopped
	}
	if observed == w.Observed {
		return w, nil
	}
	if w.Desired == DesiredPresent && observed == ObservedAbsent && !insp.Exists {
		// Drift: the workspace vanished. Make the row due so the loop
		// re-provisions it; ever_ready stays as it was.
		return s.writeObserved(ctx, w, ObservedWrite{
			Observed: ObservedAbsent, ObservedGeneration: w.ObservedGeneration,
			LastError: "workspace not found on the backend", LastErrorPhase: "",
			Attempts: 0, NextAttemptAt: s.now(), ReleaseLease: true,
		})
	}
	return s.writeObserved(ctx, w, ObservedWrite{
		Observed: observed, ObservedGeneration: w.ObservedGeneration, MarkReady: observed == ObservedRunning,
		Attempts: w.Attempts, NextAttemptAt: w.NextAttemptAt, ReleaseLease: true,
	})
}

// ─── Loop ────────────────────────────────────────────────────────────────────

// Start launches the loop; it returns immediately.
func (s *Service) Start(ctx context.Context) error {
	s.startd.Do(func() {
		go s.loop(context.WithoutCancel(ctx))
	})
	return nil
}

// Stop asks the loop to exit and waits for in-flight passes to release their
// leases (bounded by ctx).
func (s *Service) Stop(ctx context.Context) error {
	s.stopd.Do(func() { close(s.stop) })
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) loop(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()
	var wg sync.WaitGroup
	for {
		select {
		case <-s.stop:
			wg.Wait()
			return
		case <-ticker.C:
		case <-s.kick:
		}
		if _, err := s.reconcileOnce(ctx, &wg); err != nil {
			s.log.Error("reconcile pass failed", slog.Any("error", err))
		}
		if s.now().Sub(s.lastDrift) >= s.opts.DriftInterval {
			s.lastDrift = s.now()
			s.detectDrift(ctx)
		}
	}
}

// ReconcileOnce runs a single pass synchronously (tests, admin tooling).
func (s *Service) ReconcileOnce(ctx context.Context) (int, error) {
	var wg sync.WaitGroup
	n, err := s.reconcileOnce(ctx, &wg)
	wg.Wait()
	return n, err
}

func (s *Service) reconcileOnce(ctx context.Context, wg *sync.WaitGroup) (int, error) {
	claimCtx, cancel := context.WithTimeout(ctx, s.opts.WriteTimeout)
	rows, err := s.repo.Claim(claimCtx, s.opts.Owner, s.opts.Lease, s.opts.Batch)
	cancel()
	if err != nil {
		return 0, err
	}
	sem := make(chan struct{}, s.opts.Concurrency)
	started := 0
	for _, w := range rows {
		if !s.markRunning(w.BotID) {
			s.release(ctx, w.BotID)
			continue
		}
		started++
		wg.Add(1)
		sem <- struct{}{}
		go func(w Workspace) {
			defer wg.Done()
			defer func() { <-sem }()
			defer s.unmarkRunning(w.BotID)
			s.reconcileOne(ctx, w)
		}(w)
	}
	return started, nil
}

func (s *Service) markRunning(botID string) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if _, ok := s.running[botID]; ok {
		return false
	}
	s.running[botID] = struct{}{}
	return true
}

func (s *Service) unmarkRunning(botID string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	delete(s.running, botID)
}

func (s *Service) reconcileOne(ctx context.Context, w Workspace) {
	log := s.log.With(slog.String("bot_id", w.BotID), slog.String("desired", w.Desired), slog.String("observed", w.Observed), slog.Int64("generation", w.DesiredGeneration))
	switch Decide(w, s.now()) {
	case ActionNone:
		if w.ObservedGeneration < w.DesiredGeneration {
			if _, err := s.writeObserved(ctx, w, ObservedWrite{
				Observed: w.Observed, ObservedGeneration: w.DesiredGeneration,
				Attempts: 0, NextAttemptAt: s.now(), ReleaseLease: true,
			}); err != nil {
				log.Warn("catch up generation failed", slog.Any("error", err))
			}
			return
		}
		s.release(ctx, w.BotID)
	case ActionWait:
		s.release(ctx, w.BotID)
	case ActionProvision:
		s.provision(ctx, log, w)
	case ActionTeardown:
		s.teardown(ctx, log, w)
	}
}

func (s *Service) provision(ctx context.Context, log *slog.Logger, w Workspace) {
	opCtx, cancel := context.WithTimeout(ctx, s.opts.ProvisionTimeout)
	defer cancel()
	stopRenew := s.renewLoop(opCtx, w.BotID)
	defer stopRenew()

	// A half-provisioned workspace from a previous attempt is replaced only
	// when the authorization rule allows it; otherwise the backend reuses it.
	if w.Observed == ObservedFailed && MayDeleteData(w, s.backend.HasPreservedData(w.BotID)) {
		if err := s.backend.Teardown(opCtx, w.BotID, false); err != nil {
			s.fail(ctx, log, w, &StepError{Phase: PhaseTeardown, Retryable: true, Err: err})
			return
		}
	}

	cur, err := s.writeObserved(ctx, w, ObservedWrite{
		Observed: ObservedProvisioning, ObservedGeneration: w.DesiredGeneration,
		Attempts: w.Attempts, NextAttemptAt: s.now(), ReleaseLease: false,
	})
	if err != nil {
		log.Warn("enter provisioning failed", slog.Any("error", err))
		s.release(ctx, w.BotID)
		return
	}
	s.deriveBotStatus(ctx, cur)

	err = s.backend.Provision(opCtx, w.BotID, w.Image, func(ev ProgressEvent) { s.publish(w.BotID, ev) })
	if err != nil {
		var step *StepError
		if !errors.As(err, &step) {
			step = &StepError{Phase: PhaseStart, Retryable: true, Err: err}
		}
		if opCtx.Err() != nil && step.Err == nil {
			step.Err = opCtx.Err()
		}
		s.fail(ctx, log, cur, step)
		return
	}

	final, err := s.writeObserved(ctx, cur, ObservedWrite{
		Observed: ObservedRunning, ObservedGeneration: cur.DesiredGeneration, MarkReady: true,
		Attempts: 0, NextAttemptAt: s.now(), ReleaseLease: true,
	})
	if err != nil {
		log.Warn("record running failed", slog.Any("error", err))
		s.release(ctx, w.BotID)
		return
	}
	s.deriveBotStatus(ctx, final)
	s.publish(w.BotID, ProgressEvent{Type: EventReady, Workspace: &final})
	log.Info("workspace provisioned")
}

func (s *Service) fail(ctx context.Context, log *slog.Logger, w Workspace, step *StepError) {
	attempts := w.Attempts + 1
	next := farFuture
	if step.Retryable && attempts < s.opts.MaxAttempts {
		next = s.nextAttempt(attempts)
	}
	message := sanitize(step.Err)
	log.Error("workspace provisioning failed",
		slog.String("phase", step.Phase), slog.Bool("retryable", step.Retryable),
		slog.Int("attempt", int(attempts)), slog.Time("next_attempt_at", next), slog.Any("error", step.Err))
	final, err := s.writeObserved(ctx, w, ObservedWrite{
		Observed: ObservedFailed, ObservedGeneration: w.DesiredGeneration,
		LastError: message, LastErrorPhase: step.Phase,
		Attempts: attempts, NextAttemptAt: next, ReleaseLease: true,
	})
	if err != nil {
		log.Warn("record failure failed", slog.Any("error", err))
		s.release(ctx, w.BotID)
		return
	}
	s.deriveBotStatus(ctx, final)
	s.publish(w.BotID, ProgressEvent{Type: EventError, Phase: step.Phase, Err: step.Err, Message: message, Workspace: &final})
}

func (s *Service) teardown(ctx context.Context, log *slog.Logger, w Workspace) {
	opCtx, cancel := context.WithTimeout(ctx, s.opts.TeardownTimeout)
	defer cancel()
	stopRenew := s.renewLoop(opCtx, w.BotID)
	defer stopRenew()

	cur, err := s.writeObserved(ctx, w, ObservedWrite{
		Observed: ObservedRemoving, ObservedGeneration: w.DesiredGeneration,
		Attempts: w.Attempts, NextAttemptAt: s.now(), ReleaseLease: false,
	})
	if err != nil {
		log.Warn("enter removing failed", slog.Any("error", err))
		s.release(ctx, w.BotID)
		return
	}

	if err := s.backend.Teardown(opCtx, w.BotID, w.PreserveData); err != nil {
		attempts := cur.Attempts + 1
		observed := ObservedRemoving
		next := s.nextAttempt(attempts)
		if attempts >= s.opts.MaxAttempts {
			observed = ObservedFailed
			next = farFuture
		}
		log.Error("workspace teardown failed", slog.Int("attempt", int(attempts)), slog.Any("error", err))
		if _, werr := s.writeObserved(ctx, cur, ObservedWrite{
			Observed: observed, ObservedGeneration: cur.DesiredGeneration,
			LastError: sanitize(err), LastErrorPhase: PhaseTeardown,
			Attempts: attempts, NextAttemptAt: next, ReleaseLease: true,
		}); werr != nil {
			log.Warn("record teardown failure failed", slog.Any("error", werr))
			s.release(ctx, w.BotID)
		}
		return
	}

	final, err := s.writeObserved(ctx, cur, ObservedWrite{
		Observed: ObservedAbsent, ObservedGeneration: cur.DesiredGeneration,
		Attempts: 0, NextAttemptAt: s.now(), ReleaseLease: true,
	})
	if err != nil {
		log.Warn("record absent failed", slog.Any("error", err))
		s.release(ctx, w.BotID)
		return
	}
	s.publish(w.BotID, ProgressEvent{Type: EventReady, Workspace: &final})
	log.Info("workspace removed")
}

// detectDrift re-inspects running workspaces that have not been touched for
// a drift interval so a vanished container is re-provisioned.
func (s *Service) detectDrift(ctx context.Context) {
	listCtx, cancel := context.WithTimeout(ctx, s.opts.WriteTimeout)
	rows, err := s.repo.ListByObserved(listCtx, ObservedRunning, 500)
	cancel()
	if err != nil {
		s.log.Warn("drift scan failed", slog.Any("error", err))
		return
	}
	cutoff := s.now().Add(-s.opts.DriftInterval)
	for _, w := range rows {
		if w.UpdatedAt.After(cutoff) {
			continue
		}
		obsCtx, cancel := context.WithTimeout(ctx, s.opts.WriteTimeout)
		if _, err := s.Observe(obsCtx, w.BotID); err != nil && !errors.Is(err, ErrNotFound) {
			s.log.Warn("drift observe failed", slog.String("bot_id", w.BotID), slog.Any("error", err))
		}
		cancel()
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// backoffJitter spreads retries of rows that failed together (a registry
// hiccup across many bots, several Server instances) over ±20% so they do not
// come back as one spike.
const backoffJitter = 0.2

// nextAttempt schedules the retry after attempt number `attempts` failed.
func (s *Service) nextAttempt(attempts int32) time.Time {
	now := s.now()
	d := NextBackoff(now, attempts, s.opts.BackoffBase, s.opts.BackoffCap).Sub(now)
	factor := 1 - backoffJitter + 2*backoffJitter*s.rnd()
	return now.Add(time.Duration(float64(d) * factor))
}

// writeObserved persists an observation on a fresh short-lived context so an
// exhausted operation context can never lose the outcome.
func (s *Service) writeObserved(ctx context.Context, w Workspace, write ObservedWrite) (Workspace, error) {
	write.BotID = w.BotID
	write.Owner = s.opts.Owner
	write.ExpectedVersion = w.Version
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.opts.WriteTimeout)
	defer cancel()
	return s.repo.WriteObserved(wctx, write)
}

func (s *Service) release(ctx context.Context, botID string) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.opts.WriteTimeout)
	defer cancel()
	if err := s.repo.Release(rctx, botID, s.opts.Owner); err != nil {
		s.log.Warn("release lease failed", slog.String("bot_id", botID), slog.Any("error", err))
	}
}

func (s *Service) renewLoop(ctx context.Context, botID string) func() {
	interval := s.opts.Lease / 3
	if interval < time.Second {
		interval = time.Second
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.opts.WriteTimeout)
				if err := s.repo.Renew(rctx, botID, s.opts.Owner, s.opts.Lease); err != nil {
					s.log.Warn("renew lease failed", slog.String("bot_id", botID), slog.Any("error", err))
				}
				cancel()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

func (s *Service) deriveBotStatus(ctx context.Context, w Workspace) {
	s.statusMu.RLock()
	writer := s.status
	s.statusMu.RUnlock()
	if writer == nil {
		return
	}
	status, ok := DeriveBotStatus(w)
	if !ok {
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.opts.WriteTimeout)
	defer cancel()
	if err := writer.SetBotStatusFromWorkspace(wctx, w.BotID, status); err != nil {
		s.log.Warn("derive bot status failed", slog.String("bot_id", w.BotID), slog.String("status", status), slog.Any("error", err))
	}
}

const maxErrorRunes = 4096

func sanitize(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "workspace operation failed"
	}
	if r := []rune(msg); len(r) > maxErrorRunes {
		msg = string(r[:maxErrorRunes])
	}
	return msg
}

// String renders a workspace for logs.
func (w Workspace) String() string {
	return fmt.Sprintf("bot=%s desired=%s/%d observed=%s/%d attempts=%d", w.BotID, w.Desired, w.DesiredGeneration, w.Observed, w.ObservedGeneration, w.Attempts)
}
