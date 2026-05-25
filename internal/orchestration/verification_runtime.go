package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
)

const (
	verificationCompletionRetryInterval      = 250 * time.Millisecond
	inProcessVerificationExecutorConcurrency = 2
)

type VerificationLeaseRuntime interface {
	HeartbeatVerification(context.Context, VerificationHeartbeat) (*TaskVerification, error)
	CompleteVerification(context.Context, VerificationCompletion) (*TaskVerification, error)
}

type VerificationRunner func(context.Context, TaskVerification, []string) VerificationCompletion

func (s *Service) ClaimNextVerification(ctx context.Context, claim VerificationClaim) (*TaskVerification, error) {
	workerID := strings.TrimSpace(claim.WorkerID)
	if workerID == "" {
		return nil, fmt.Errorf("%w: worker_id is required", ErrInvalidArgument)
	}
	executorID := strings.TrimSpace(claim.ExecutorID)
	if executorID == "" {
		executorID = DefaultVerifierExecutorID
	}
	ttl := normalizeLeaseTTL(claim.LeaseTTLSeconds, TaskVerificationDefaultLeaseTTL)
	supportedProfiles := normalizeWorkerProfiles(claim.VerifierProfiles)
	if len(supportedProfiles) == 0 {
		return nil, fmt.Errorf("%w: verifier_profiles is required", ErrInvalidArgument)
	}
	leaseToken := strings.TrimSpace(claim.LeaseToken)

	lease, err := s.RegisterWorker(ctx, WorkerRegistration{
		WorkerID:        workerID,
		ExecutorID:      executorID,
		DisplayName:     workerID,
		Capabilities:    verifierCapabilities(supportedProfiles),
		LeaseToken:      leaseToken,
		LeaseTTLSeconds: int(ttl / time.Second),
	})
	if err != nil {
		return nil, err
	}

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin verification claim tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := tx.ClaimNextCreatedOrchestrationTaskVerification(ctx, sqlc.ClaimNextCreatedOrchestrationTaskVerificationParams{
		WorkerID:         workerID,
		ExecutorID:       executorID,
		WorkerLeaseToken: lease.LeaseToken,
		VerifierProfiles: supportedProfiles,
		ClaimToken:       uuid.NewString(),
		LeaseTtlSeconds:  leaseTTLSecondsValue(ttl),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoRunnableVerification
		}
		return nil, fmt.Errorf("claim task verification: %w", err)
	}
	if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
		TaskID:           row.TaskID,
		AggregateType:    "verification",
		AggregateID:      row.ID,
		AggregateVersion: row.ClaimEpoch,
		Type:             "run.event.verification.claimed",
		Payload: map[string]any{
			"verification_id":  row.ID.String(),
			"task_id":          row.TaskID.String(),
			"result_id":        row.ResultID.String(),
			"status":           row.Status,
			"claim_epoch":      row.ClaimEpoch,
			"claim_token":      row.ClaimToken,
			"worker_id":        row.WorkerID,
			"executor_id":      row.ExecutorID,
			"verifier_profile": row.VerifierProfile,
			"lease_expires_at": timeForJSON(db.TimeFromPg(row.LeaseExpiresAt)),
		},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit verification claim tx: %w", err)
	}
	verification := toTaskVerification(row)
	return &verification, nil
}

func (s *Service) AttachVerificationSession(ctx context.Context, verificationID, sessionID string) (*TaskVerification, error) {
	pgVerificationID, err := db.ParseUUID(verificationID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid verification id", ErrInvalidArgument)
	}
	pgSessionID, err := db.ParseUUID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid session id", ErrInvalidArgument)
	}
	row, err := s.queries.AttachOrchestrationTaskVerificationSession(ctx, sqlc.AttachOrchestrationTaskVerificationSessionParams{
		ID:        pgVerificationID,
		SessionID: pgSessionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVerificationNotFound
		}
		return nil, fmt.Errorf("attach verification session: %w", err)
	}
	verification := toTaskVerification(row)
	return &verification, nil
}

func (s *Service) ProcessNextVerification(ctx context.Context, workerID, leaseToken string, verifierProfiles []string, leaseTTLSeconds int) (bool, error) {
	if strings.TrimSpace(workerID) == "" {
		return false, fmt.Errorf("%w: worker_id is required", ErrInvalidArgument)
	}
	profiles := normalizeWorkerProfiles(verifierProfiles)
	if len(profiles) == 0 {
		return false, fmt.Errorf("%w: verifier_profiles is required", ErrInvalidArgument)
	}

	verification, err := s.ClaimNextVerification(ctx, VerificationClaim{
		WorkerID:         workerID,
		ExecutorID:       DefaultVerifierExecutorID,
		VerifierProfiles: profiles,
		LeaseToken:       leaseToken,
		LeaseTTLSeconds:  leaseTTLSeconds,
	})
	if err != nil {
		if errors.Is(err, ErrNoRunnableVerification) {
			return false, nil
		}
		return false, err
	}

	runningVerification, err := s.StartVerification(ctx, verification.ID, verification.ClaimToken)
	if err != nil {
		if errors.Is(err, ErrVerificationImmutable) || errors.Is(err, ErrVerificationLeaseConflict) {
			return true, nil
		}
		return false, err
	}

	executor := s.verificationExecutor
	if executor == nil {
		executor = verificationExecutorFunc(func(execCtx context.Context, verification TaskVerification) VerificationCompletion {
			return ExecuteBuiltinVerification(execCtx, s.queries, verification)
		})
	}
	leaseLost := RunClaimedVerification(ctx, s, s.logger, *runningVerification, leaseTTLSeconds, profiles, func(execCtx context.Context, verification TaskVerification, _ []string) VerificationCompletion {
		return executor.ExecuteVerification(execCtx, verification)
	})
	if leaseLost {
		return true, nil
	}
	return true, nil
}

func (s *Service) RunVerifierLoop(ctx context.Context) {
	var wg sync.WaitGroup
	for lane := 0; lane < inProcessVerificationExecutorConcurrency; lane++ {
		lane := lane
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerID := fmt.Sprintf("server-verifyd-%02d-%s", lane+1, uuid.NewString())
			leaseToken := uuid.NewString()
			profiles := []string{DefaultVerifierProfile, BuiltinBasicVerifierProfile}
			runBoolLoop(ctx, s.logger, "verifier", 200*time.Millisecond, func(loopCtx context.Context) (bool, error) {
				return s.ProcessNextVerification(loopCtx, workerID, leaseToken, profiles, int(TaskVerificationDefaultLeaseTTL/time.Second))
			})
		}()
	}
	<-ctx.Done()
	wg.Wait()
}

func builtinVerifierProfiles(verifierProfiles []string) []string {
	normalized := normalizeWorkerProfiles(verifierProfiles)
	for _, profile := range normalized {
		if profile == BuiltinBasicVerifierProfile || profile == DefaultVerifierProfile {
			return []string{BuiltinBasicVerifierProfile}
		}
	}
	return nil
}

func (s *Service) StartVerification(ctx context.Context, verificationID, claimToken string) (*TaskVerification, error) {
	pgVerificationID, err := db.ParseUUID(verificationID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid verification id", ErrInvalidArgument)
	}
	normalizedClaimToken := strings.TrimSpace(claimToken)
	if normalizedClaimToken == "" {
		return nil, fmt.Errorf("%w: claim token is required", ErrInvalidArgument)
	}

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin start verification tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := tx.GetOrchestrationTaskVerificationByIDForUpdate(ctx, pgVerificationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVerificationNotFound
		}
		return nil, fmt.Errorf("lock task verification: %w", err)
	}
	if strings.TrimSpace(row.ClaimToken) != normalizedClaimToken {
		return nil, ErrVerificationLeaseConflict
	}
	observedAt, err := tx.DatabaseNow(ctx)
	if err != nil {
		return nil, err
	}
	if leaseExpiredAt(row.LeaseExpiresAt, observedAt) {
		return nil, ErrVerificationLeaseConflict
	}
	if ok, err := ensureWorkerLeaseSnapshot(ctx, tx, row.WorkerID, row.ExecutorID, row.WorkerLeaseToken, observedAt); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrVerificationLeaseConflict
	}
	if row.Status != TaskVerificationStatusClaimed && row.Status != TaskVerificationStatusRunning {
		return nil, ErrVerificationImmutable
	}

	runRow, err := tx.GetOrchestrationRunByIDForUpdate(ctx, row.RunID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRunNotFound
		}
		return nil, fmt.Errorf("lock run for start verification: %w", err)
	}
	if runRow.LifecycleStatus != LifecycleStatusRunning {
		if runRow.LifecycleStatus == LifecycleStatusCancelling && row.Status == TaskVerificationStatusClaimed {
			taskRow, taskErr := tx.GetOrchestrationTaskByIDForUpdate(ctx, row.TaskID)
			if taskErr != nil {
				if errors.Is(taskErr, pgx.ErrNoRows) {
					return nil, ErrTaskNotFound
				}
				return nil, fmt.Errorf("lock task for invalid verification claim after run state change: %w", taskErr)
			}
			lostRow, releaseErr := tx.MarkOrchestrationTaskVerificationLost(ctx, sqlc.MarkOrchestrationTaskVerificationLostParams{
				ID:             row.ID,
				ClaimEpoch:     row.ClaimEpoch,
				Verdict:        VerificationVerdictRejected,
				Summary:        "run cancelled",
				FailureClass:   "run_cancelled",
				TerminalReason: "run cancelled",
			})
			if releaseErr != nil {
				if errors.Is(releaseErr, pgx.ErrNoRows) {
					return nil, ErrVerificationLeaseConflict
				}
				return nil, fmt.Errorf("retire invalid verification claim after run state change: %w", releaseErr)
			}
			if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
				TaskID:           lostRow.TaskID,
				AggregateType:    "verification",
				AggregateID:      lostRow.ID,
				AggregateVersion: lostRow.ClaimEpoch,
				Type:             "run.event.verification.lost",
				Payload: map[string]any{
					"verification_id": lostRow.ID.String(),
					"task_id":         lostRow.TaskID.String(),
					"previous_status": row.Status,
					"new_status":      lostRow.Status,
					"terminal_reason": lostRow.TerminalReason,
				},
			}); err != nil {
				return nil, err
			}
			if err := s.cancelTaskDuringRunCancellation(ctx, tx, runRow, taskRow, pgtype.UUID{}); err != nil {
				return nil, err
			}
		} else if row.Status == TaskVerificationStatusClaimed {
			if _, releaseErr := tx.ReleaseOrchestrationTaskVerificationClaim(ctx, sqlc.ReleaseOrchestrationTaskVerificationClaimParams{
				ID:         row.ID,
				ClaimToken: normalizedClaimToken,
			}); releaseErr != nil && !errors.Is(releaseErr, pgx.ErrNoRows) {
				return nil, fmt.Errorf("release invalid verification claim after run state change: %w", releaseErr)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit invalid verification claim after run state change: %w", err)
		}
		return nil, ErrVerificationImmutable
	}

	taskRow, err := tx.GetOrchestrationTaskByIDForUpdate(ctx, row.TaskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("lock task for start verification: %w", err)
	}
	if taskRow.Status != TaskStatusVerifying || taskRow.SupersededByPlannerEpoch.Valid {
		if runRow.LifecycleStatus == LifecycleStatusCancelling && row.Status == TaskVerificationStatusClaimed {
			lostRow, releaseErr := tx.MarkOrchestrationTaskVerificationLost(ctx, sqlc.MarkOrchestrationTaskVerificationLostParams{
				ID:             row.ID,
				ClaimEpoch:     row.ClaimEpoch,
				Verdict:        VerificationVerdictRejected,
				Summary:        "run cancelled",
				FailureClass:   "run_cancelled",
				TerminalReason: "run cancelled",
			})
			if releaseErr != nil {
				if errors.Is(releaseErr, pgx.ErrNoRows) {
					return nil, ErrVerificationLeaseConflict
				}
				return nil, fmt.Errorf("retire invalid verification claim after task state change: %w", releaseErr)
			}
			if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
				TaskID:           lostRow.TaskID,
				AggregateType:    "verification",
				AggregateID:      lostRow.ID,
				AggregateVersion: lostRow.ClaimEpoch,
				Type:             "run.event.verification.lost",
				Payload: map[string]any{
					"verification_id": lostRow.ID.String(),
					"task_id":         lostRow.TaskID.String(),
					"previous_status": row.Status,
					"new_status":      lostRow.Status,
					"terminal_reason": lostRow.TerminalReason,
				},
			}); err != nil {
				return nil, err
			}
			if err := s.cancelTaskDuringRunCancellation(ctx, tx, runRow, taskRow, pgtype.UUID{}); err != nil {
				return nil, err
			}
		} else if row.Status == TaskVerificationStatusClaimed {
			if _, releaseErr := tx.ReleaseOrchestrationTaskVerificationClaim(ctx, sqlc.ReleaseOrchestrationTaskVerificationClaimParams{
				ID:         row.ID,
				ClaimToken: normalizedClaimToken,
			}); releaseErr != nil && !errors.Is(releaseErr, pgx.ErrNoRows) {
				return nil, fmt.Errorf("release invalid verification claim after task state change: %w", releaseErr)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit invalid verification claim after task state change: %w", err)
		}
		return nil, ErrVerificationImmutable
	}
	if row.Status == TaskVerificationStatusRunning {
		verification := toTaskVerification(row)
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit replayed start verification tx: %w", err)
		}
		return &verification, nil
	}

	running, err := tx.MarkOrchestrationTaskVerificationRunning(ctx, sqlc.MarkOrchestrationTaskVerificationRunningParams{
		ID:         row.ID,
		ClaimToken: normalizedClaimToken,
	})
	if err != nil {
		return nil, fmt.Errorf("mark verification running: %w", err)
	}
	if _, err := s.appendEvent(ctx, tx, running.RunID, eventSpec{
		TaskID:           running.TaskID,
		AggregateType:    "verification",
		AggregateID:      running.ID,
		AggregateVersion: running.ClaimEpoch,
		Type:             "run.event.verification.running",
		Payload: map[string]any{
			"verification_id":  running.ID.String(),
			"task_id":          running.TaskID.String(),
			"previous_status":  row.Status,
			"new_status":       running.Status,
			"claim_epoch":      running.ClaimEpoch,
			"claim_token":      running.ClaimToken,
			"verifier_profile": running.VerifierProfile,
			"lease_expires_at": timeForJSON(db.TimeFromPg(running.LeaseExpiresAt)),
		},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit start verification tx: %w", err)
	}
	verification := toTaskVerification(running)
	return &verification, nil
}

func (s *Service) HeartbeatVerification(ctx context.Context, input VerificationHeartbeat) (*TaskVerification, error) {
	pgVerificationID, err := db.ParseUUID(input.VerificationID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid verification id", ErrInvalidArgument)
	}
	normalizedClaimToken := strings.TrimSpace(input.ClaimToken)
	if normalizedClaimToken == "" {
		return nil, fmt.Errorf("%w: claim token is required", ErrInvalidArgument)
	}

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin verification heartbeat tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := tx.GetOrchestrationTaskVerificationByIDForUpdate(ctx, pgVerificationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVerificationNotFound
		}
		return nil, fmt.Errorf("lock task verification for heartbeat: %w", err)
	}
	if strings.TrimSpace(row.ClaimToken) != normalizedClaimToken {
		return nil, ErrVerificationLeaseConflict
	}
	if row.Status != TaskVerificationStatusClaimed && row.Status != TaskVerificationStatusRunning {
		return nil, ErrVerificationImmutable
	}
	observedAt, err := tx.DatabaseNow(ctx)
	if err != nil {
		return nil, err
	}
	if leaseExpiredAt(row.LeaseExpiresAt, observedAt) {
		return nil, ErrVerificationLeaseConflict
	}
	if err := heartbeatWorkerLeaseInTx(ctx, tx, row.WorkerID, row.WorkerLeaseToken, normalizeLeaseTTL(input.LeaseTTLSeconds, TaskVerificationDefaultLeaseTTL), ErrVerificationLeaseConflict); err != nil {
		return nil, err
	}
	if ok, err := ensureWorkerLeaseSnapshot(ctx, tx, row.WorkerID, row.ExecutorID, row.WorkerLeaseToken, observedAt); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrVerificationLeaseConflict
	}

	runRow, err := tx.GetOrchestrationRunByIDForUpdate(ctx, row.RunID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRunNotFound
		}
		return nil, fmt.Errorf("lock run for verification heartbeat: %w", err)
	}
	runAlreadyFailed := runRow.LifecycleStatus == LifecycleStatusFailed
	if runRow.LifecycleStatus != LifecycleStatusRunning && !runAlreadyFailed {
		if runRow.LifecycleStatus == LifecycleStatusCancelling {
			taskRow, taskErr := tx.GetOrchestrationTaskByIDForUpdate(ctx, row.TaskID)
			if taskErr != nil {
				if errors.Is(taskErr, pgx.ErrNoRows) {
					return nil, ErrTaskNotFound
				}
				return nil, fmt.Errorf("lock task for verification heartbeat cancellation: %w", taskErr)
			}
			if _, err := s.retireVerificationDuringRunCancellation(ctx, tx, runRow, row); err != nil {
				return nil, err
			}
			if err := s.cancelTaskDuringRunCancellation(ctx, tx, runRow, taskRow, pgtype.UUID{}); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit verification heartbeat cancellation tx: %w", err)
			}
			return nil, ErrVerificationImmutable
		}
		return nil, ErrVerificationImmutable
	}

	taskRow, err := tx.GetOrchestrationTaskByIDForUpdate(ctx, row.TaskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("lock task for verification heartbeat: %w", err)
	}
	if taskRow.Status != TaskStatusVerifying || taskRow.SupersededByPlannerEpoch.Valid {
		return nil, ErrVerificationImmutable
	}

	updated, err := tx.HeartbeatOrchestrationTaskVerification(ctx, sqlc.HeartbeatOrchestrationTaskVerificationParams{
		ID:              pgVerificationID,
		ClaimToken:      normalizedClaimToken,
		LeaseTtlSeconds: leaseTTLSecondsValue(normalizeLeaseTTL(input.LeaseTTLSeconds, TaskVerificationDefaultLeaseTTL)),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVerificationLeaseConflict
		}
		return nil, fmt.Errorf("heartbeat task verification: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit verification heartbeat tx: %w", err)
	}
	verification := toTaskVerification(updated)
	return &verification, nil
}

func (s *Service) CompleteVerification(ctx context.Context, input VerificationCompletion) (*TaskVerification, error) {
	pgVerificationID, err := db.ParseUUID(input.VerificationID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid verification id", ErrInvalidArgument)
	}
	normalizedClaimToken := strings.TrimSpace(input.ClaimToken)
	if normalizedClaimToken == "" {
		return nil, fmt.Errorf("%w: claim token is required", ErrInvalidArgument)
	}
	completionStatus := normalizeVerificationCompletionStatus(input.Status)
	if completionStatus == "" {
		return nil, fmt.Errorf("%w: unsupported verification completion status %q", ErrInvalidArgument, input.Status)
	}
	verdict := normalizeVerificationVerdict(input.Verdict)
	if verdict == "" {
		return nil, fmt.Errorf("%w: unsupported verification verdict %q", ErrInvalidArgument, input.Verdict)
	}
	if input.RequestReplan && (verdict != VerificationVerdictRejected || completionStatus != TaskVerificationStatusCompleted) {
		return nil, fmt.Errorf("%w: request_replan requires completed rejected verification", ErrInvalidArgument)
	}
	if verdict == VerificationVerdictAccepted {
		if completionStatus != TaskVerificationStatusCompleted {
			return nil, fmt.Errorf("%w: accepted verification must complete successfully", ErrInvalidArgument)
		}
		if input.RequestReplan {
			return nil, fmt.Errorf("%w: accepted verification cannot request_replan", ErrInvalidArgument)
		}
	}
	if completionStatus == TaskVerificationStatusFailed && verdict != VerificationVerdictRejected {
		return nil, fmt.Errorf("%w: failed verification must be rejected", ErrInvalidArgument)
	}

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin complete verification tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := tx.GetOrchestrationTaskVerificationByIDForUpdate(ctx, pgVerificationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrVerificationNotFound
		}
		return nil, fmt.Errorf("lock task verification for completion: %w", err)
	}
	if strings.TrimSpace(row.ClaimToken) != normalizedClaimToken {
		return nil, ErrVerificationLeaseConflict
	}
	if row.Status == TaskVerificationStatusLost {
		return nil, ErrVerificationLeaseConflict
	}
	if isTerminalVerificationStatus(row.Status) {
		verification := toTaskVerification(row)
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit replayed complete verification tx: %w", err)
		}
		return &verification, nil
	}
	observedAt, err := tx.DatabaseNow(ctx)
	if err != nil {
		return nil, err
	}
	if leaseExpiredAt(row.LeaseExpiresAt, observedAt) {
		return nil, ErrVerificationLeaseConflict
	}
	if row.Status != TaskVerificationStatusRunning {
		return nil, ErrVerificationImmutable
	}
	if ok, err := ensureWorkerLeaseSnapshot(ctx, tx, row.WorkerID, row.ExecutorID, row.WorkerLeaseToken, observedAt); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrVerificationLeaseConflict
	}

	runRow, err := tx.GetOrchestrationRunByIDForUpdate(ctx, row.RunID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRunNotFound
		}
		return nil, fmt.Errorf("lock run for verification completion: %w", err)
	}
	runAlreadyFailed := runRow.LifecycleStatus == LifecycleStatusFailed
	if runRow.LifecycleStatus != LifecycleStatusRunning && !runAlreadyFailed {
		if runRow.LifecycleStatus == LifecycleStatusCancelling {
			taskRow, taskErr := tx.GetOrchestrationTaskByIDForUpdate(ctx, row.TaskID)
			if taskErr != nil {
				if errors.Is(taskErr, pgx.ErrNoRows) {
					return nil, ErrTaskNotFound
				}
				return nil, fmt.Errorf("lock task for verification completion cancellation: %w", taskErr)
			}
			cancelledVerification, err := s.retireVerificationDuringRunCancellation(ctx, tx, runRow, row)
			if err != nil {
				return nil, err
			}
			if err := s.cancelTaskDuringRunCancellation(ctx, tx, runRow, taskRow, pgtype.UUID{}); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit verification completion cancellation tx: %w", err)
			}
			verification := toTaskVerification(cancelledVerification)
			return &verification, nil
		}
		return nil, ErrVerificationImmutable
	}

	taskRow, err := tx.GetOrchestrationTaskByIDForUpdate(ctx, row.TaskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("lock task for verification completion: %w", err)
	}
	if taskRow.Status != TaskStatusVerifying || taskRow.SupersededByPlannerEpoch.Valid {
		return nil, ErrVerificationImmutable
	}
	verificationPolicy := decodeJSONObject(taskRow.VerificationPolicy)
	rejectAction := verificationRejectAction(verificationPolicy)
	if input.RequestReplan && rejectAction != VerificationRejectActionReplan {
		return nil, fmt.Errorf("%w: verification policy does not allow request_replan", ErrInvalidArgument)
	}

	resultRow, err := tx.GetOrchestrationTaskResultByID(ctx, row.ResultID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("load verification result: %w", err)
	}

	var finalRow sqlc.OrchestrationTaskVerification
	switch completionStatus {
	case TaskVerificationStatusCompleted:
		finalRow, err = tx.MarkOrchestrationTaskVerificationCompleted(ctx, sqlc.MarkOrchestrationTaskVerificationCompletedParams{
			ID:             row.ID,
			ClaimToken:     normalizedClaimToken,
			Verdict:        verdict,
			Summary:        strings.TrimSpace(input.Summary),
			FailureClass:   strings.TrimSpace(input.FailureClass),
			TerminalReason: strings.TrimSpace(input.TerminalReason),
		})
	case TaskVerificationStatusFailed:
		finalRow, err = tx.MarkOrchestrationTaskVerificationFailed(ctx, sqlc.MarkOrchestrationTaskVerificationFailedParams{
			ID:             row.ID,
			ClaimToken:     normalizedClaimToken,
			Verdict:        verdict,
			Summary:        strings.TrimSpace(input.Summary),
			FailureClass:   strings.TrimSpace(input.FailureClass),
			TerminalReason: normalizeVerificationTerminalReason(input.TerminalReason, verdict),
		})
	default:
		return nil, fmt.Errorf("%w: unsupported verification completion status %q", ErrInvalidArgument, completionStatus)
	}
	if err != nil {
		return nil, fmt.Errorf("mark verification terminal: %w", err)
	}

	eventType := "run.event.verification.rejected"
	if verdict == VerificationVerdictAccepted {
		eventType = "run.event.verification.passed"
	}
	if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
		TaskID:           row.TaskID,
		AggregateType:    "verification",
		AggregateID:      finalRow.ID,
		AggregateVersion: finalRow.ClaimEpoch,
		Type:             eventType,
		Payload: map[string]any{
			"verification_id": finalRow.ID.String(),
			"task_id":         finalRow.TaskID.String(),
			"result_id":       finalRow.ResultID.String(),
			"previous_status": row.Status,
			"new_status":      finalRow.Status,
			"verdict":         finalRow.Verdict,
			"summary":         finalRow.Summary,
			"failure_class":   finalRow.FailureClass,
			"terminal_reason": finalRow.TerminalReason,
			"request_replan":  input.RequestReplan,
		},
	}); err != nil {
		return nil, err
	}

	switch {
	case verdict == VerificationVerdictAccepted:
		if !runAlreadyFailed && resultRow.RequestReplan {
			childPlans := decodePlannedChildTasks(decodeJSONObject(resultRow.StructuredOutput))
			if len(childPlans) == 0 {
				return nil, fmt.Errorf("%w: request_replan requires structured_output.child_tasks", ErrPlanningIntentInvalid)
			}
			childPlans = normalizeReplanMidTasks(childPlans)
			if err := validateReplanMidTasks(childPlans); err != nil {
				return nil, err
			}
			if !resultRow.AttemptID.Valid {
				return nil, fmt.Errorf("%w: request_replan requires attempt_id", ErrPlanningIntentInvalid)
			}
			attemptRow, err := tx.GetOrchestrationTaskAttemptByID(ctx, resultRow.AttemptID)
			if err != nil {
				return nil, fmt.Errorf("load attempt for verified request_replan: %w", err)
			}
			if _, err := s.applyReplanDAG(ctx, tx, runRow, taskRow, attemptRow.ID, replanOrigin{
				Kind: "verification",
				ID:   finalRow.ID,
			}, childPlans, "verification.accepted_request_replan", &replanSourceCompletion{
				ResultID:         row.ResultID,
				ResultIDString:   row.ResultID.String(),
				PreviousStatus:   taskRow.Status,
				StructuredOutput: decodeJSONObject(resultRow.StructuredOutput),
				Reason:           "verification_passed_request_replan_dag_replaced",
			}); err != nil {
				return nil, err
			}
			break
		}

		completedTask, err := tx.MarkOrchestrationTaskCompleted(ctx, sqlc.MarkOrchestrationTaskCompletedParams{
			ID:             taskRow.ID,
			LatestResultID: row.ResultID,
		})
		if err != nil {
			return nil, fmt.Errorf("mark task completed after verification: %w", err)
		}
		if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
			TaskID:           completedTask.ID,
			AggregateType:    "task",
			AggregateID:      completedTask.ID,
			AggregateVersion: completedTask.StatusVersion,
			Type:             "run.event.task.completed",
			Payload: map[string]any{
				"task_id":           completedTask.ID.String(),
				"previous_status":   taskRow.Status,
				"new_status":        completedTask.Status,
				"latest_result_id":  row.ResultID.String(),
				"completion_reason": "verification_passed",
			},
		}); err != nil {
			return nil, err
		}
		if runAlreadyFailed {
			break
		}
		if _, err := s.activateDependencySatisfiedTasks(ctx, tx); err != nil {
			return nil, err
		}
		if _, _, err := s.maybeMarkRunCompletedAfterTaskTransition(ctx, tx, runRow, taskRow.ID, resultRow.AttemptID); err != nil {
			return nil, err
		}
	case input.RequestReplan:
		if runAlreadyFailed {
			failedTask, err := s.failTaskFromVerification(ctx, tx, taskRow, row.ResultID, finalRow, "run_already_failed")
			if err != nil {
				return nil, err
			}
			_ = failedTask
			break
		}
		failedTask, err := s.failTaskFromVerification(ctx, tx, taskRow, row.ResultID, finalRow, "verification_requested_replan")
		if err != nil {
			return nil, err
		}
		childPlans := decodePlannedChildTasks(decodeJSONObject(resultRow.StructuredOutput))
		if len(childPlans) == 0 {
			if err := s.markRunFailedFromVerificationFailure(ctx, tx, runRow, failedTask, finalRow); err != nil {
				return nil, err
			}
			break
		}
		if err := validatePlannedChildTasks(childPlans); err != nil {
			if err := s.markRunFailedFromVerificationFailure(ctx, tx, runRow, failedTask, finalRow); err != nil {
				return nil, err
			}
			break
		}
		if !resultRow.AttemptID.Valid {
			if err := s.markRunFailedFromVerificationFailure(ctx, tx, runRow, failedTask, finalRow); err != nil {
				return nil, err
			}
			break
		}
		attemptRow, err := tx.GetOrchestrationTaskAttemptByID(ctx, resultRow.AttemptID)
		if err != nil {
			if markErr := s.markRunFailedFromVerificationFailure(ctx, tx, runRow, failedTask, finalRow); markErr != nil {
				return nil, markErr
			}
			break
		}
		if _, err := s.enqueueReplanPlanningIntent(ctx, tx, runRow, taskRow, attemptRow, childPlans, "verification.request_replan"); err != nil {
			if markErr := s.markRunFailedFromVerificationFailure(ctx, tx, runRow, failedTask, finalRow); markErr != nil {
				return nil, markErr
			}
			break
		}
		if err := s.syncRunPlanningStatus(ctx, tx, runRow.ID); err != nil {
			return nil, err
		}
	default:
		if runAlreadyFailed {
			failedTask, err := s.failTaskFromVerification(ctx, tx, taskRow, row.ResultID, finalRow, "run_already_failed")
			if err != nil {
				return nil, err
			}
			_ = failedTask
			break
		}
		failedTask, err := tx.MarkOrchestrationTaskFailed(ctx, sqlc.MarkOrchestrationTaskFailedParams{
			ID:             taskRow.ID,
			LatestResultID: row.ResultID,
			TerminalReason: normalizeVerificationTerminalReason(input.TerminalReason, verdict),
		})
		if err != nil {
			return nil, fmt.Errorf("mark task failed after verification: %w", err)
		}
		if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
			TaskID:           failedTask.ID,
			AggregateType:    "task",
			AggregateID:      failedTask.ID,
			AggregateVersion: failedTask.StatusVersion,
			Type:             "run.event.task.failed",
			Payload: map[string]any{
				"task_id":          failedTask.ID.String(),
				"previous_status":  taskRow.Status,
				"new_status":       failedTask.Status,
				"latest_result_id": row.ResultID.String(),
				"failure_class":    finalRow.FailureClass,
				"terminal_reason":  failedTask.TerminalReason,
				"failure_reason":   "verification_rejected",
			},
		}); err != nil {
			return nil, err
		}
		if err := s.propagateVerificationFailureToDependents(ctx, tx, failedTask, finalRow); err != nil {
			return nil, err
		}
		if err := s.markRunFailedFromVerificationFailure(ctx, tx, runRow, failedTask, finalRow); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit complete verification tx: %w", err)
	}
	verification := toTaskVerification(finalRow)
	return &verification, nil
}

func (s *Service) RecoverExpiredVerifications(ctx context.Context) (int, error) {
	expired, err := s.queries.ListExpiredOrchestrationTaskVerifications(ctx)
	if err != nil {
		return 0, fmt.Errorf("list expired verifications: %w", err)
	}
	recovered := 0
	for _, candidate := range expired {
		tx, err := s.store.BeginTx(ctx)
		if err != nil {
			return recovered, fmt.Errorf("begin verification recovery tx: %w", err)
		}
		row, err := tx.GetOrchestrationTaskVerificationByIDForUpdate(ctx, candidate.ID)
		if err != nil {
			_ = tx.Rollback(ctx)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return recovered, fmt.Errorf("lock expired verification: %w", err)
		}
		observedAt, err := tx.DatabaseNow(ctx)
		if err != nil {
			_ = tx.Rollback(ctx)
			return recovered, err
		}
		if isTerminalVerificationStatus(row.Status) || !leaseExpiredAt(row.LeaseExpiresAt, observedAt) {
			_ = tx.Rollback(ctx)
			continue
		}

		runRow, err := tx.GetOrchestrationRunByIDForUpdate(ctx, row.RunID)
		if err != nil {
			_ = tx.Rollback(ctx)
			return recovered, fmt.Errorf("lock run for expired verification: %w", err)
		}
		taskRow, err := tx.GetOrchestrationTaskByIDForUpdate(ctx, row.TaskID)
		if err != nil {
			_ = tx.Rollback(ctx)
			return recovered, fmt.Errorf("lock task for expired verification: %w", err)
		}

		if row.Status == TaskVerificationStatusClaimed {
			if runRow.LifecycleStatus == LifecycleStatusCancelling {
				lostRow, err := tx.MarkOrchestrationTaskVerificationLost(ctx, sqlc.MarkOrchestrationTaskVerificationLostParams{
					ID:             row.ID,
					ClaimEpoch:     row.ClaimEpoch,
					Verdict:        VerificationVerdictRejected,
					Summary:        "run cancelled",
					FailureClass:   "run_cancelled",
					TerminalReason: "run cancelled",
				})
				if err != nil {
					_ = tx.Rollback(ctx)
					return recovered, fmt.Errorf("mark claimed verification lost during cancellation: %w", err)
				}
				if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
					TaskID:           lostRow.TaskID,
					AggregateType:    "verification",
					AggregateID:      lostRow.ID,
					AggregateVersion: lostRow.ClaimEpoch,
					Type:             "run.event.verification.lost",
					Payload: map[string]any{
						"verification_id": lostRow.ID.String(),
						"task_id":         lostRow.TaskID.String(),
						"previous_status": row.Status,
						"new_status":      lostRow.Status,
						"terminal_reason": lostRow.TerminalReason,
					},
				}); err != nil {
					_ = tx.Rollback(ctx)
					return recovered, err
				}
				cancelledTask, err := tx.MarkOrchestrationTaskCancelled(ctx, sqlc.MarkOrchestrationTaskCancelledParams{
					ID:             taskRow.ID,
					TerminalReason: "run cancelled",
				})
				if err != nil {
					_ = tx.Rollback(ctx)
					return recovered, fmt.Errorf("cancel task after claimed verification cancellation: %w", err)
				}
				if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
					TaskID:           cancelledTask.ID,
					AggregateType:    "task",
					AggregateID:      cancelledTask.ID,
					AggregateVersion: cancelledTask.StatusVersion,
					Type:             "run.event.task.cancelled",
					Payload: map[string]any{
						"task_id":         cancelledTask.ID.String(),
						"previous_status": taskRow.Status,
						"new_status":      cancelledTask.Status,
						"terminal_reason": cancelledTask.TerminalReason,
					},
				}); err != nil {
					_ = tx.Rollback(ctx)
					return recovered, err
				}
				if _, _, err := s.maybeMarkRunCompletedAfterTaskTransition(ctx, tx, runRow, cancelledTask.ID, pgtype.UUID{}); err != nil {
					_ = tx.Rollback(ctx)
					return recovered, err
				}
				if err := tx.Commit(ctx); err != nil {
					return recovered, fmt.Errorf("commit claimed verification cancellation recovery tx: %w", err)
				}
				recovered++
				continue
			}
			if runRow.LifecycleStatus == LifecycleStatusFailed {
				lostRow, err := tx.MarkOrchestrationTaskVerificationLost(ctx, sqlc.MarkOrchestrationTaskVerificationLostParams{
					ID:             row.ID,
					ClaimEpoch:     row.ClaimEpoch,
					Verdict:        VerificationVerdictRejected,
					Summary:        "run already failed",
					FailureClass:   "run_already_failed",
					TerminalReason: "run already failed",
				})
				if err != nil {
					_ = tx.Rollback(ctx)
					return recovered, fmt.Errorf("mark claimed verification lost after run failure: %w", err)
				}
				if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
					TaskID:           lostRow.TaskID,
					AggregateType:    "verification",
					AggregateID:      lostRow.ID,
					AggregateVersion: lostRow.ClaimEpoch,
					Type:             "run.event.verification.lost",
					Payload: map[string]any{
						"verification_id": lostRow.ID.String(),
						"task_id":         lostRow.TaskID.String(),
						"previous_status": row.Status,
						"new_status":      lostRow.Status,
						"terminal_reason": lostRow.TerminalReason,
					},
				}); err != nil {
					_ = tx.Rollback(ctx)
					return recovered, err
				}
				failedTask, err := tx.MarkOrchestrationTaskFailed(ctx, sqlc.MarkOrchestrationTaskFailedParams{
					ID:             taskRow.ID,
					LatestResultID: row.ResultID,
					TerminalReason: "run already failed",
				})
				if err != nil {
					_ = tx.Rollback(ctx)
					return recovered, fmt.Errorf("fail task after claimed verification on failed run: %w", err)
				}
				if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
					TaskID:           failedTask.ID,
					AggregateType:    "task",
					AggregateID:      failedTask.ID,
					AggregateVersion: failedTask.StatusVersion,
					Type:             "run.event.task.failed",
					Payload: map[string]any{
						"task_id":          failedTask.ID.String(),
						"previous_status":  taskRow.Status,
						"new_status":       failedTask.Status,
						"latest_result_id": row.ResultID.String(),
						"failure_class":    "run_already_failed",
						"terminal_reason":  failedTask.TerminalReason,
						"failure_reason":   "run_already_failed",
					},
				}); err != nil {
					_ = tx.Rollback(ctx)
					return recovered, err
				}
				if err := tx.Commit(ctx); err != nil {
					return recovered, fmt.Errorf("commit failed-run claimed verification recovery tx: %w", err)
				}
				recovered++
				continue
			}
			releasedRow, err := tx.ReleaseOrchestrationTaskVerificationClaim(ctx, sqlc.ReleaseOrchestrationTaskVerificationClaimParams{
				ID:         row.ID,
				ClaimToken: row.ClaimToken,
			})
			if err != nil {
				_ = tx.Rollback(ctx)
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				return recovered, fmt.Errorf("release expired verification claim: %w", err)
			}
			if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
				TaskID:           releasedRow.TaskID,
				AggregateType:    "verification",
				AggregateID:      releasedRow.ID,
				AggregateVersion: releasedRow.ClaimEpoch,
				Type:             "run.event.verification.requeued",
				Payload: map[string]any{
					"verification_id": releasedRow.ID.String(),
					"task_id":         releasedRow.TaskID.String(),
					"previous_status": row.Status,
					"new_status":      releasedRow.Status,
					"terminal_reason": "verification lease expired before execution start",
				},
			}); err != nil {
				_ = tx.Rollback(ctx)
				return recovered, err
			}
			if err := tx.Commit(ctx); err != nil {
				return recovered, fmt.Errorf("commit claimed verification requeue tx: %w", err)
			}
			recovered++
			continue
		}

		lostRow, err := tx.MarkOrchestrationTaskVerificationLost(ctx, sqlc.MarkOrchestrationTaskVerificationLostParams{
			ID:             row.ID,
			ClaimEpoch:     row.ClaimEpoch,
			Verdict:        VerificationVerdictRejected,
			Summary:        "verification lease expired",
			FailureClass:   "lease_expired",
			TerminalReason: "verification lease expired",
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			return recovered, fmt.Errorf("mark verification lost: %w", err)
		}
		if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
			TaskID:           lostRow.TaskID,
			AggregateType:    "verification",
			AggregateID:      lostRow.ID,
			AggregateVersion: lostRow.ClaimEpoch,
			Type:             "run.event.verification.lost",
			Payload: map[string]any{
				"verification_id": lostRow.ID.String(),
				"task_id":         lostRow.TaskID.String(),
				"previous_status": row.Status,
				"new_status":      lostRow.Status,
				"terminal_reason": lostRow.TerminalReason,
			},
		}); err != nil {
			_ = tx.Rollback(ctx)
			return recovered, err
		}

		if taskRow.SupersededByPlannerEpoch.Valid {
			if err := tx.Commit(ctx); err != nil {
				return recovered, fmt.Errorf("commit superseded verification recovery tx: %w", err)
			}
			recovered++
			continue
		}
		if runRow.LifecycleStatus == LifecycleStatusCancelling {
			cancelledTask, err := tx.MarkOrchestrationTaskCancelled(ctx, sqlc.MarkOrchestrationTaskCancelledParams{
				ID:             taskRow.ID,
				TerminalReason: "run cancelled",
			})
			if err != nil {
				_ = tx.Rollback(ctx)
				return recovered, fmt.Errorf("cancel task from lost verification during cancellation: %w", err)
			}
			if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
				TaskID:           cancelledTask.ID,
				AggregateType:    "task",
				AggregateID:      cancelledTask.ID,
				AggregateVersion: cancelledTask.StatusVersion,
				Type:             "run.event.task.cancelled",
				Payload: map[string]any{
					"task_id":         cancelledTask.ID.String(),
					"previous_status": taskRow.Status,
					"new_status":      cancelledTask.Status,
					"terminal_reason": cancelledTask.TerminalReason,
				},
			}); err != nil {
				_ = tx.Rollback(ctx)
				return recovered, err
			}
			if _, _, err := s.maybeMarkRunCompletedAfterTaskTransition(ctx, tx, runRow, cancelledTask.ID, pgtype.UUID{}); err != nil {
				_ = tx.Rollback(ctx)
				return recovered, err
			}
			if err := tx.Commit(ctx); err != nil {
				return recovered, fmt.Errorf("commit running verification cancellation recovery tx: %w", err)
			}
			recovered++
			continue
		}

		failedTask, err := tx.MarkOrchestrationTaskFailed(ctx, sqlc.MarkOrchestrationTaskFailedParams{
			ID:             taskRow.ID,
			LatestResultID: row.ResultID,
			TerminalReason: "verification lease expired",
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			return recovered, fmt.Errorf("mark task failed from lost verification: %w", err)
		}
		if _, err := s.appendEvent(ctx, tx, row.RunID, eventSpec{
			TaskID:           failedTask.ID,
			AggregateType:    "task",
			AggregateID:      failedTask.ID,
			AggregateVersion: failedTask.StatusVersion,
			Type:             "run.event.task.failed",
			Payload: map[string]any{
				"task_id":          failedTask.ID.String(),
				"previous_status":  taskRow.Status,
				"new_status":       failedTask.Status,
				"latest_result_id": row.ResultID.String(),
				"failure_class":    "lease_expired",
				"terminal_reason":  failedTask.TerminalReason,
				"failure_reason":   "verification_lease_expired",
			},
		}); err != nil {
			_ = tx.Rollback(ctx)
			return recovered, err
		}
		if err := s.propagateVerificationFailureToDependents(ctx, tx, failedTask, lostRow); err != nil {
			_ = tx.Rollback(ctx)
			return recovered, err
		}
		if err := s.markRunFailedFromVerificationFailure(ctx, tx, runRow, failedTask, lostRow); err != nil {
			_ = tx.Rollback(ctx)
			return recovered, err
		}
		if err := tx.Commit(ctx); err != nil {
			return recovered, fmt.Errorf("commit verification recovery tx: %w", err)
		}
		recovered++
	}
	return recovered, nil
}

func (s *Service) RunVerificationRecoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := s.RecoverExpiredVerifications(ctx)
			if err != nil {
				s.logger.Error("verification recovery loop failed", slog.Any("error", err))
				continue
			}
			if count > 0 {
				s.logger.Info("recovered expired orchestration verifications", slog.Int("count", count))
			}
		}
	}
}

func RunClaimedVerification(ctx context.Context, runtime VerificationLeaseRuntime, log *slog.Logger, verification TaskVerification, leaseTTLSeconds int, verifierProfiles []string, execute VerificationRunner) bool {
	return RunClaimedVerificationWithInterval(ctx, runtime, log, verification, leaseTTLSeconds, heartbeatInterval(leaseTTLSeconds), verifierProfiles, execute)
}

func RunClaimedVerificationWithInterval(ctx context.Context, runtime VerificationLeaseRuntime, log *slog.Logger, verification TaskVerification, leaseTTLSeconds int, heartbeatEvery time.Duration, verifierProfiles []string, execute VerificationRunner) bool {
	execCtx, cancelExec := context.WithCancel(ctx)
	defer cancelExec()
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()

	verificationHeartbeatDone := make(chan bool, 1)
	go runVerificationHeartbeatLoopWithInterval(heartbeatCtx, cancelExec, runtime, log, verification, leaseTTLSeconds, heartbeatEvery, verificationHeartbeatDone)

	completion := execute(execCtx, verification, verifierProfiles)
	heartbeatResultRead := false
	checkHeartbeat := func(block bool) (bool, bool) {
		if heartbeatResultRead {
			return true, false
		}
		if block {
			leaseLost := <-verificationHeartbeatDone
			heartbeatResultRead = true
			return true, leaseLost
		}
		select {
		case leaseLost := <-verificationHeartbeatDone:
			heartbeatResultRead = true
			return true, leaseLost
		default:
			return false, false
		}
	}

	if execCtx.Err() != nil {
		_, leaseLost := checkHeartbeat(true)
		if leaseLost {
			return true
		}
		if ctx.Err() != nil {
			completion = workerShutdownVerificationCompletion(verification)
		} else {
			return false
		}
	}

	for {
		if done, leaseLost := checkHeartbeat(false); done && leaseLost {
			return true
		}

		if ctx.Err() != nil && completion.Status == TaskVerificationStatusCompleted {
			completion = workerShutdownVerificationCompletion(verification)
		}

		completeCtx, cancelComplete := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_, completeErr := runtime.CompleteVerification(completeCtx, completion)
		cancelComplete()
		if completeErr == nil {
			cancelHeartbeat()
			_, leaseLost := checkHeartbeat(true)
			return leaseLost
		}

		log.Error("complete verification failed", slog.String("verification_id", verification.ID), slog.Any("error", completeErr))
		if errors.Is(completeErr, ErrVerificationLeaseConflict) || errors.Is(completeErr, ErrVerificationImmutable) {
			cancelHeartbeat()
			if done, leaseLost := checkHeartbeat(true); done {
				return leaseLost || errors.Is(completeErr, ErrVerificationLeaseConflict)
			}
			return errors.Is(completeErr, ErrVerificationLeaseConflict)
		}

		select {
		case leaseLost := <-verificationHeartbeatDone:
			heartbeatResultRead = true
			if leaseLost {
				return true
			}
			cancelHeartbeat()
			return false
		case <-ctx.Done():
		case <-time.After(verificationCompletionRetryInterval):
		}
	}
}

func workerShutdownVerificationCompletion(verification TaskVerification) VerificationCompletion {
	return VerificationCompletion{
		VerificationID: verification.ID,
		ClaimToken:     verification.ClaimToken,
		Status:         TaskVerificationStatusFailed,
		Verdict:        VerificationVerdictRejected,
		Summary:        "worker shutdown interrupted verification",
		FailureClass:   "worker_shutdown",
		TerminalReason: "worker shutdown interrupted verification",
		RequestReplan:  false,
	}
}

func runVerificationHeartbeatLoopWithInterval(ctx context.Context, cancel context.CancelFunc, runtime VerificationLeaseRuntime, log *slog.Logger, verification TaskVerification, leaseTTLSeconds int, interval time.Duration, done chan<- bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			done <- false
			return
		case <-ticker.C:
			if _, err := runtime.HeartbeatVerification(ctx, VerificationHeartbeat{
				VerificationID:  verification.ID,
				ClaimToken:      verification.ClaimToken,
				LeaseTTLSeconds: leaseTTLSeconds,
			}); err != nil {
				log.Warn("verification heartbeat failed", slog.String("verification_id", verification.ID), slog.Any("error", err))
				if errors.Is(err, ErrVerificationLeaseConflict) {
					cancel()
					done <- true
					return
				}
				if errors.Is(err, ErrVerificationImmutable) {
					cancel()
					done <- false
					return
				}
				consecutiveFailures++
				if consecutiveFailures >= 3 {
					log.Error("verification lease renewal failed repeatedly; cancelling execution", slog.String("verification_id", verification.ID))
					cancel()
					done <- true
					return
				}
				continue
			}
			consecutiveFailures = 0
		}
	}
}

func heartbeatInterval(leaseTTLSeconds int) time.Duration {
	ttl := time.Duration(leaseTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = TaskVerificationDefaultLeaseTTL
	}
	interval := ttl / 3
	if interval <= 0 {
		return time.Second
	}
	return interval
}

func normalizeVerificationCompletionStatus(raw string) string {
	switch strings.TrimSpace(raw) {
	case "", TaskVerificationStatusCompleted:
		return TaskVerificationStatusCompleted
	case TaskVerificationStatusFailed:
		return TaskVerificationStatusFailed
	default:
		return ""
	}
}

func normalizeVerificationVerdict(raw string) string {
	switch strings.TrimSpace(raw) {
	case VerificationVerdictAccepted:
		return VerificationVerdictAccepted
	case "", VerificationVerdictRejected:
		return VerificationVerdictRejected
	default:
		return ""
	}
}

func normalizeVerificationTerminalReason(raw, verdict string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		return trimmed
	}
	if verdict == VerificationVerdictAccepted {
		return ""
	}
	return "verification rejected"
}

func isTerminalVerificationStatus(status string) bool {
	switch status {
	case TaskVerificationStatusCompleted, TaskVerificationStatusFailed, TaskVerificationStatusLost:
		return true
	default:
		return false
	}
}

func toTaskVerification(row sqlc.OrchestrationTaskVerification) TaskVerification {
	claimEpoch, _ := uint64FromInt64(row.ClaimEpoch, "verification.claim_epoch")
	return TaskVerification{
		ID:              row.ID.String(),
		RunID:           row.RunID.String(),
		TaskID:          row.TaskID.String(),
		ResultID:        row.ResultID.String(),
		SessionID:       uuidToString(row.SessionID),
		AttemptNo:       int(row.AttemptNo),
		WorkerID:        row.WorkerID,
		ExecutorID:      row.ExecutorID,
		VerifierProfile: row.VerifierProfile,
		Status:          row.Status,
		ClaimEpoch:      claimEpoch,
		ClaimToken:      row.ClaimToken,
		LeaseExpiresAt:  optionalTime(db.TimeFromPg(row.LeaseExpiresAt)),
		LastHeartbeatAt: optionalTime(db.TimeFromPg(row.LastHeartbeatAt)),
		Verdict:         row.Verdict,
		Summary:         row.Summary,
		FailureClass:    row.FailureClass,
		TerminalReason:  row.TerminalReason,
		StartedAt:       optionalTime(db.TimeFromPg(row.StartedAt)),
		FinishedAt:      optionalTime(db.TimeFromPg(row.FinishedAt)),
		CreatedAt:       db.TimeFromPg(row.CreatedAt),
		UpdatedAt:       db.TimeFromPg(row.UpdatedAt),
	}
}

func verifierCapabilities(verifierProfiles []string) map[string]any {
	return profileCapabilities("verifier_profiles", verifierProfiles)
}

func requiresTaskVerification(policy map[string]any) bool {
	return len(normalizeObject(policy)) > 0
}

func verifierProfileForTaskPolicy(policy map[string]any) string {
	if profile := strings.TrimSpace(stringValue(policy["verifier_profile"])); profile != "" {
		return profile
	}
	if strings.TrimSpace(stringValue(policy["mode"])) == VerificationModeBuiltinBasic {
		return BuiltinBasicVerifierProfile
	}
	return DefaultVerifierProfile
}

func isRetryableVerificationFailureClass(failureClass string) bool {
	switch strings.TrimSpace(failureClass) {
	case "worker_shutdown", "lease_expired", "verifier_generate_failed", "verification_context_load_failed", "verification_session_create_failed", "verifier_model_resolution_failed":
		return true
	default:
		return false
	}
}

func (s *Service) failTaskFromVerification(ctx context.Context, tx Queries, taskRow sqlc.OrchestrationTask, resultID pgtype.UUID, verificationRow sqlc.OrchestrationTaskVerification, failureReason string) (sqlc.OrchestrationTask, error) {
	failedTask, err := tx.MarkOrchestrationTaskFailed(ctx, sqlc.MarkOrchestrationTaskFailedParams{
		ID:             taskRow.ID,
		LatestResultID: resultID,
		TerminalReason: normalizeVerificationTerminalReason(verificationRow.TerminalReason, verificationRow.Verdict),
	})
	if err != nil {
		return sqlc.OrchestrationTask{}, fmt.Errorf("mark task failed after verification: %w", err)
	}
	if _, err := s.appendEvent(ctx, tx, taskRow.RunID, eventSpec{
		TaskID:           failedTask.ID,
		AggregateType:    "task",
		AggregateID:      failedTask.ID,
		AggregateVersion: failedTask.StatusVersion,
		Type:             "run.event.task.failed",
		Payload: map[string]any{
			"task_id":          failedTask.ID.String(),
			"previous_status":  taskRow.Status,
			"new_status":       failedTask.Status,
			"latest_result_id": resultID.String(),
			"failure_class":    verificationRow.FailureClass,
			"terminal_reason":  failedTask.TerminalReason,
			"failure_reason":   strings.TrimSpace(failureReason),
		},
	}); err != nil {
		return sqlc.OrchestrationTask{}, err
	}
	return failedTask, nil
}

func (s *Service) propagateVerificationFailureToDependents(ctx context.Context, tx Queries, failedTask sqlc.OrchestrationTask, verificationRow sqlc.OrchestrationTaskVerification) error {
	return s.propagateDependencyFailure(ctx, tx, failedTask, taskFailureCause{VerificationID: verificationRow.ID})
}

func (s *Service) markRunFailedFromVerificationFailure(ctx context.Context, tx Queries, runRow sqlc.OrchestrationRun, taskRow sqlc.OrchestrationTask, verificationRow sqlc.OrchestrationTaskVerification) error {
	return s.markRunFailedAfterTaskFailure(ctx, tx, runRow, taskRow, taskFailureCause{VerificationID: verificationRow.ID})
}
