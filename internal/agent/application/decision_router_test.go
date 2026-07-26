package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/memohai/memoh/internal/agent/decision"
	userinput "github.com/memohai/memoh/internal/agent/decision/input"
	agentpkg "github.com/memohai/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/memohai/memoh/internal/agent/runtime/session"
	"github.com/memohai/memoh/internal/agent/turn"
	conversation "github.com/memohai/memoh/internal/agent/view"
	"github.com/memohai/memoh/internal/runtimefence"
)

const (
	decisionBotID     = "11111111-1111-1111-1111-111111111111"
	decisionSessionID = "22222222-2222-2222-2222-222222222222"
	decisionTargetID  = "33333333-3333-3333-3333-333333333333"
)

type routerTestResolver struct {
	prepared         runtimefence.PreservedDecision
	respondApproval  []ToolApprovalResponseInput
	respondInput     []UserInputResponseInput
	allocated        int
	activated        []runtimefence.ActivationOptions
	respondEvents    []agentpkg.StreamEvent
	respondFence     runtimefence.Fence
	reconcileHandled bool
	reconcileErr     error
	reconcileCalls   int
	commitErr        error
	lifecycle        []string
	runtimeMode      decision.ContinuationMode
	runtimeWaiter    bool
}

func (r *routerTestResolver) AllocateRuntimePersistenceFence(context.Context, string, string) (runtimefence.Fence, error) {
	r.allocated++
	return runtimefence.Fence{BotID: decisionBotID, SessionID: decisionSessionID, Token: 7}, nil
}

func (r *routerTestResolver) ActivateRuntimePersistenceFenceWithOptions(_ context.Context, _ runtimefence.Fence, options runtimefence.ActivationOptions) error {
	r.activated = append(r.activated, options)
	return nil
}

func (r *routerTestResolver) PrepareToolApprovalResponseTarget(context.Context, ToolApprovalResponseInput) (runtimefence.PreservedDecision, error) {
	return r.prepared, nil
}

func (r *routerTestResolver) PrepareUserInputResponseTarget(context.Context, UserInputResponseInput) (runtimefence.PreservedDecision, error) {
	return r.prepared, nil
}

func (r *routerTestResolver) PrepareToolApprovalRuntimeTarget(context.Context, ToolApprovalResponseInput) (RuntimeDecisionTarget, error) {
	return RuntimeDecisionTarget{Decision: r.prepared, ContinuationMode: r.runtimeModeOrDefault(), HasLocalWaiter: r.runtimeWaiter}, nil
}

func (r *routerTestResolver) PrepareUserInputRuntimeTarget(context.Context, UserInputResponseInput) (RuntimeDecisionTarget, error) {
	return RuntimeDecisionTarget{Decision: r.prepared, ContinuationMode: r.runtimeModeOrDefault(), HasLocalWaiter: r.runtimeWaiter}, nil
}

func (r *routerTestResolver) runtimeModeOrDefault() decision.ContinuationMode {
	if r.runtimeMode == "" {
		return decision.ContinuationModeDurable
	}
	return r.runtimeMode
}

func (r *routerTestResolver) RespondToolApproval(ctx context.Context, input turn.ToolApprovalResponse, eventCh chan<- StreamEventPayload) error {
	r.respondApproval = append(r.respondApproval, ToolApprovalResponseInput{
		BotID: input.BotID, ThreadID: input.ThreadID, ActorChannelIdentityID: input.ActorChannelIdentityID,
		ActorUserID: input.ActorUserID, ApprovalID: input.ApprovalID, ExplicitID: input.ExplicitID,
		ReplyExternalMessageID: input.ReplyExternalMessageID, Decision: input.Decision, Reason: input.Reason,
		ChatToken: input.ChatToken, SuppressActivePromptAttach: input.SuppressActivePromptAttach, ResolveOnly: input.ResolveOnly,
	})
	r.respondFence, _ = runtimefence.FromContext(ctx)
	return emitRouterTestEvents(eventCh, r.respondEvents)
}

func (r *routerTestResolver) RespondUserInput(ctx context.Context, input turn.UserInputResponse, eventCh chan<- StreamEventPayload) error {
	answers := make([]userinput.QuestionAnswer, len(input.Answers))
	for i := range input.Answers {
		answers[i] = userinput.QuestionAnswer{
			QuestionID: input.Answers[i].QuestionID, OptionIDs: input.Answers[i].OptionIDs,
			CustomText: input.Answers[i].CustomText, Text: input.Answers[i].Text, Skipped: input.Answers[i].Skipped,
		}
	}
	r.respondInput = append(r.respondInput, UserInputResponseInput{
		BotID: input.BotID, ThreadID: input.ThreadID, ActorChannelIdentityID: input.ActorChannelIdentityID,
		ActorUserID: input.ActorUserID, UserInputID: input.UserInputID, ExplicitID: input.ExplicitID,
		ReplyExternalMessageID: input.ReplyExternalMessageID, Answers: answers, TextAnswer: input.TextAnswer,
		Canceled: input.Canceled, Reason: input.Reason, ChatToken: input.ChatToken,
		SuppressActivePromptAttach: input.SuppressActivePromptAttach, ResolveOnly: input.ResolveOnly,
	})
	r.respondFence, _ = runtimefence.FromContext(ctx)
	return emitRouterTestEvents(eventCh, r.respondEvents)
}

func (r *routerTestResolver) CommitToolApprovalResponse(ctx context.Context, input ToolApprovalResponseInput) (CommittedToolApprovalResponse, error) {
	r.respondApproval = append(r.respondApproval, input)
	r.respondFence, _ = runtimefence.FromContext(ctx)
	r.lifecycle = append(r.lifecycle, "commit")
	return CommittedToolApprovalResponse{}, r.commitErr
}

func (r *routerTestResolver) ContinueCommittedToolApprovalResponse(ctx context.Context, _ CommittedToolApprovalResponse, eventCh chan<- StreamEventPayload) error {
	r.respondFence, _ = runtimefence.FromContext(ctx)
	r.lifecycle = append(r.lifecycle, "continue")
	return emitRouterTestEvents(eventCh, r.respondEvents)
}

func (r *routerTestResolver) CommitUserInputResponse(ctx context.Context, input UserInputResponseInput) (CommittedUserInputResponse, error) {
	r.respondInput = append(r.respondInput, input)
	r.respondFence, _ = runtimefence.FromContext(ctx)
	r.lifecycle = append(r.lifecycle, "commit")
	return CommittedUserInputResponse{}, r.commitErr
}

func (r *routerTestResolver) ContinueCommittedUserInputResponse(ctx context.Context, _ CommittedUserInputResponse, eventCh chan<- StreamEventPayload) error {
	r.respondFence, _ = runtimefence.FromContext(ctx)
	r.lifecycle = append(r.lifecycle, "continue")
	return emitRouterTestEvents(eventCh, r.respondEvents)
}

func (r *routerTestResolver) ReconcileToolApprovalResponse(context.Context, ToolApprovalResponseInput) (bool, error) {
	r.reconcileCalls++
	return r.reconcileHandled, r.reconcileErr
}

func (r *routerTestResolver) ReconcileUserInputResponse(context.Context, UserInputResponseInput) (bool, error) {
	r.reconcileCalls++
	return r.reconcileHandled, r.reconcileErr
}

func (*routerTestResolver) DeferSessionCompaction(string, string, string) func() {
	return func() {}
}

func emitRouterTestEvents(eventCh chan<- StreamEventPayload, events []agentpkg.StreamEvent) error {
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}
		eventCh <- raw
	}
	return nil
}

type routerTestManager struct {
	distributed        bool
	dispatchHandled    bool
	dispatchErr        error
	invokeHandler      bool
	commandHandlers    map[string]sessionruntime.CommandHandler
	commandReconcilers map[string]sessionruntime.CommandReconciler
	starts             int
	startStreamIDs     []string
	startContinuations []bool
	validations        int
	handledEvents      []agentpkg.StreamEvent
	finalizedEvents    []agentpkg.StreamEvent
	canonicalReady     []bool
	finalizeErr        error
	finishes           int
	ownershipCancel    context.CancelCauseFunc
	admissions         []sessionruntime.RunAdmissionView
}

func (m *routerTestManager) RegisterCommandHandler(commandType string, handler sessionruntime.CommandHandler, reconciler sessionruntime.CommandReconciler) error {
	if m.commandHandlers == nil {
		m.commandHandlers = make(map[string]sessionruntime.CommandHandler)
		m.commandReconcilers = make(map[string]sessionruntime.CommandReconciler)
	}
	m.commandHandlers[commandType] = handler
	m.commandReconcilers[commandType] = reconciler
	return nil
}

func (m *routerTestManager) DispatchActiveCommand(ctx context.Context, botID, sessionID, commandType, targetID string, payload []byte) (bool, error) {
	handler := m.commandHandlers[commandType]
	if !m.dispatchHandled || !m.invokeHandler || handler == nil {
		return m.dispatchHandled, m.dispatchErr
	}
	err := handler(ctx, sessionruntime.Command{
		Type: commandType, BotID: botID, SessionID: sessionID, StreamID: "active-stream",
		Generation: "active-generation", TargetID: targetID, Payload: payload,
	})
	return true, err
}

func (m *routerTestManager) IsDistributed() bool { return m.distributed }

func (m *routerTestManager) ValidateRunOwnership(context.Context, sessionruntime.RunHandle) error {
	m.validations++
	return nil
}

func (m *routerTestManager) StartRunWithOptions(ctx context.Context, options sessionruntime.RunStartOptions) (sessionruntime.RunHandle, error) {
	m.starts++
	m.startStreamIDs = append(m.startStreamIDs, options.StreamID)
	m.startContinuations = append(m.startContinuations, options.DecisionContinuation)
	m.ownershipCancel = options.OwnershipCancel
	handle := sessionruntime.RunHandle{
		BotID: options.BotID, SessionID: options.SessionID, StreamID: options.StreamID, Generation: "generation-1",
	}
	if options.AdmissionBuilder != nil {
		admission, err := options.AdmissionBuilder(ctx, handle)
		if err != nil {
			return sessionruntime.RunHandle{}, err
		}
		m.admissions = append(m.admissions, admission)
	}
	return handle, nil
}

func (m *routerTestManager) HandleAgentEvent(_ context.Context, _ sessionruntime.RunHandle, event agentpkg.StreamEvent) ([]conversation.UIMessage, error) {
	m.handledEvents = append(m.handledEvents, event)
	return nil, nil
}

func (m *routerTestManager) FinalizeAgentEvent(_ context.Context, _ sessionruntime.RunHandle, event agentpkg.StreamEvent, canonicalReady bool, _ string) ([]conversation.UIMessage, error) {
	m.finalizedEvents = append(m.finalizedEvents, event)
	m.canonicalReady = append(m.canonicalReady, canonicalReady)
	return nil, m.finalizeErr
}

func (m *routerTestManager) FinishRun(context.Context, sessionruntime.RunHandle, string, string) error {
	m.finishes++
	if m.ownershipCancel != nil {
		m.ownershipCancel(sessionruntime.ErrRunOwnershipLost)
	}
	return nil
}

func TestDecisionRouterRoutesActiveApprovalToCanonicalOwner(t *testing.T) {
	resolver := &routerTestResolver{prepared: runtimefence.PreservedDecision{
		Kind: runtimefence.DecisionToolApproval, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
	}}
	manager := &routerTestManager{dispatchHandled: true, invokeHandler: true}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

	err := router.RespondToolApproval(context.Background(), ToolApprovalResponseInput{
		BotID: decisionBotID, ApprovalID: decisionTargetID, Decision: "approve", ChatToken: "secret",
	}, nil)
	if err != nil {
		t.Fatalf("RespondToolApproval() error = %v", err)
	}
	if manager.starts != 0 || resolver.allocated != 0 {
		t.Fatalf("active response started fallback: starts=%d allocated=%d", manager.starts, resolver.allocated)
	}
	if len(resolver.respondApproval) != 1 {
		t.Fatalf("owner response calls = %d, want 1", len(resolver.respondApproval))
	}
	got := resolver.respondApproval[0]
	if !got.ResolveOnly || got.BotID != decisionBotID || got.ThreadID != decisionSessionID || got.ExplicitID != decisionTargetID {
		t.Fatalf("owner response input = %#v", got)
	}
	if got.ChatToken != "" {
		t.Fatal("routed command retained transport credential")
	}
}

func TestDecisionRouterMemoryFallbackUsesRuntimeLifecycleWithoutFence(t *testing.T) {
	resolver := &routerTestResolver{prepared: runtimefence.PreservedDecision{
		Kind: runtimefence.DecisionUserInput, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
	}}
	manager := &routerTestManager{}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

	if err := router.RespondUserInputWithOptions(context.Background(), UserInputResponseInput{UserInputID: decisionTargetID, TextAnswer: "yes"}, nil, DecisionRespondOptions{
		OnDecisionCommitted: func() { resolver.lifecycle = append(resolver.lifecycle, "ack") },
	}); err != nil {
		t.Fatalf("RespondUserInput() error = %v", err)
	}
	if len(resolver.respondInput) != 1 || resolver.respondInput[0].ThreadID != decisionSessionID {
		t.Fatalf("local response inputs = %#v", resolver.respondInput)
	}
	if resolver.respondFence.Valid() || resolver.allocated != 0 || manager.starts != 1 {
		t.Fatalf("memory continuation lifecycle = fence:%#v allocated:%d starts:%d", resolver.respondFence, resolver.allocated, manager.starts)
	}
	if len(manager.admissions) != 1 || manager.admissions[0].ResolvedDecision == nil || manager.admissions[0].ResolvedDecision.Status != "submitted" {
		t.Fatalf("memory continuation admission = %#v", manager.admissions)
	}
	if got := strings.Join(resolver.lifecycle, ","); got != "commit,ack,continue" {
		t.Fatalf("memory decision lifecycle = %q, want commit,ack,continue", got)
	}
}

func TestDecisionRouterDoesNotCreateContinuationForNonDurableDecision(t *testing.T) {
	// Distributed runtime without a local waiter: the waiter may be alive on
	// another server, so the response must fail closed without touching the
	// durable decision.
	for _, mode := range []decision.ContinuationMode{
		decision.ContinuationModeLocalWaiter,
		decision.ContinuationModeUnknown,
	} {
		t.Run(string(mode), func(t *testing.T) {
			resolver := &routerTestResolver{
				prepared: runtimefence.PreservedDecision{
					Kind: runtimefence.DecisionToolApproval, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
				},
				runtimeMode: mode,
			}
			manager := &routerTestManager{distributed: true}
			router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

			err := router.RespondToolApproval(context.Background(), ToolApprovalResponseInput{
				ApprovalID: decisionTargetID, Decision: "approve",
			}, nil)
			if !errors.Is(err, ErrRuntimeDecisionOwnerUnavailable) {
				t.Fatalf("RespondToolApproval() error = %v, want owner unavailable", err)
			}
			if manager.starts != 0 || resolver.allocated != 0 || len(resolver.lifecycle) != 0 {
				t.Fatalf("non-durable decision started work: starts:%d allocated:%d lifecycle:%v", manager.starts, resolver.allocated, resolver.lifecycle)
			}
		})
	}
}

func TestDecisionRouterCommitsLocalWaiterDecisionWithLocalWaiter(t *testing.T) {
	// A local waiter blocked in this process accepts the response through the
	// commit layer directly: no runtime run, no fence, no continuation. This
	// is the only path that reaches channel-originated ACP/MCP waiters, which
	// never register runtime runs.
	for _, distributed := range []bool{false, true} {
		t.Run(fmt.Sprintf("distributed=%t", distributed), func(t *testing.T) {
			resolver := &routerTestResolver{
				prepared: runtimefence.PreservedDecision{
					Kind: runtimefence.DecisionToolApproval, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
				},
				runtimeMode:   decision.ContinuationModeLocalWaiter,
				runtimeWaiter: true,
			}
			manager := &routerTestManager{distributed: distributed}
			router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

			if err := router.RespondToolApproval(context.Background(), ToolApprovalResponseInput{
				ApprovalID: decisionTargetID, Decision: "approve",
			}, nil); err != nil {
				t.Fatalf("RespondToolApproval() error = %v", err)
			}
			if got := strings.Join(resolver.lifecycle, ","); got != "commit,continue" {
				t.Fatalf("local waiter lifecycle = %q, want commit,continue", got)
			}
			if manager.starts != 0 || resolver.allocated != 0 {
				t.Fatalf("local waiter response started continuation: starts=%d allocated=%d", manager.starts, resolver.allocated)
			}
		})
	}
}

func TestDecisionRouterDelegatesNonDurableDecisionToCommitInProcess(t *testing.T) {
	// An in-process runtime has exactly one candidate process: no local waiter
	// means the waiter is gone for good, so the response is delegated to the
	// commit layer, which fails closed for unknown modes and auto-closes
	// unfenced orphaned local_waiter decisions.
	for _, mode := range []decision.ContinuationMode{
		decision.ContinuationModeLocalWaiter,
		decision.ContinuationModeUnknown,
	} {
		t.Run(string(mode), func(t *testing.T) {
			resolver := &routerTestResolver{
				prepared: runtimefence.PreservedDecision{
					Kind: runtimefence.DecisionUserInput, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
				},
				runtimeMode: mode,
			}
			manager := &routerTestManager{}
			router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

			if err := router.RespondUserInput(context.Background(), UserInputResponseInput{
				UserInputID: decisionTargetID, TextAnswer: "yes",
			}, nil); err != nil {
				t.Fatalf("RespondUserInput() error = %v", err)
			}
			if got := strings.Join(resolver.lifecycle, ","); got != "commit,continue" {
				t.Fatalf("in-process delegation lifecycle = %q, want commit,continue", got)
			}
			if manager.starts != 0 || resolver.allocated != 0 {
				t.Fatalf("in-process delegation started continuation: starts=%d allocated=%d", manager.starts, resolver.allocated)
			}
		})
	}
}

func TestDecisionRouterCommitFailureDoesNotPublishOrContinue(t *testing.T) {
	wantErr := errors.New("durable decision commit failed")
	resolver := &routerTestResolver{
		prepared: runtimefence.PreservedDecision{
			Kind: runtimefence.DecisionToolApproval, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
		},
		commitErr: wantErr,
	}
	manager := &routerTestManager{}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

	committedCalls := 0
	err := router.RespondToolApprovalWithOptions(context.Background(), ToolApprovalResponseInput{
		ApprovalID: decisionTargetID,
		Decision:   "approve",
	}, nil, DecisionRespondOptions{OnDecisionCommitted: func() { committedCalls++ }})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RespondToolApproval() error = %v, want %v", err, wantErr)
	}
	if got := strings.Join(resolver.lifecycle, ","); got != "commit" {
		t.Fatalf("failed decision lifecycle = %q, want commit", got)
	}
	if len(manager.admissions) != 0 || len(manager.handledEvents) != 0 || len(manager.finalizedEvents) != 0 {
		t.Fatalf("failed decision was projected: admissions=%#v handled=%#v finalized=%#v", manager.admissions, manager.handledEvents, manager.finalizedEvents)
	}
	if committedCalls != 0 {
		t.Fatalf("failed commit acknowledgements = %d, want 0", committedCalls)
	}
}

func TestPreparedDecisionReconcilesAmbiguousCommitAfterRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	commitErr := errors.New("commit result was ambiguous")
	continueCalls := 0
	prepared := &PreparedDecision{
		commit: func(context.Context) error {
			cancel()
			return commitErr
		},
		reconcile: func(reconcileCtx context.Context) (bool, error) {
			if err := reconcileCtx.Err(); err != nil {
				return true, fmt.Errorf("reconcile context was canceled: %w", err)
			}
			return true, nil
		},
		continueRun: func(context.Context, chan<- StreamEventPayload) error {
			continueCalls++
			return nil
		},
	}
	committedCalls := 0

	err := prepared.commitAndContinue(ctx, nil, func() { committedCalls++ })
	if err != nil {
		t.Fatalf("commitAndContinue() error = %v", err)
	}
	if committedCalls != 1 || continueCalls != 0 {
		t.Fatalf("calls = committed:%d continue:%d, want 1/0", committedCalls, continueCalls)
	}
}

func TestDecisionRouterDistributedFallbackClaimsFenceAndProjectsTerminal(t *testing.T) {
	resolver := &routerTestResolver{
		prepared: runtimefence.PreservedDecision{
			Kind: runtimefence.DecisionToolApproval, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
		},
		respondEvents: []agentpkg.StreamEvent{
			{Type: agentpkg.EventAgentStart},
			{Type: agentpkg.EventTextDelta, Delta: "continued"},
			{Type: agentpkg.EventAgentEnd, HistoryCommitted: true},
		},
	}
	manager := &routerTestManager{distributed: true}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)
	output := make(chan StreamEventPayload, len(resolver.respondEvents))

	if err := router.RespondToolApproval(context.Background(), ToolApprovalResponseInput{ApprovalID: decisionTargetID, Decision: "approve"}, output); err != nil {
		t.Fatalf("RespondToolApproval() error = %v", err)
	}
	if manager.starts != 1 || resolver.allocated != 1 || len(resolver.activated) != 1 {
		t.Fatalf("distributed admission = starts:%d allocated:%d activated:%d", manager.starts, resolver.allocated, len(resolver.activated))
	}
	if preserved := resolver.activated[0].PreserveDecision; preserved == nil || *preserved != resolver.prepared {
		t.Fatalf("preserved decision = %#v", preserved)
	}
	if resolver.respondFence.Token != 7 {
		t.Fatalf("continuation fence = %#v", resolver.respondFence)
	}
	if len(manager.handledEvents) != 2 || len(manager.finalizedEvents) != 1 || !manager.canonicalReady[0] {
		t.Fatalf("runtime projection = handled:%#v finalized:%#v canonical:%#v", manager.handledEvents, manager.finalizedEvents, manager.canonicalReady)
	}
	if manager.finishes != 1 || len(output) != len(resolver.respondEvents) {
		t.Fatalf("completion = finishes:%d output:%d", manager.finishes, len(output))
	}
}

func TestDecisionRouterUsesTransportSuppliedStreamIdentity(t *testing.T) {
	// The WS protocol lets the client choose the stream ID so its abort
	// frames and event envelopes correlate with the continuation run; other
	// transports leave it empty and get a server-generated identity.
	resolver := &routerTestResolver{prepared: runtimefence.PreservedDecision{
		Kind: runtimefence.DecisionToolApproval, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
	}}
	manager := &routerTestManager{distributed: true}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

	if err := router.RespondToolApprovalWithOptions(context.Background(), ToolApprovalResponseInput{
		ApprovalID: decisionTargetID, Decision: "approve",
	}, nil, DecisionRespondOptions{StreamID: " client-stream-7 "}); err != nil {
		t.Fatalf("RespondToolApprovalWithOptions() error = %v", err)
	}
	if len(manager.startStreamIDs) != 1 || manager.startStreamIDs[0] != "client-stream-7" {
		t.Fatalf("continuation stream ids = %#v, want [client-stream-7]", manager.startStreamIDs)
	}
	if len(manager.startContinuations) != 1 || !manager.startContinuations[0] {
		t.Fatalf("continuation flag = %#v, want [true]", manager.startContinuations)
	}

	generated := &routerTestManager{distributed: true}
	generatedRouter := newDecisionRouter(slog.New(slog.DiscardHandler), generated, &routerTestResolver{prepared: resolver.prepared})
	if err := generatedRouter.RespondToolApproval(context.Background(), ToolApprovalResponseInput{
		ApprovalID: decisionTargetID, Decision: "approve",
	}, nil); err != nil {
		t.Fatalf("RespondToolApproval() error = %v", err)
	}
	if len(generated.startStreamIDs) != 1 || !strings.HasPrefix(generated.startStreamIDs[0], "decision-") {
		t.Fatalf("generated stream ids = %#v, want decision- prefix", generated.startStreamIDs)
	}
}

func TestDecisionRouterLeavesPendingTerminalForManagerRetry(t *testing.T) {
	resolver := &routerTestResolver{
		prepared: runtimefence.PreservedDecision{
			Kind: runtimefence.DecisionUserInput, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
		},
		respondEvents: []agentpkg.StreamEvent{{Type: agentpkg.EventAgentEnd, HistoryCommitted: true}},
	}
	manager := &routerTestManager{distributed: true, finalizeErr: sessionruntime.ErrTerminalCommitPending}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

	err := router.RespondUserInput(context.Background(), UserInputResponseInput{UserInputID: decisionTargetID, TextAnswer: "yes"}, nil)
	if !errors.Is(err, sessionruntime.ErrTerminalCommitPending) {
		t.Fatalf("terminal error = %v, want ErrTerminalCommitPending", err)
	}
	if manager.finishes != 0 {
		t.Fatalf("pending terminal was overwritten by %d FinishRun call(s)", manager.finishes)
	}
}

func TestDecisionRouterPropagatesActiveCommandResult(t *testing.T) {
	wantErr := errors.New("decision conflict")
	resolver := &routerTestResolver{prepared: runtimefence.PreservedDecision{
		Kind: runtimefence.DecisionToolApproval, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
	}}
	manager := &routerTestManager{dispatchHandled: true, dispatchErr: wantErr}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

	err := router.RespondToolApproval(context.Background(), ToolApprovalResponseInput{ApprovalID: decisionTargetID, Decision: "approve"}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("RespondToolApproval() error = %v, want %v", err, wantErr)
	}
}

func TestDecisionRouterReconcilesCommittedDuplicateBeforeStartingContinuation(t *testing.T) {
	resolver := &routerTestResolver{
		prepared: runtimefence.PreservedDecision{
			Kind: runtimefence.DecisionToolApproval, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
		},
		reconcileHandled: true,
	}
	manager := &routerTestManager{distributed: true}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

	committedCalls := 0
	if err := router.RespondToolApprovalWithOptions(context.Background(), ToolApprovalResponseInput{ApprovalID: decisionTargetID, Decision: "approve"}, nil, DecisionRespondOptions{
		OnDecisionCommitted: func() { committedCalls++ },
	}); err != nil {
		t.Fatalf("duplicate response error = %v", err)
	}
	if resolver.reconcileCalls != 1 || resolver.allocated != 0 || manager.starts != 0 || len(resolver.respondApproval) != 0 {
		t.Fatalf("duplicate routing = reconcile:%d allocated:%d starts:%d responses:%d", resolver.reconcileCalls, resolver.allocated, manager.starts, len(resolver.respondApproval))
	}
	if committedCalls != 1 {
		t.Fatalf("duplicate commit acknowledgements = %d, want 1", committedCalls)
	}
}

func TestDecisionRouterReturnsCommittedPayloadConflict(t *testing.T) {
	resolver := &routerTestResolver{
		prepared: runtimefence.PreservedDecision{
			Kind: runtimefence.DecisionUserInput, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
		},
		reconcileHandled: true,
		reconcileErr:     userinput.ErrAlreadyDecided,
	}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), &routerTestManager{distributed: true}, resolver)

	err := router.RespondUserInput(context.Background(), UserInputResponseInput{UserInputID: decisionTargetID, TextAnswer: "different"}, nil)
	if !errors.Is(err, sessionruntime.ErrCommandPayloadConflict) {
		t.Fatalf("conflicting duplicate error = %v, want payload conflict", err)
	}
}

func TestDecisionRouterRoutesDecisionAcrossRedisOwnersOptional(t *testing.T) {
	redisURL := strings.TrimSpace(os.Getenv("MEMOH_TEST_REDIS_URL"))
	if redisURL == "" {
		redisURL = strings.TrimSpace(os.Getenv("MEMOH_TEST_VALKEY_URL"))
	}
	if redisURL == "" {
		t.Skip("set MEMOH_TEST_REDIS_URL or MEMOH_TEST_VALKEY_URL to run cross-owner decision routing")
	}
	prefix := fmt.Sprintf("memoh:test:decision_runtime:%d:", time.Now().UnixNano())
	newBackend := func() *sessionruntime.RedisBackend {
		backend, err := sessionruntime.NewRedisBackend(context.Background(), sessionruntime.RedisOptions{
			URL: redisURL, KeyPrefix: prefix, StateTTL: time.Minute,
		})
		if err != nil {
			t.Fatalf("new Redis backend: %v", err)
		}
		return backend
	}
	newManager := func(ownerID string) *sessionruntime.Manager {
		manager := sessionruntime.NewManager(newBackend(), sessionruntime.Options{
			OwnerID: ownerID, StateTTL: time.Minute, OwnerLeaseTTL: 5 * time.Second, CommandAckTTL: 2 * time.Second,
		})
		if err := manager.Start(context.Background()); err != nil {
			t.Fatalf("start manager %s: %v", ownerID, err)
		}
		return manager
	}
	owner := newManager("decision-owner")
	remote := newManager("decision-remote")
	t.Cleanup(func() {
		if err := remote.Close(); err != nil {
			t.Errorf("close remote manager: %v", err)
		}
		if err := owner.Close(); err != nil {
			t.Errorf("close owner manager: %v", err)
		}
	})
	prepared := runtimefence.PreservedDecision{
		Kind: runtimefence.DecisionToolApproval, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
	}
	ownerResolver := &routerTestResolver{prepared: prepared}
	remoteResolver := &routerTestResolver{prepared: prepared}
	newDecisionRouter(slog.New(slog.DiscardHandler), owner, ownerResolver)
	remoteRouter := newDecisionRouter(slog.New(slog.DiscardHandler), remote, remoteResolver)

	handle, err := owner.StartRunWithOptions(context.Background(), sessionruntime.RunStartOptions{
		BotID: decisionBotID, SessionID: decisionSessionID, StreamID: "active-decision-run",
	})
	if err != nil {
		t.Fatalf("start active decision run: %v", err)
	}
	if _, err := owner.HandleAgentEvent(context.Background(), handle, agentpkg.StreamEvent{
		Type: agentpkg.EventToolApprovalRequest, ApprovalID: decisionTargetID, ToolCallID: "call-1", ToolName: "exec", Status: "pending",
	}); err != nil {
		t.Fatalf("project active decision: %v", err)
	}

	if err := remoteRouter.RespondToolApproval(context.Background(), ToolApprovalResponseInput{
		BotID: decisionBotID, ApprovalID: decisionTargetID, Decision: "approve",
	}, nil); err != nil {
		t.Fatalf("route remote approval: %v", err)
	}
	if len(ownerResolver.respondApproval) != 1 || !ownerResolver.respondApproval[0].ResolveOnly {
		t.Fatalf("owner responses = %#v", ownerResolver.respondApproval)
	}
	if len(remoteResolver.respondApproval) != 0 {
		t.Fatalf("remote server executed owner response: %#v", remoteResolver.respondApproval)
	}
}

func TestDecisionRouterRunsFencedContinuationWithRedisOptional(t *testing.T) {
	redisURL := strings.TrimSpace(os.Getenv("MEMOH_TEST_REDIS_URL"))
	if redisURL == "" {
		redisURL = strings.TrimSpace(os.Getenv("MEMOH_TEST_VALKEY_URL"))
	}
	if redisURL == "" {
		t.Skip("set MEMOH_TEST_REDIS_URL or MEMOH_TEST_VALKEY_URL to run fenced decision continuation")
	}
	backend, err := sessionruntime.NewRedisBackend(context.Background(), sessionruntime.RedisOptions{
		URL: redisURL, KeyPrefix: fmt.Sprintf("memoh:test:decision_continuation:%d:", time.Now().UnixNano()), StateTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("new Redis backend: %v", err)
	}
	manager := sessionruntime.NewManager(backend, sessionruntime.Options{
		OwnerID: "decision-continuation-owner", StateTTL: time.Minute, OwnerLeaseTTL: 5 * time.Second, CommandAckTTL: 2 * time.Second,
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Errorf("close manager: %v", err)
		}
	})
	resolver := &routerTestResolver{
		prepared: runtimefence.PreservedDecision{
			Kind: runtimefence.DecisionUserInput, ID: decisionTargetID, BotID: decisionBotID, SessionID: decisionSessionID,
		},
		respondEvents: []agentpkg.StreamEvent{
			{Type: agentpkg.EventAgentStart},
			{Type: agentpkg.EventTextDelta, Delta: "continued"},
			{Type: agentpkg.EventAgentEnd, HistoryCommitted: true},
		},
	}
	router := newDecisionRouter(slog.New(slog.DiscardHandler), manager, resolver)

	if err := router.RespondUserInput(context.Background(), UserInputResponseInput{
		BotID: decisionBotID, UserInputID: decisionTargetID, TextAnswer: "continue",
	}, nil); err != nil {
		t.Fatalf("run fenced continuation: %v", err)
	}
	if resolver.allocated != 1 || len(resolver.activated) != 1 || resolver.respondFence.Token != 7 {
		t.Fatalf("fenced continuation = allocated:%d activated:%d fence:%#v", resolver.allocated, len(resolver.activated), resolver.respondFence)
	}
	snapshot, err := manager.Snapshot(context.Background(), decisionBotID, decisionSessionID)
	if err != nil {
		t.Fatalf("load continuation snapshot: %v", err)
	}
	if snapshot.CurrentRunView == nil || snapshot.CurrentRunView.Status != sessionruntime.RunStatusCompleted || !snapshot.CurrentRunView.HistoryCommitted || !snapshot.CurrentRunView.CanonicalReady {
		t.Fatalf("continuation snapshot = %#v", snapshot.CurrentRunView)
	}
}
