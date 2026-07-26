package application

import (
	"context"
	"encoding/json"
	"fmt"

	sessionruntime "github.com/memohai/memoh/internal/agent/runtime/session"
)

func (c *DecisionCoordinator) bindDecisionCommandHandlers(manager runtimeManager) error {
	if c == nil || manager == nil || c.resolver == nil {
		return nil
	}
	return bindDecisionCommandHandlers(manager, c.resolver)
}

// BindDecisionCommandHandlers installs the shared owner command decoder on a manager.
// It remains exported for transports that assemble their dependencies through
// setters, while DecisionRouter owns the production application-level routing policy.
func BindDecisionCommandHandlers(manager *sessionruntime.Manager, commandResolver DecisionCommandResolver) error {
	if manager == nil || commandResolver == nil {
		return nil
	}
	return bindDecisionCommandHandlers(manager, commandResolver)
}

func bindDecisionCommandHandlers(manager runtimeManager, commandResolver DecisionCommandResolver) error {
	if manager == nil || commandResolver == nil {
		return nil
	}
	reconciler, ok := commandResolver.(commandReconciler)
	var reconcileApproval sessionruntime.CommandReconciler
	var reconcileInput sessionruntime.CommandReconciler
	if ok {
		reconcileApproval = func(ctx context.Context, command sessionruntime.Command) (bool, error) {
			input, err := decodeToolApprovalCommand(command)
			if err != nil {
				return true, err
			}
			handled, reconcileErr := reconciler.ReconcileToolApprovalResponse(ctx, input)
			return handled, normalizeDecisionResponseError(reconcileErr)
		}
		reconcileInput = func(ctx context.Context, command sessionruntime.Command) (bool, error) {
			input, err := decodeUserInputCommand(command)
			if err != nil {
				return true, err
			}
			handled, reconcileErr := reconciler.ReconcileUserInputResponse(ctx, input)
			return handled, normalizeDecisionResponseError(reconcileErr)
		}
	}
	if err := manager.RegisterCommandHandler(sessionruntime.CommandToolApprovalResponse, func(ctx context.Context, command sessionruntime.Command) error {
		input, err := decodeToolApprovalCommand(command)
		if err != nil {
			return err
		}
		committed, err := commandResolver.CommitToolApprovalResponse(ctx, input)
		if err != nil {
			return normalizeDecisionResponseError(err)
		}
		return commandResolver.ContinueCommittedToolApprovalResponse(ctx, committed, nil)
	}, reconcileApproval); err != nil {
		return fmt.Errorf("register tool approval command handler: %w", err)
	}
	if err := manager.RegisterCommandHandler(sessionruntime.CommandUserInputResponse, func(ctx context.Context, command sessionruntime.Command) error {
		input, err := decodeUserInputCommand(command)
		if err != nil {
			return err
		}
		committed, err := commandResolver.CommitUserInputResponse(ctx, input)
		if err != nil {
			return normalizeDecisionResponseError(err)
		}
		return commandResolver.ContinueCommittedUserInputResponse(ctx, committed, nil)
	}, reconcileInput); err != nil {
		return fmt.Errorf("register user input command handler: %w", err)
	}
	return nil
}

func decodeToolApprovalCommand(command sessionruntime.Command) (ToolApprovalResponseInput, error) {
	var input ToolApprovalResponseInput
	if err := json.Unmarshal(command.Payload, &input); err != nil {
		return input, fmt.Errorf("decode tool approval response: %w", err)
	}
	input.BotID = command.BotID
	input.ThreadID = command.SessionID
	input.ApprovalID = command.TargetID
	input.ExplicitID = command.TargetID
	input.ReplyExternalMessageID = ""
	input.ChatToken = ""
	input.SuppressActivePromptAttach = true
	input.ResolveOnly = true
	return input, nil
}

func decodeUserInputCommand(command sessionruntime.Command) (UserInputResponseInput, error) {
	var input UserInputResponseInput
	if err := json.Unmarshal(command.Payload, &input); err != nil {
		return input, fmt.Errorf("decode user input response: %w", err)
	}
	input.BotID = command.BotID
	input.ThreadID = command.SessionID
	input.UserInputID = command.TargetID
	input.ExplicitID = command.TargetID
	input.ReplyExternalMessageID = ""
	input.ChatToken = ""
	input.SuppressActivePromptAttach = true
	input.ResolveOnly = true
	return input, nil
}
