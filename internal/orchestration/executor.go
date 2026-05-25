package orchestration

import "context"

// AttemptExecutor runs a claimed task attempt. Implementations may use an LLM,
// deterministic fixtures, or a future remote worker, but completion still
// returns through the control-plane attempt lifecycle.
type AttemptExecutor interface {
	ExecuteAttempt(context.Context, TaskAttempt) AttemptCompletion
}

// VerificationExecutor runs a claimed task verification through the same
// control-plane verification lifecycle.
type VerificationExecutor interface {
	ExecuteVerification(context.Context, TaskVerification) VerificationCompletion
}

type attemptExecutorFunc func(context.Context, TaskAttempt) AttemptCompletion

func (fn attemptExecutorFunc) ExecuteAttempt(ctx context.Context, attempt TaskAttempt) AttemptCompletion {
	return fn(ctx, attempt)
}

type verificationExecutorFunc func(context.Context, TaskVerification) VerificationCompletion

func (fn verificationExecutorFunc) ExecuteVerification(ctx context.Context, verification TaskVerification) VerificationCompletion {
	return fn(ctx, verification)
}

// SetAttemptExecutor installs the in-process task executor used by the runtime
// loop after attempts are dispatched.
func (s *Service) SetAttemptExecutor(executor AttemptExecutor) {
	if s == nil {
		return
	}
	s.attemptExecutor = executor
}

// SetAttemptExecutorFunc is a convenience for tests and simple adapters.
func (s *Service) SetAttemptExecutorFunc(fn func(context.Context, TaskAttempt) AttemptCompletion) {
	if fn == nil {
		s.SetAttemptExecutor(nil)
		return
	}
	s.SetAttemptExecutor(attemptExecutorFunc(fn))
}

// SetVerificationExecutor installs the in-process verifier used by the runtime
// loop after verifications are claimed.
func (s *Service) SetVerificationExecutor(executor VerificationExecutor) {
	if s == nil {
		return
	}
	s.verificationExecutor = executor
}

// SetVerificationExecutorFunc is a convenience for tests and simple adapters.
func (s *Service) SetVerificationExecutorFunc(fn func(context.Context, TaskVerification) VerificationCompletion) {
	if fn == nil {
		s.SetVerificationExecutor(nil)
		return
	}
	s.SetVerificationExecutor(verificationExecutorFunc(fn))
}
