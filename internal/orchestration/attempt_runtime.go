package orchestration

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const attemptCompletionRetryInterval = 250 * time.Millisecond

type AttemptLeaseRuntime interface {
	HeartbeatAttempt(context.Context, AttemptHeartbeat) (*TaskAttempt, error)
	CompleteAttempt(context.Context, AttemptCompletion) (*TaskAttempt, error)
}

type AttemptRunner func(context.Context, TaskAttempt, []string) AttemptCompletion

func RunClaimedAttempt(ctx context.Context, runtime AttemptLeaseRuntime, log *slog.Logger, attempt TaskAttempt, leaseTTLSeconds int, workerProfiles []string, execute AttemptRunner) bool {
	return RunClaimedAttemptWithInterval(ctx, runtime, log, attempt, leaseTTLSeconds, heartbeatInterval(leaseTTLSeconds), workerProfiles, execute)
}

func RunClaimedAttemptWithInterval(ctx context.Context, runtime AttemptLeaseRuntime, log *slog.Logger, attempt TaskAttempt, leaseTTLSeconds int, heartbeatEvery time.Duration, workerProfiles []string, execute AttemptRunner) bool {
	execCtx, cancelExec := context.WithCancel(ctx)
	defer cancelExec()
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()

	attemptHeartbeatDone := make(chan bool, 1)
	go runAttemptHeartbeatLoopWithInterval(heartbeatCtx, cancelExec, runtime, log, attempt, leaseTTLSeconds, heartbeatEvery, attemptHeartbeatDone)

	completion := execute(execCtx, attempt, workerProfiles)
	heartbeatResultRead := false
	checkHeartbeat := func(block bool) (bool, bool) {
		if heartbeatResultRead {
			return true, false
		}
		if block {
			leaseLost := <-attemptHeartbeatDone
			heartbeatResultRead = true
			return true, leaseLost
		}
		select {
		case leaseLost := <-attemptHeartbeatDone:
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
			completion = workerShutdownAttemptCompletion(attempt)
		} else {
			return false
		}
	}

	for {
		if done, leaseLost := checkHeartbeat(false); done && leaseLost {
			return true
		}

		if ctx.Err() != nil && completion.Status == TaskAttemptStatusCompleted {
			completion = workerShutdownAttemptCompletion(attempt)
		}

		completeCtx, cancelComplete := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_, completeErr := runtime.CompleteAttempt(completeCtx, completion)
		cancelComplete()
		if completeErr == nil {
			cancelHeartbeat()
			_, leaseLost := checkHeartbeat(true)
			return leaseLost
		}

		log.Error("complete attempt failed", slog.String("attempt_id", attempt.ID), slog.Any("error", completeErr))
		if errors.Is(completeErr, ErrAttemptLeaseConflict) || errors.Is(completeErr, ErrAttemptImmutable) {
			cancelHeartbeat()
			if done, leaseLost := checkHeartbeat(true); done {
				return leaseLost || errors.Is(completeErr, ErrAttemptLeaseConflict)
			}
			return errors.Is(completeErr, ErrAttemptLeaseConflict)
		}

		select {
		case leaseLost := <-attemptHeartbeatDone:
			heartbeatResultRead = true
			if leaseLost {
				return true
			}
			cancelHeartbeat()
			return false
		case <-ctx.Done():
		case <-time.After(attemptCompletionRetryInterval):
		}
	}
}

func workerShutdownAttemptCompletion(attempt TaskAttempt) AttemptCompletion {
	return AttemptCompletion{
		AttemptID:      attempt.ID,
		ClaimToken:     attempt.ClaimToken,
		Status:         TaskAttemptStatusFailed,
		Summary:        "worker shutdown interrupted attempt",
		FailureClass:   "worker_shutdown",
		TerminalReason: "worker shutdown interrupted attempt",
	}
}

func runAttemptHeartbeatLoopWithInterval(ctx context.Context, cancel context.CancelFunc, runtime AttemptLeaseRuntime, log *slog.Logger, attempt TaskAttempt, leaseTTLSeconds int, interval time.Duration, done chan<- bool) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			done <- false
			return
		case <-ticker.C:
			if _, err := runtime.HeartbeatAttempt(ctx, AttemptHeartbeat{
				AttemptID:       attempt.ID,
				ClaimToken:      attempt.ClaimToken,
				LeaseTTLSeconds: leaseTTLSeconds,
			}); err != nil {
				log.Warn("attempt heartbeat failed", slog.String("attempt_id", attempt.ID), slog.Any("error", err))
				if errors.Is(err, ErrAttemptLeaseConflict) {
					cancel()
					done <- true
					return
				}
				if errors.Is(err, ErrAttemptImmutable) {
					cancel()
					done <- false
					return
				}
				consecutiveFailures++
				if consecutiveFailures >= 3 {
					log.Error("attempt lease renewal failed repeatedly; cancelling execution", slog.String("attempt_id", attempt.ID))
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
