package botsetup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/botworkspace"
	"github.com/felinics/memoh/internal/reconcile"
	"github.com/felinics/memoh/internal/settings"
)

// memRepo mirrors the SQL semantics in Go: due predicate, lease, version.
type memRepo struct {
	mu   sync.Mutex
	rows map[string]*Setup
	now  func() time.Time
}

func newMemRepo(now func() time.Time) *memRepo { return &memRepo{rows: map[string]*Setup{}, now: now} }

func (m *memRepo) get(botID string) Setup {
	m.mu.Lock()
	defer m.mu.Unlock()
	return copySetup(*m.rows[botID])
}

func copySetup(s Setup) Setup {
	s.Steps = append([]Step(nil), s.Steps...)
	return s
}

func (m *memRepo) Upsert(_ context.Context, botID, requestedBy string, spec Spec) (Setup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[botID]
	if !ok {
		row = &Setup{BotID: botID, DesiredGeneration: 0, Version: 0}
		m.rows[botID] = row
	}
	row.DesiredGeneration++
	row.Spec = spec
	if requestedBy != "" {
		row.RequestedBy = requestedBy
	}
	row.State, row.Attempts, row.NextAttemptAt, row.Version = StatePending, 0, m.now(), row.Version+1
	row.Steps = row.Steps[:0]
	for _, name := range StepOrder {
		status := StatusSkipped
		if spec.Manages(name) {
			status = StatusPending
		}
		row.Steps = append(row.Steps, Step{Step: name, Status: status, Generation: row.DesiredGeneration})
	}
	return copySetup(*row), nil
}

func (m *memRepo) Retry(_ context.Context, botID string) (Setup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[botID]
	if !ok {
		return Setup{}, ErrNotFound
	}
	row.DesiredGeneration++
	row.State, row.Attempts, row.NextAttemptAt, row.Version = StatePending, 0, m.now(), row.Version+1
	for i := range row.Steps {
		if row.Steps[i].Status != StatusDone && row.Steps[i].Status != StatusSkipped {
			row.Steps[i].Status, row.Steps[i].Attempts, row.Steps[i].LastError = StatusPending, 0, ""
		}
	}
	return copySetup(*row), nil
}

func (m *memRepo) Get(_ context.Context, botID string) (Setup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[botID]
	if !ok {
		return Setup{}, ErrNotFound
	}
	return copySetup(*row), nil
}

func (m *memRepo) due(row *Setup) bool {
	now := m.now()
	return !row.NextAttemptAt.After(now) &&
		(row.LeaseUntil.IsZero() || row.LeaseUntil.Before(now)) &&
		(row.ObservedGeneration < row.DesiredGeneration || row.State != StateDone)
}

func (m *memRepo) Claim(_ context.Context, owner string, lease time.Duration, limit int32) ([]Setup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Setup
	for _, row := range m.rows {
		if int32(len(out)) >= limit || !m.due(row) { //nolint:gosec // test sizes
			continue
		}
		row.LeaseOwner, row.LeaseUntil, row.Version = owner, m.now().Add(lease), row.Version+1
		out = append(out, copySetup(*row))
	}
	return out, nil
}

func (m *memRepo) Renew(_ context.Context, botID, owner string, lease time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[botID]
	if !ok || row.LeaseOwner != owner {
		return ErrVersionConflict
	}
	row.LeaseUntil = m.now().Add(lease)
	return nil
}

func (m *memRepo) WriteObserved(_ context.Context, w ObservedWrite) (Setup, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[w.BotID]
	if !ok || row.LeaseOwner != w.Owner || row.Version != w.ExpectedVersion {
		return Setup{}, ErrVersionConflict
	}
	row.State, row.ObservedGeneration, row.Attempts, row.NextAttemptAt = w.State, w.ObservedGeneration, w.Attempts, w.NextAttemptAt
	if w.ReleaseLease {
		row.LeaseOwner, row.LeaseUntil = "", time.Time{}
	}
	row.Version++
	return copySetup(*row), nil
}

func (m *memRepo) Release(_ context.Context, botID, owner string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.rows[botID]; ok && row.LeaseOwner == owner {
		row.LeaseOwner, row.LeaseUntil = "", time.Time{}
	}
	return nil
}

func (m *memRepo) WriteStep(_ context.Context, botID string, step Step) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := m.rows[botID]
	for i := range row.Steps {
		if row.Steps[i].Step == step.Step {
			row.Steps[i] = step
			return nil
		}
	}
	row.Steps = append(row.Steps, step)
	return nil
}

type fakeSettings struct {
	current settings.Settings
	upserts []settings.UpsertRequest
	err     error
}

func (f *fakeSettings) GetBot(context.Context, string) (settings.Settings, error) {
	return f.current, nil
}

func (f *fakeSettings) UpsertBot(_ context.Context, _ string, req settings.UpsertRequest) (settings.Settings, error) {
	if f.err != nil {
		return settings.Settings{}, f.err
	}
	f.upserts = append(f.upserts, req)
	if req.ChatModelID != nil {
		f.current.ChatModelID = *req.ChatModelID
	}
	return f.current, nil
}

type fakeGrants struct {
	existing []bots.UserGrant
	created  []bots.CreateUserGrantRequest
	failFor  string
}

func (f *fakeGrants) ListUserGrants(context.Context, string) ([]bots.UserGrant, error) {
	return f.existing, nil
}

func (f *fakeGrants) CreateUserGrant(_ context.Context, _, _ string, req bots.CreateUserGrantRequest) (bots.UserGrant, error) {
	if req.UserID == f.failFor {
		return bots.UserGrant{}, errors.New("user not found")
	}
	f.created = append(f.created, req)
	f.existing = append(f.existing, bots.UserGrant{SubjectType: req.SubjectType, UserID: req.UserID})
	return bots.UserGrant{}, nil
}

type fakeWorkspaces struct{ observed string }

func (f *fakeWorkspaces) Get(context.Context, string) (botworkspace.Workspace, error) {
	return botworkspace.Workspace{Observed: f.observed}, nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestService(t *testing.T, deps Deps) (*Service, *memRepo, *clock) {
	t.Helper()
	clk := &clock{t: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	repo := newMemRepo(clk.now)
	svc := New(repo, deps, nil, Options{Options: reconcile.Options{Owner: "test", MaxAttempts: 3, BackoffBase: 30 * time.Second, BackoffCap: 10 * time.Minute}})
	svc.now = clk.now
	svc.rnd = func() float64 { return 0.5 }
	return svc, repo, clk
}

const bot = "00000000-0000-0000-0000-00000000aaaa"

func strp(s string) *string { return &s }

func TestSetupAppliesSettingsAndGrantsOnce(t *testing.T) {
	st := &fakeSettings{}
	gr := &fakeGrants{}
	svc, repo, _ := newTestService(t, Deps{Settings: st, Grants: gr, Workspaces: &fakeWorkspaces{observed: botworkspace.ObservedRunning}})
	ctx := context.Background()
	spec := Spec{
		Settings: &settings.UpsertRequest{ChatModelID: strp("m1")},
		Grants:   []bots.CreateUserGrantRequest{{SubjectType: "user", UserID: "u2", Permissions: []string{"chat"}}},
	}
	if _, err := svc.EnsureSetup(ctx, bot, "owner", spec); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.ReconcileOnce(ctx); n != 1 {
		t.Fatalf("claimed %d rows, want 1", n)
	}
	got := repo.get(bot)
	if got.State != StateDone || got.ObservedGeneration != 1 || got.LeaseOwner != "" {
		t.Fatalf("after pass: %+v", got)
	}
	for _, name := range []string{StepSettings, StepGrants} {
		if s, _ := got.StepByName(name); s.Status != StatusDone {
			t.Fatalf("step %s = %+v, want done", name, s)
		}
	}
	if s, _ := got.StepByName(StepAgent); s.Status != StatusSkipped {
		t.Fatalf("unmanaged step should be skipped: %+v", s)
	}
	if len(st.upserts) != 1 || len(gr.created) != 1 {
		t.Fatalf("upserts=%d created=%d, want 1/1", len(st.upserts), len(gr.created))
	}
	// Re-asserting the same intent re-verifies but applies nothing again.
	if _, err := svc.EnsureSetup(ctx, bot, "owner", spec); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.ReconcileOnce(ctx); n != 1 {
		t.Fatalf("second claim = %d, want 1", n)
	}
	if len(st.upserts) != 1 || len(gr.created) != 1 {
		t.Fatalf("second pass re-applied: upserts=%d created=%d", len(st.upserts), len(gr.created))
	}
	if got := repo.get(bot); got.State != StateDone || got.ObservedGeneration != 2 {
		t.Fatalf("after second pass: %+v", got)
	}
}

func TestSetupWaitsForRunningWorkspaceWithoutSpendingBudget(t *testing.T) {
	ws := &fakeWorkspaces{observed: botworkspace.ObservedProvisioning}
	st := &fakeSettings{}
	svc, repo, clk := newTestService(t, Deps{Settings: st, Workspaces: ws})
	ctx := context.Background()
	_, _ = svc.EnsureSetup(ctx, bot, "", Spec{Settings: &settings.UpsertRequest{ChatModelID: strp("m1")}})
	_, _ = svc.ReconcileOnce(ctx)
	got := repo.get(bot)
	if got.State != StateWaiting || got.Attempts != 0 || len(st.upserts) != 0 {
		t.Fatalf("waiting row: %+v upserts=%d", got, len(st.upserts))
	}
	if n, _ := svc.ReconcileOnce(ctx); n != 0 {
		t.Fatalf("waiting row claimed again before its interval (%d)", n)
	}
	clk.advance(11 * time.Second)
	ws.observed = botworkspace.ObservedRunning
	_, _ = svc.ReconcileOnce(ctx)
	if got := repo.get(bot); got.State != StateDone || len(st.upserts) != 1 {
		t.Fatalf("after workspace ran: %+v upserts=%d", got, len(st.upserts))
	}
}

func TestSetupRetriesFailedStepThenSpendsBudget(t *testing.T) {
	st := &fakeSettings{err: errors.New("settings store down")}
	svc, repo, clk := newTestService(t, Deps{Settings: st, Workspaces: &fakeWorkspaces{observed: botworkspace.ObservedRunning}})
	ctx := context.Background()
	_, _ = svc.EnsureSetup(ctx, bot, "", Spec{Settings: &settings.UpsertRequest{ChatModelID: strp("m1")}})

	_, _ = svc.ReconcileOnce(ctx)
	got := repo.get(bot)
	step, _ := got.StepByName(StepSettings)
	if got.State != StateFailed || got.Attempts != 1 || !got.RetryPending(3) || step.Status != StatusRetrying || step.LastError == "" {
		t.Fatalf("after first failure: %+v step=%+v", got, step)
	}
	if !got.NextAttemptAt.Equal(clk.now().Add(30 * time.Second)) {
		t.Fatalf("backoff = %s, want +30s", got.NextAttemptAt.Sub(clk.now()))
	}
	if got.Final(3) {
		t.Fatal("a retry inside the budget must not be final")
	}
	for i := 0; i < 2; i++ {
		clk.advance(time.Hour)
		_, _ = svc.ReconcileOnce(ctx)
	}
	got = repo.get(bot)
	step, _ = got.StepByName(StepSettings)
	if got.Attempts != 3 || !got.Final(3) || step.Status != StatusFailed {
		t.Fatalf("after budget spent: %+v step=%+v", got, step)
	}
	// Recovery: the store comes back and a Retry re-runs only the failed step.
	st.err = nil
	if _, err := svc.Retry(ctx, bot); err != nil {
		t.Fatal(err)
	}
	_, _ = svc.ReconcileOnce(ctx)
	if got := repo.get(bot); got.State != StateDone || len(st.upserts) != 1 {
		t.Fatalf("after retry: %+v upserts=%d", got, len(st.upserts))
	}
}

func TestGrantsStepRecordsPartialFailureAndCompletes(t *testing.T) {
	gr := &fakeGrants{failFor: "u3"}
	svc, repo, _ := newTestService(t, Deps{Grants: gr, Workspaces: &fakeWorkspaces{observed: botworkspace.ObservedRunning}})
	ctx := context.Background()
	_, _ = svc.EnsureSetup(ctx, bot, "owner", Spec{Grants: []bots.CreateUserGrantRequest{
		{SubjectType: "user", UserID: "u2", Permissions: []string{"chat"}},
		{SubjectType: "user", UserID: "u3", Permissions: []string{"chat"}},
	}})
	_, _ = svc.ReconcileOnce(ctx)
	got := repo.get(bot)
	step, _ := got.StepByName(StepGrants)
	if got.State != StateDone || step.Status != StatusDone || step.LastError == "" || len(gr.created) != 1 {
		t.Fatalf("partial grants: %+v step=%+v created=%d", got, step, len(gr.created))
	}
}

func TestEnsureSetupRejectsEmptySpec(t *testing.T) {
	svc, _, _ := newTestService(t, Deps{})
	if _, err := svc.EnsureSetup(context.Background(), bot, "", Spec{}); !errors.Is(err, ErrEmptySpec) {
		t.Fatalf("EnsureSetup(empty) = %v, want ErrEmptySpec", err)
	}
}

func TestAwaitReturnsWhenSetupIsFinal(t *testing.T) {
	st := &fakeSettings{}
	svc, _, _ := newTestService(t, Deps{Settings: st, Workspaces: &fakeWorkspaces{observed: botworkspace.ObservedRunning}})
	ctx := context.Background()
	intent, _ := svc.EnsureSetup(ctx, bot, "", Spec{Settings: &settings.UpsertRequest{ChatModelID: strp("m1")}})
	events, unsubscribe := svc.Subscribe(bot)
	defer unsubscribe()
	_, _ = svc.ReconcileOnce(ctx)
	got, err := svc.Await(ctx, bot, intent.DesiredGeneration)
	if err != nil || got.State != StateDone {
		t.Fatalf("Await() = %+v, %v", got, err)
	}
	var types []string
	for len(events) > 0 {
		ev := <-events
		types = append(types, ev.Type+":"+ev.Step+":"+ev.Status)
	}
	want := []string{"setup_step:settings:running", "setup_step:settings:done", "ready::"}
	if len(types) != len(want) {
		t.Fatalf("events = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("events = %v, want %v", types, want)
		}
	}
}
