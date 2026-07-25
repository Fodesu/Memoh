package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/memohai/twilight-ai/sdk"

	contextlimit "github.com/memohai/memoh/internal/agent/context/limit"
	"github.com/memohai/memoh/internal/agent/decision"
	toolapproval "github.com/memohai/memoh/internal/agent/decision/approval"
	"github.com/memohai/memoh/internal/agent/runtime/native"
	"github.com/memohai/memoh/internal/bots"
	sessionpkg "github.com/memohai/memoh/internal/chat/thread"
	"github.com/memohai/memoh/internal/models"
	"github.com/memohai/memoh/internal/runtimefence"
	"github.com/memohai/memoh/internal/workspace"
)

type ToolApprovalResponseInput struct {
	BotID                      string
	ThreadID                   string
	ActorChannelIdentityID     string
	ActorUserID                string
	ApprovalID                 string
	ExplicitID                 string
	ReplyExternalMessageID     string
	Decision                   string
	Reason                     string
	ChatToken                  string
	SuppressActivePromptAttach bool
	ResolveOnly                bool
}

// CommittedToolApprovalResponse contains a durable approval decision and the
// owner-local state needed to continue its parent turn.
type CommittedToolApprovalResponse struct {
	request      toolapproval.Request
	input        ToolApprovalResponseInput
	resumePolicy decision.ResumePolicy
	activePrompt *acpActivePromptSubscription
}

// CommitToolApprovalResponse durably resolves an approval without executing
// the tool or starting the agent continuation.
func (s *Service) CommitToolApprovalResponse(ctx context.Context, input ToolApprovalResponseInput) (CommittedToolApprovalResponse, error) {
	if s.toolApproval == nil {
		return CommittedToolApprovalResponse{}, errors.New("tool approval service not configured")
	}
	target, err := s.toolApproval.ResolveTarget(ctx, toolapproval.ResolveInput{
		BotID:                  input.BotID,
		SessionID:              input.ThreadID,
		ExplicitID:             firstNonEmpty(input.ExplicitID, input.ApprovalID),
		ReplyExternalMessageID: input.ReplyExternalMessageID,
	})
	if err != nil {
		return CommittedToolApprovalResponse{}, err
	}
	if err := runtimefence.ValidateScope(ctx, target.BotID, target.SessionID); err != nil {
		return CommittedToolApprovalResponse{}, err
	}
	isACP, err := s.authorizeToolApprovalResponse(ctx, target, input)
	if err != nil {
		return CommittedToolApprovalResponse{}, err
	}
	hasLocalWaiter := s.toolApproval.CanRespond(target)
	resumePolicy := toolApprovalResumePolicy(target, isACP, hasLocalWaiter)
	if resumePolicy == decision.ResumePolicyUnknown {
		return CommittedToolApprovalResponse{}, ErrRuntimeDecisionOwnerUnavailable
	}
	if resumePolicy == decision.ResumePolicyLiveWaiter && !hasLocalWaiter {
		if target.RuntimeFenced {
			return CommittedToolApprovalResponse{}, ErrRuntimeDecisionOwnerUnavailable
		}
		rejected, rejectErr := s.toolApproval.Reject(ctx, target.ID, "", "tool approval expired: the requesting tool call is no longer waiting")
		if rejectErr != nil && !errors.Is(rejectErr, toolapproval.ErrAlreadyDecided) {
			return CommittedToolApprovalResponse{}, rejectErr
		}
		if rejectErr == nil {
			target = rejected
		}
		return CommittedToolApprovalResponse{request: target, input: input, resumePolicy: resumePolicy}, nil
	}

	var activePrompt *acpActivePromptSubscription
	if isACP && !input.SuppressActivePromptAttach {
		activePrompt, _ = s.subscribeACPActivePrompt(
			firstNonEmpty(target.BotID, input.BotID),
			firstNonEmpty(target.SessionID, input.ThreadID),
		)
	}
	switch strings.ToLower(strings.TrimSpace(input.Decision)) {
	case "approve", "approved":
		target, err = s.toolApproval.Approve(ctx, target.ID, input.ActorChannelIdentityID, input.Reason)
	case "reject", "rejected":
		target, err = s.toolApproval.Reject(ctx, target.ID, input.ActorChannelIdentityID, input.Reason)
	default:
		err = fmt.Errorf("unknown tool approval decision %q", input.Decision)
	}
	if err != nil {
		if activePrompt != nil {
			activePrompt.release()
		}
		return CommittedToolApprovalResponse{}, err
	}
	return CommittedToolApprovalResponse{request: target, input: input, resumePolicy: resumePolicy, activePrompt: activePrompt}, nil
}

// ContinueCommittedToolApprovalResponse executes the approved tool, when
// applicable, and resumes the parent turn without updating the decision again.
func (s *Service) ContinueCommittedToolApprovalResponse(ctx context.Context, committed CommittedToolApprovalResponse, eventCh chan<- WSStreamEvent) error {
	target := committed.request
	if strings.TrimSpace(target.ID) == "" {
		return errors.New("committed tool approval response is missing its request")
	}
	switch committed.resumePolicy {
	case decision.ResumePolicyLiveWaiter:
		if committed.activePrompt != nil {
			return forwardACPActivePrompt(ctx, committed.activePrompt, eventCh, acpActivePromptForwardOptions{
				SkipToolCallID: target.ToolCallID,
				SkipApprovalID: target.ID,
			})
		}
		return emitApprovalAck(ctx, eventCh)
	case decision.ResumePolicyUnknown:
		return ErrRuntimeDecisionOwnerUnavailable
	}
	ctx = workspace.WithWorkspaceTarget(ctx, target.WorkspaceTargetID)
	var toolResult sdk.ToolResultPart
	var err error
	switch target.Status {
	case toolapproval.StatusApproved:
		toolResult, err = s.executeApprovedTool(ctx, target, committed.input)
	case toolapproval.StatusRejected:
		toolResult = sdk.ToolResultPart{
			ToolCallID: target.ToolCallID,
			ToolName:   target.ToolName,
			Result:     s.limitToolResultText(rejectedToolResultText(committed.input.Reason), target.ToolName),
			IsError:    true,
		}
	default:
		return fmt.Errorf("committed tool approval has unexpected status %q", target.Status)
	}
	if err != nil {
		return err
	}
	return s.storeToolResultAndContinue(ctx, target, committed.input, toolResult, eventCh)
}

func (s *Service) PrepareToolApprovalResponse(ctx context.Context, input ToolApprovalResponseInput) (runtimefence.PreservedDecision, error) {
	target, err := s.prepareToolApprovalResponseTarget(ctx, input, false)
	return target.Decision, err
}

// PrepareToolApprovalResponseTarget validates a response and returns its
// canonical scope even when the decision was already committed. Runtime
// routers use that scope to replay or reconcile idempotent commands.
func (s *Service) PrepareToolApprovalResponseTarget(ctx context.Context, input ToolApprovalResponseInput) (runtimefence.PreservedDecision, error) {
	target, err := s.prepareToolApprovalResponseTarget(ctx, input, true)
	return target.Decision, err
}

func (s *Service) PrepareToolApprovalRuntimeTarget(ctx context.Context, input ToolApprovalResponseInput) (RuntimeDecisionTarget, error) {
	return s.prepareToolApprovalResponseTarget(ctx, input, true)
}

func (s *Service) prepareToolApprovalResponseTarget(ctx context.Context, input ToolApprovalResponseInput, includeDecided bool) (RuntimeDecisionTarget, error) {
	if s.toolApproval == nil {
		return RuntimeDecisionTarget{}, errors.New("tool approval service not configured")
	}
	target, err := s.toolApproval.ResolveTarget(ctx, toolapproval.ResolveInput{
		BotID:                  input.BotID,
		SessionID:              input.ThreadID,
		ExplicitID:             firstNonEmpty(input.ExplicitID, input.ApprovalID),
		ReplyExternalMessageID: input.ReplyExternalMessageID,
	})
	if includeDecided && errors.Is(err, toolapproval.ErrNotFound) {
		explicitID := firstNonEmpty(input.ExplicitID, input.ApprovalID)
		if explicitID != "" {
			if existing, getErr := s.toolApproval.Get(ctx, explicitID); getErr == nil {
				if (strings.TrimSpace(input.BotID) == "" || strings.TrimSpace(input.BotID) == existing.BotID) &&
					(strings.TrimSpace(input.ThreadID) == "" || strings.TrimSpace(input.ThreadID) == existing.SessionID) {
					target = existing
					err = nil
				}
			}
		}
	}
	if err != nil {
		return RuntimeDecisionTarget{}, err
	}
	if err := runtimefence.ValidateScope(ctx, target.BotID, target.SessionID); err != nil {
		return RuntimeDecisionTarget{}, err
	}
	isACP, err := s.authorizeToolApprovalResponse(ctx, target, input)
	if err != nil {
		return RuntimeDecisionTarget{}, err
	}
	switch strings.ToLower(strings.TrimSpace(input.Decision)) {
	case "approve", "approved", "reject", "rejected":
	default:
		return RuntimeDecisionTarget{}, fmt.Errorf("unknown tool approval decision %q", input.Decision)
	}
	hasLocalWaiter := s.toolApproval.CanRespond(target)
	return RuntimeDecisionTarget{
		Decision: runtimefence.PreservedDecision{
			Kind: runtimefence.DecisionToolApproval, ID: target.ID, BotID: target.BotID, SessionID: target.SessionID,
		},
		ResumePolicy:   toolApprovalResumePolicy(target, isACP, hasLocalWaiter),
		HasLocalWaiter: hasLocalWaiter,
	}, nil
}

func toolApprovalResumePolicy(target toolapproval.Request, isACP, hasLocalWaiter bool) decision.ResumePolicy {
	if hasLocalWaiter {
		return decision.ResumePolicyLiveWaiter
	}
	policy := decision.NormalizeResumePolicy(target.ResumePolicy)
	if policy == decision.ResumePolicyUnknown {
		return policy
	}
	if isACP || policy == decision.ResumePolicyLiveWaiter {
		return decision.ResumePolicyLiveWaiter
	}
	return policy
}

func (s *Service) respondToolApproval(ctx context.Context, input ToolApprovalResponseInput, eventCh chan<- WSStreamEvent) error {
	if s.toolApproval == nil {
		return errors.New("tool approval service not configured")
	}
	target, err := s.toolApproval.ResolveTarget(ctx, toolapproval.ResolveInput{
		BotID:                  input.BotID,
		SessionID:              input.ThreadID,
		ExplicitID:             firstNonEmpty(input.ExplicitID, input.ApprovalID),
		ReplyExternalMessageID: input.ReplyExternalMessageID,
	})
	if err != nil {
		if input.ResolveOnly && errors.Is(err, toolapproval.ErrNotFound) {
			if handled, replayErr := s.reconcileToolApprovalReplay(ctx, input); handled {
				return replayErr
			}
		}
		return err
	}
	if err := runtimefence.ValidateScope(ctx, target.BotID, target.SessionID); err != nil {
		return err
	}
	isACP, err := s.authorizeToolApprovalResponse(ctx, target, input)
	if err != nil {
		return err
	}
	if input.ResolveOnly {
		return s.resolveToolApprovalDecision(ctx, target, input, eventCh)
	}
	if isACP {
		return s.respondACPToolApproval(ctx, target, input, eventCh)
	}
	ctx = workspace.WithWorkspaceTarget(ctx, target.WorkspaceTargetID)
	if s.toolApproval.CanRespond(target) {
		return s.respondLiveToolApproval(ctx, target, input, eventCh)
	}

	var toolResult sdk.ToolResultPart
	switch strings.ToLower(strings.TrimSpace(input.Decision)) {
	case "approve", "approved":
		approved, err := s.toolApproval.Approve(ctx, target.ID, input.ActorChannelIdentityID, input.Reason)
		if err != nil {
			return err
		}
		toolResult, err = s.executeApprovedTool(ctx, approved, input)
		if err != nil {
			return err
		}
	case "reject", "rejected":
		rejected, err := s.toolApproval.Reject(ctx, target.ID, input.ActorChannelIdentityID, input.Reason)
		if err != nil {
			return err
		}
		toolResult = sdk.ToolResultPart{
			ToolCallID: rejected.ToolCallID,
			ToolName:   rejected.ToolName,
			Result:     s.limitToolResultText(rejectedToolResultText(input.Reason), rejected.ToolName),
			IsError:    true,
		}
	default:
		return fmt.Errorf("unknown tool approval decision %q", input.Decision)
	}

	return s.storeToolResultAndContinue(ctx, target, input, toolResult, eventCh)
}

func (s *Service) reconcileToolApprovalReplay(ctx context.Context, input ToolApprovalResponseInput) (bool, error) {
	targetID := firstNonEmpty(input.ExplicitID, input.ApprovalID)
	target, err := s.toolApproval.Get(ctx, targetID)
	if err != nil {
		if errors.Is(err, toolapproval.ErrNotFound) {
			return false, nil
		}
		return true, err
	}
	if strings.TrimSpace(target.BotID) != strings.TrimSpace(input.BotID) || strings.TrimSpace(target.SessionID) != strings.TrimSpace(input.ThreadID) {
		return false, nil
	}
	if target.Status == toolapproval.StatusPending {
		return false, nil
	}
	if err := runtimefence.ValidateScope(ctx, target.BotID, target.SessionID); err != nil {
		return true, err
	}
	if _, err := s.authorizeToolApprovalResponse(ctx, target, input); err != nil {
		return true, err
	}
	wantStatus := ""
	switch strings.ToLower(strings.TrimSpace(input.Decision)) {
	case "approve", "approved":
		wantStatus = toolapproval.StatusApproved
	case "reject", "rejected":
		wantStatus = toolapproval.StatusRejected
	default:
		return true, fmt.Errorf("unknown tool approval decision %q", input.Decision)
	}
	if target.Status != wantStatus || strings.TrimSpace(target.DecisionReason) != strings.TrimSpace(input.Reason) {
		return true, toolapproval.ErrAlreadyDecided
	}
	return true, emitApprovalAck(ctx, nil)
}

// ReconcileToolApprovalResponse checks whether a ResolveOnly response was
// already committed. It is read-only and does not require local run control.
func (s *Service) ReconcileToolApprovalResponse(ctx context.Context, input ToolApprovalResponseInput) (bool, error) {
	if s == nil || s.toolApproval == nil {
		return false, errors.New("tool approval service not configured")
	}
	return s.reconcileToolApprovalReplay(ctx, input)
}

// A live waiter owns tool execution and continuation inside the active run.
// The response path only resolves the decision so the waiter can resume.
func (s *Service) respondLiveToolApproval(ctx context.Context, target toolapproval.Request, input ToolApprovalResponseInput, eventCh chan<- WSStreamEvent) error {
	return s.resolveToolApprovalDecision(ctx, target, input, eventCh)
}

func (s *Service) resolveToolApprovalDecision(ctx context.Context, target toolapproval.Request, input ToolApprovalResponseInput, eventCh chan<- WSStreamEvent) error {
	switch strings.ToLower(strings.TrimSpace(input.Decision)) {
	case "approve", "approved":
		if _, err := s.toolApproval.Approve(ctx, target.ID, input.ActorChannelIdentityID, input.Reason); err != nil {
			return err
		}
	case "reject", "rejected":
		if _, err := s.toolApproval.Reject(ctx, target.ID, input.ActorChannelIdentityID, input.Reason); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown tool approval decision %q", input.Decision)
	}
	return emitApprovalAck(ctx, eventCh)
}

func (s *Service) toolOutputLimit() contextlimit.ToolOutputLimit {
	limit := native.DefaultLimits().ToolOutputLimit()
	if s != nil && s.agent != nil {
		limit = s.agent.Limits().ToolOutputLimit()
	}
	return limit
}

func (s *Service) limitToolResultText(text, toolName string) string {
	limit := s.toolOutputLimit()
	return contextlimit.LimitString(text, "tool result ("+toolName+")", limit)
}

func (s *Service) limitToolResultValue(value any, toolName string) any {
	return contextlimit.LimitToolOutput(value, "tool result ("+toolName+")", s.toolOutputLimit())
}

func (s *Service) limitToolApprovalResult(result sdk.ToolApprovalResult, toolName string) sdk.ToolApprovalResult {
	if result.Decision == sdk.ToolApprovalDecisionRejected {
		result.Reason = s.limitToolResultText(result.Reason, toolName)
	}
	return result
}

func (s *Service) respondACPToolApproval(ctx context.Context, target toolapproval.Request, input ToolApprovalResponseInput, eventCh chan<- WSStreamEvent) error {
	if !s.toolApproval.CanRespond(target) {
		if target.RuntimeFenced {
			return ErrRuntimeDecisionOwnerUnavailable
		}
		_, err := s.toolApproval.Reject(ctx, target.ID, "", "tool approval expired: the requesting tool call is no longer waiting")
		if err != nil && !errors.Is(err, toolapproval.ErrAlreadyDecided) {
			return err
		}
		return emitApprovalAck(ctx, eventCh)
	}
	var activePrompt *acpActivePromptSubscription
	if eventCh != nil && !input.SuppressActivePromptAttach {
		activePrompt, _ = s.subscribeACPActivePrompt(
			firstNonEmpty(target.BotID, input.BotID),
			firstNonEmpty(target.SessionID, input.ThreadID),
		)
	}
	switch strings.ToLower(strings.TrimSpace(input.Decision)) {
	case "approve", "approved":
		if _, err := s.toolApproval.Approve(ctx, target.ID, input.ActorChannelIdentityID, input.Reason); err != nil {
			if activePrompt != nil {
				activePrompt.release()
			}
			return err
		}
	case "reject", "rejected":
		if _, err := s.toolApproval.Reject(ctx, target.ID, input.ActorChannelIdentityID, input.Reason); err != nil {
			if activePrompt != nil {
				activePrompt.release()
			}
			return err
		}
	default:
		if activePrompt != nil {
			activePrompt.release()
		}
		return fmt.Errorf("unknown tool approval decision %q", input.Decision)
	}
	if activePrompt != nil {
		return forwardACPActivePrompt(ctx, activePrompt, eventCh, acpActivePromptForwardOptions{
			SkipToolCallID: target.ToolCallID,
			SkipApprovalID: target.ID,
		})
	}
	return emitApprovalAck(ctx, eventCh)
}

func (s *Service) authorizeToolApprovalResponse(ctx context.Context, target toolapproval.Request, input ToolApprovalResponseInput) (bool, error) {
	if s == nil || s.sessionService == nil {
		return false, errors.New("session service not configured")
	}
	if s.botPermissions == nil {
		return false, errors.New("bot permission checker not configured")
	}
	sessionID := firstNonEmpty(target.SessionID, input.ThreadID)
	sess, err := s.sessionService.Get(ctx, sessionID)
	if err != nil {
		return false, err
	}
	botID := firstNonEmpty(target.BotID, input.BotID)
	if strings.TrimSpace(sess.BotID) == "" || strings.TrimSpace(botID) == "" || sess.BotID != botID {
		return false, toolapproval.ErrForbidden
	}
	if sessionpkg.IsACPRuntime(sess) {
		return true, s.authorizeACPToolApprovalResponse(ctx, target, input)
	}

	actorID := firstNonEmpty(input.ActorUserID, input.ActorChannelIdentityID)
	if actorID == "" {
		return false, toolapproval.ErrForbidden
	}
	if ok, err := s.botPermissions.HasBotPermission(ctx, botID, actorID, bots.PermissionManage); err != nil {
		return false, err
	} else if ok {
		return false, s.authorizeToolApprovalOperation(ctx, target, input)
	}
	if strings.TrimSpace(sess.CreatedByUserID) == "" || sess.CreatedByUserID != actorID {
		return false, toolapproval.ErrForbidden
	}
	required := toolApprovalSessionPermission(sess)
	if ok, err := s.botPermissions.HasBotPermission(ctx, botID, actorID, required); err != nil {
		return false, err
	} else if !ok {
		return false, toolapproval.ErrForbidden
	}
	return false, s.authorizeToolApprovalOperation(ctx, target, input)
}

func toolApprovalSessionPermission(sess sessionpkg.Thread) string {
	mode := strings.TrimSpace(sess.SessionMode)
	if !sessionpkg.IsKnownSessionMode(mode) {
		mode, _ = sessionpkg.DescriptorFromLegacyType(sess.Type)
	}
	switch mode {
	case sessionpkg.TypeChat, sessionpkg.TypeSubagent:
		return bots.PermissionChat
	default:
		return bots.PermissionManage
	}
}

func (s *Service) authorizeACPToolApprovalResponse(ctx context.Context, target toolapproval.Request, input ToolApprovalResponseInput) error {
	if s == nil || s.sessionService == nil {
		return errors.New("session service not configured")
	}
	if s.botPermissions == nil {
		return errors.New("bot permission checker not configured")
	}
	sessionID := firstNonEmpty(target.SessionID, input.ThreadID)
	sess, err := s.sessionService.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if !sessionpkg.IsACPRuntime(sess) {
		return s.authorizeToolApprovalOperation(ctx, target, input)
	}
	botID := firstNonEmpty(target.BotID, input.BotID)
	if strings.TrimSpace(sess.BotID) != "" && strings.TrimSpace(botID) != "" && sess.BotID != botID {
		return toolapproval.ErrForbidden
	}
	if strings.TrimSpace(botID) == "" {
		botID = sess.BotID
	}
	target.BotID = botID
	actorID := firstNonEmpty(input.ActorUserID, input.ActorChannelIdentityID)
	if actorID == "" {
		return toolapproval.ErrForbidden
	}
	acpMeta := mergeACPRuntimeMetadata(sess.Metadata, sess.RuntimeMetadata)
	runtimeOwnerID := metadataString(acpMeta, "runtime_owner_account_id")
	if runtimeOwnerID == "" || runtimeOwnerID != actorID {
		return toolapproval.ErrForbidden
	}
	return s.authorizeToolApprovalOperation(ctx, target, input)
}

func (s *Service) authorizeToolApprovalOperation(ctx context.Context, target toolapproval.Request, input ToolApprovalResponseInput) error {
	if s == nil || s.botPermissions == nil {
		return errors.New("bot permission checker not configured")
	}
	botID := firstNonEmpty(target.BotID, input.BotID)
	actorID := firstNonEmpty(input.ActorUserID, input.ActorChannelIdentityID)
	permission, ok := toolApprovalPermission(target.Operation)
	if strings.TrimSpace(botID) == "" || strings.TrimSpace(actorID) == "" || !ok {
		return toolapproval.ErrForbidden
	}
	if ok, err := s.botPermissions.HasBotPermission(ctx, botID, actorID, permission); err != nil {
		return err
	} else if !ok {
		return toolapproval.ErrForbidden
	}
	return nil
}

func toolApprovalPermission(operation string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case toolapproval.OperationRead:
		return bots.PermissionWorkspaceRead, true
	case toolapproval.OperationWrite:
		return bots.PermissionWorkspaceWrite, true
	case toolapproval.OperationExec:
		return bots.PermissionWorkspaceExec, true
	default:
		return "", false
	}
}

func emitApprovalAck(ctx context.Context, eventCh chan<- WSStreamEvent) error {
	if eventCh == nil {
		return nil
	}
	for _, event := range []native.StreamEvent{
		{Type: native.EventAgentStart},
		{Type: native.EventAgentEnd},
	} {
		if err := sendAgentStreamEvent(ctx, eventCh, event); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) executeApprovedTool(ctx context.Context, req toolapproval.Request, input ToolApprovalResponseInput) (sdk.ToolResultPart, error) {
	ctx = workspace.WithWorkspaceTarget(ctx, req.WorkspaceTargetID)
	req = withLocalWebReplyTarget(req)
	resolved, err := s.ResolveRunConfig(ctx,
		input.BotID,
		req.SessionID,
		firstNonEmpty(req.ChannelIdentityID, input.ActorChannelIdentityID),
		req.SourcePlatform,
		req.ReplyTarget,
		req.ConversationType,
		input.ChatToken,
	)
	if err != nil {
		return sdk.ToolResultPart{}, err
	}
	return s.agent.ExecuteTool(ctx, resolved.RunConfig, sdk.ToolCall{
		ToolCallID: req.ToolCallID,
		ToolName:   req.ToolName,
		Input:      req.ToolInput,
	})
}

func (s *Service) storeToolResultAndContinue(ctx context.Context, approval toolapproval.Request, input ToolApprovalResponseInput, result sdk.ToolResultPart, eventCh chan<- WSStreamEvent) error {
	approval = withLocalWebReplyTarget(approval)
	ctx = workspace.WithWorkspaceTarget(ctx, approval.WorkspaceTargetID)
	target, err := s.resolveWorkspaceTargetSnapshot(ctx, input.BotID, approval.WorkspaceTargetID)
	if err != nil {
		return err
	}
	modelMessages := sdkMessagesToModelMessages([]sdk.Message{sdk.ToolMessage(result)})
	storeReq := ChatRequest{
		BotID:                   input.BotID,
		ChatID:                  input.BotID,
		ThreadID:                approval.SessionID,
		SourceChannelIdentityID: firstNonEmpty(approval.ChannelIdentityID, input.ActorChannelIdentityID),
		CurrentChannel:          approval.SourcePlatform,
		ReplyTarget:             approval.ReplyTarget,
		ConversationType:        approval.ConversationType,
		UserMessagePersisted:    true,
		WorkspaceTargetID:       approval.WorkspaceTargetID,
	}
	storeReq.WorkspaceTarget = target
	if err := s.storeRoundWithOptions(ctx, storeReq, modelMessages, "", storeRoundOptions{AllowPendingToolCalls: true}); err != nil {
		return err
	}
	return s.continueToolApprovalSession(ctx, approval, input, eventCh)
}

func (s *Service) continueToolApprovalSession(ctx context.Context, approval toolapproval.Request, input ToolApprovalResponseInput, eventCh chan<- WSStreamEvent) error {
	approval = withLocalWebReplyTarget(approval)
	ctx = workspace.WithWorkspaceTarget(ctx, approval.WorkspaceTargetID)
	resolved, err := s.ResolveRunConfig(ctx,
		input.BotID,
		approval.SessionID,
		firstNonEmpty(approval.ChannelIdentityID, input.ActorChannelIdentityID),
		approval.SourcePlatform,
		approval.ReplyTarget,
		approval.ConversationType,
		input.ChatToken,
	)
	if err != nil {
		return err
	}

	cfg, err := s.prepareContinuationRunConfig(
		ctx,
		resolved.RunConfig,
		historyScopeFallbackFromToolApprovalRequest(approval),
		compactionSummaryScope(firstNonEmpty(approval.BotID, input.BotID), "", approval.SessionID, approval.ConversationType, "", approval.ReplyTarget),
		eventCh,
	)
	if err != nil {
		return err
	}

	req := ChatRequest{
		BotID:                   input.BotID,
		ChatID:                  input.BotID,
		ThreadID:                approval.SessionID,
		SourceChannelIdentityID: firstNonEmpty(approval.ChannelIdentityID, input.ActorChannelIdentityID),
		CurrentChannel:          approval.SourcePlatform,
		ReplyTarget:             approval.ReplyTarget,
		ConversationType:        approval.ConversationType,
		UserMessagePersisted:    true,
		WorkspaceTargetID:       approval.WorkspaceTargetID,
		WorkspaceTarget:         workspaceTargetFromRunConfig(resolved.RunConfig),
	}

	stream := s.agent.Stream(ctx, cfg)
	stored := false
	for event := range stream {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		if !stored && event.IsTerminal() && len(event.Messages) > 0 {
			if snap, ok := extractTerminalSnapshot(data); ok {
				if storeErr := s.persistTerminalSnapshot(
					context.WithoutCancel(ctx),
					req,
					resolvedContext{model: models.GetResponse{ID: resolved.ModelID}},
					snap,
				); storeErr != nil {
					return storeErr
				}
				stored = true
			}
		}
		if eventCh != nil {
			select {
			case eventCh <- json.RawMessage(data):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func withLocalWebReplyTarget(req toolapproval.Request) toolapproval.Request {
	if strings.EqualFold(strings.TrimSpace(req.SourcePlatform), "web") && strings.TrimSpace(req.ReplyTarget) == "" {
		req.ReplyTarget = strings.TrimSpace(req.BotID)
	}
	return req
}

func rejectedToolResultText(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "tool execution rejected by user"
	}
	return "tool execution rejected by user: " + reason
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
