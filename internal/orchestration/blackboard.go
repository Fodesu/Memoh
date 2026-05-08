package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
	"github.com/memohai/memoh/internal/orchestrationblackboard"
)

// captureBlackboardRevisions snapshots the live blackboard view that a
// dispatched task should see. The returned list is JSON-friendly and gets
// written into orchestration_input_manifests.captured_blackboard_revisions
// so verifier replay can reach the same view through GetRevision later on.
//
// When the kernel was started without a blackboard store, this returns an
// empty list and the dispatch path keeps its current pre-Stage-2 shape.
func (s *Service) captureBlackboardRevisions(
	ctx context.Context,
	qtx *sqlc.Queries,
	runID, taskID pgtype.UUID,
) []map[string]any {
	if s == nil || s.bbStore == nil {
		return []map[string]any{}
	}
	var revisions []map[string]any

	runEntries, err := s.bbStore.List(ctx, orchestrationblackboard.RunKey(
		runID.String(),
		orchestrationblackboard.NamespaceContext,
	))
	switch {
	case errors.Is(err, orchestrationblackboard.ErrNotFound), err == nil:
	default:
		s.logger.Warn("blackboard list run context failed",
			slog.String("run_id", runID.String()),
			slog.Any("error", err))
	}
	for _, entry := range runEntries {
		revisions = append(revisions, blackboardRevisionEntry(entry))
	}

	deps, err := qtx.ListActiveOrchestrationTaskDependenciesBySuccessor(ctx, taskID)
	if err != nil {
		s.logger.Warn("blackboard capture failed listing dependencies",
			slog.String("task_id", taskID.String()),
			slog.Any("error", err))
		return revisions
	}
	for _, dep := range deps {
		predID := dep.PredecessorTaskID.String()
		entries, err := s.bbStore.List(ctx, orchestrationblackboard.TaskKey(
			predID,
			orchestrationblackboard.NamespaceResult,
		))
		if err != nil {
			s.logger.Warn("blackboard list predecessor result failed",
				slog.String("task_id", predID),
				slog.Any("error", err))
			continue
		}
		for _, entry := range entries {
			revisions = append(revisions, blackboardRevisionEntry(entry))
		}
	}

	if revisions == nil {
		return []map[string]any{}
	}
	return revisions
}

// publishTaskCompletionToBlackboard mirrors a fresh task result into the
// blackboard so downstream tasks reading via List see the latest revision
// and verifier replay finds an authoritative copy at
// bb.task.{task_id}.result.summary.
//
// The kernel is the orchestrator-class writer here, not the worker that
// produced the result, because the kernel owns the commit decision in
// Postgres and CAS would otherwise need to thread the worker's claim
// epoch through every kernel call site. The Postgres row remains the
// only authoritative copy; blackboard publish failures are logged and
// swallowed so they cannot block the kernel commit pipeline.
func (s *Service) publishTaskCompletionToBlackboard(
	ctx context.Context,
	attemptRow sqlc.OrchestrationTaskAttempt,
	completionStatus string,
	input AttemptCompletion,
) {
	if s == nil || s.bbStore == nil || s.bbWriter == nil {
		return
	}
	key := orchestrationblackboard.TaskKey(
		attemptRow.TaskID.String(),
		orchestrationblackboard.NamespaceResult,
		"summary",
	)
	payload := map[string]any{
		"attempt_id":        attemptRow.ID.String(),
		"task_id":           attemptRow.TaskID.String(),
		"run_id":            attemptRow.RunID.String(),
		"claim_epoch":       attemptRow.ClaimEpoch,
		"status":            completionStatus,
		"summary":           strings.TrimSpace(input.Summary),
		"failure_class":     strings.TrimSpace(input.FailureClass),
		"request_replan":    input.RequestReplan,
		"structured_output": input.StructuredOutput,
	}

	var expected orchestrationblackboard.Revision
	if entry, err := s.bbStore.Get(ctx, key); err == nil {
		expected = entry.Revision
	}

	if _, err := s.bbWriter.CompareAndSwap(
		ctx,
		key,
		expected,
		orchestrationblackboard.PersistenceFromPostgres,
		payload,
	); err != nil {
		s.logger.Warn("blackboard publish completion failed",
			slog.String("task_id", attemptRow.TaskID.String()),
			slog.String("attempt_id", attemptRow.ID.String()),
			slog.Any("error", err))
	}
}

func blackboardRevisionEntry(entry orchestrationblackboard.Entry) map[string]any {
	return map[string]any{
		"key":      entry.Key.String(),
		"revision": uint64(entry.Revision),
	}
}

// RebuildBlackboardResult summarises a RebuildBlackboard pass for callers
// (admin UI, support tooling). Counts are the number of blackboard
// entries written for each source, not the number of Postgres rows
// scanned.
type RebuildBlackboardResult struct {
	RunContextWritten    int  `json:"run_context_written"`
	TaskResultsWritten   int  `json:"task_results_written"`
	WriteErrors          int  `json:"write_errors"`
	BlackboardConfigured bool `json:"blackboard_configured"`
}

// RebuildBlackboard reconstructs the blackboard runtime view for a run
// from Postgres. It is the recovery primitive of last resort: after a
// JetStream KV bucket loss, an admin or supervisor can invoke this to
// repopulate run-scope context and committed task results without
// touching authoritative Postgres state.
//
// The rebuild is idempotent. Existing values are overwritten with the
// current Postgres view; revisions move forward. Callers should keep the
// run quiesced (no in-flight task completions) for the duration so live
// writers do not race the rebuild path.
func (s *Service) RebuildBlackboard(ctx context.Context, caller ControlIdentity, runID string) (RebuildBlackboardResult, error) {
	if s == nil {
		return RebuildBlackboardResult{}, errors.New("orchestration: nil service")
	}
	caller, normalizeErr := normalizeControlIdentity(caller)
	if normalizeErr != nil {
		return RebuildBlackboardResult{}, normalizeErr
	}
	pgRunID, err := db.ParseUUID(runID)
	if err != nil {
		return RebuildBlackboardResult{}, fmt.Errorf("%w: invalid run id", ErrInvalidArgument)
	}
	runRow, err := s.queries.GetOrchestrationRunByID(ctx, pgRunID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RebuildBlackboardResult{}, ErrRunNotFound
		}
		return RebuildBlackboardResult{}, fmt.Errorf("load run for rebuild: %w", err)
	}
	if err := authorizeRun(caller, runRow); err != nil {
		return RebuildBlackboardResult{}, err
	}
	if s.bbStore == nil || s.bbWriter == nil {
		return RebuildBlackboardResult{BlackboardConfigured: false}, nil
	}

	result := RebuildBlackboardResult{BlackboardConfigured: true}

	if rebuildErr := s.rebuildRunContext(ctx, runRow); rebuildErr != nil {
		s.logger.Warn("rebuild blackboard run context failed",
			slog.String("run_id", runID),
			slog.Any("error", rebuildErr))
		result.WriteErrors++
	} else {
		result.RunContextWritten++
	}

	results, err := s.queries.ListCurrentOrchestrationTaskResultsByRun(ctx, pgRunID)
	if err != nil {
		return result, fmt.Errorf("list task results for rebuild: %w", err)
	}
	for _, row := range results {
		attemptRow, attemptErr := s.queries.GetOrchestrationTaskAttemptByID(ctx, row.AttemptID)
		if attemptErr != nil {
			s.logger.Warn("rebuild blackboard task result skipped (attempt missing)",
				slog.String("task_id", row.TaskID.String()),
				slog.String("attempt_id", row.AttemptID.String()),
				slog.Any("error", attemptErr))
			result.WriteErrors++
			continue
		}
		structured := decodeJSONObject(row.StructuredOutput)
		payload := map[string]any{
			"attempt_id":        attemptRow.ID.String(),
			"task_id":           attemptRow.TaskID.String(),
			"run_id":            attemptRow.RunID.String(),
			"claim_epoch":       attemptRow.ClaimEpoch,
			"status":            row.Status,
			"summary":           strings.TrimSpace(row.Summary),
			"failure_class":     strings.TrimSpace(row.FailureClass),
			"request_replan":    row.RequestReplan,
			"structured_output": structured,
		}
		key := orchestrationblackboard.TaskKey(
			attemptRow.TaskID.String(),
			orchestrationblackboard.NamespaceResult,
			"summary",
		)
		var expected orchestrationblackboard.Revision
		if entry, getErr := s.bbStore.Get(ctx, key); getErr == nil {
			expected = entry.Revision
		}
		if _, putErr := s.bbWriter.CompareAndSwap(
			ctx,
			key,
			expected,
			orchestrationblackboard.PersistenceFromPostgres,
			payload,
		); putErr != nil {
			s.logger.Warn("rebuild blackboard task result write failed",
				slog.String("task_id", attemptRow.TaskID.String()),
				slog.Any("error", putErr))
			result.WriteErrors++
			continue
		}
		result.TaskResultsWritten++
	}
	return result, nil
}

func (s *Service) rebuildRunContext(ctx context.Context, runRow sqlc.OrchestrationRun) error {
	key := orchestrationblackboard.RunKey(
		runRow.ID.String(),
		orchestrationblackboard.NamespaceContext,
		"goal",
	)
	payload := map[string]any{
		"run_id":           runRow.ID.String(),
		"goal":             strings.TrimSpace(runRow.Goal),
		"input":            decodeJSONObject(runRow.Input),
		"control_policy":   decodeJSONObject(runRow.ControlPolicy),
		"source_metadata":  decodeJSONObject(runRow.SourceMetadata),
		"lifecycle_status": runRow.LifecycleStatus,
		"planner_epoch":    runRow.PlannerEpoch,
	}
	var expected orchestrationblackboard.Revision
	if entry, err := s.bbStore.Get(ctx, key); err == nil {
		expected = entry.Revision
	}
	_, err := s.bbWriter.CompareAndSwap(
		ctx,
		key,
		expected,
		orchestrationblackboard.PersistenceFromPostgres,
		payload,
	)
	return err
}
