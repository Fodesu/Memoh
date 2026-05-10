-- 0089_add_orchestration_side_effect_approval_tokens
-- Add fenced approval tokens for irreversible orchestration side effects.

CREATE TABLE IF NOT EXISTS orchestration_side_effect_approval_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id UUID NOT NULL REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id UUID NOT NULL REFERENCES orchestration_task_attempts(id) ON DELETE CASCADE,
  claim_epoch BIGINT NOT NULL CHECK (claim_epoch > 0),
  env_session_id UUID REFERENCES orchestration_env_sessions(id) ON DELETE SET NULL,
  env_lease_epoch BIGINT NOT NULL DEFAULT 0 CHECK (env_lease_epoch >= 0),
  effect_class TEXT NOT NULL DEFAULT 'external_irreversible' CHECK (effect_class IN ('external_irreversible')),
  token_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'consumed', 'revoked', 'expired')),
  approved_by TEXT NOT NULL DEFAULT '',
  approval_reason TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  consumed_action_id UUID REFERENCES orchestration_action_ledger(id) ON DELETE SET NULL,
  expires_at TIMESTAMPTZ,
  consumed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT orchestration_side_effect_tokens_task_run_fk
    FOREIGN KEY (task_id, run_id) REFERENCES orchestration_tasks(id, run_id) ON DELETE CASCADE,
  CONSTRAINT orchestration_side_effect_tokens_attempt_fk
    FOREIGN KEY (attempt_id, run_id, task_id) REFERENCES orchestration_task_attempts(id, run_id, task_id) ON DELETE CASCADE,
  CONSTRAINT orchestration_side_effect_tokens_hash_unique UNIQUE (token_hash)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_side_effect_tokens_attempt
  ON orchestration_side_effect_approval_tokens(attempt_id, status, created_at, id);

CREATE INDEX IF NOT EXISTS idx_orchestration_side_effect_tokens_env
  ON orchestration_side_effect_approval_tokens(env_session_id, env_lease_epoch, status, id)
  WHERE env_session_id IS NOT NULL;
