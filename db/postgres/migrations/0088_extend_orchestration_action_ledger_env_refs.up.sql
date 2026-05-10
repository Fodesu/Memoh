-- 0088_extend_orchestration_action_ledger_env_refs
-- Classify action effects and link env actions to sessions and snapshots.

ALTER TABLE orchestration_action_ledger
  ADD COLUMN IF NOT EXISTS effect_class TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS env_session_id UUID,
  ADD COLUMN IF NOT EXISTS env_binding_id UUID,
  ADD COLUMN IF NOT EXISTS before_env_snapshot_id UUID,
  ADD COLUMN IF NOT EXISTS after_env_snapshot_id UUID;

ALTER TABLE orchestration_action_ledger
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_action_kind_check,
  ADD CONSTRAINT orchestration_action_ledger_action_kind_check CHECK (
    action_kind IN ('tool_call', 'env_acquire', 'env_release', 'env_hold', 'env_snapshot')
  );

ALTER TABLE orchestration_action_ledger
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_effect_class_check,
  ADD CONSTRAINT orchestration_action_ledger_effect_class_check CHECK (
    effect_class IN ('', 'env_local_read', 'env_local_mutation', 'external_read', 'external_write', 'external_irreversible')
  );

DROP INDEX IF EXISTS idx_orchestration_action_ledger_attempt_tool_call_unique;
DROP INDEX IF EXISTS idx_orchestration_action_ledger_verification_tool_call_unique;

CREATE UNIQUE INDEX IF NOT EXISTS idx_orchestration_action_ledger_attempt_tool_call_unique
  ON orchestration_action_ledger(attempt_id, tool_call_id)
  WHERE attempt_id IS NOT NULL AND action_kind = 'tool_call';

CREATE UNIQUE INDEX IF NOT EXISTS idx_orchestration_action_ledger_verification_tool_call_unique
  ON orchestration_action_ledger(verification_id, tool_call_id)
  WHERE verification_id IS NOT NULL AND action_kind = 'tool_call';

CREATE INDEX IF NOT EXISTS idx_orchestration_action_ledger_env_session
  ON orchestration_action_ledger(env_session_id, started_at, id)
  WHERE env_session_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_orchestration_action_ledger_effect
  ON orchestration_action_ledger(run_id, effect_class, started_at, id)
  WHERE effect_class <> '';

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orchestration_action_ledger_env_session_fk') THEN
    ALTER TABLE orchestration_action_ledger
      ADD CONSTRAINT orchestration_action_ledger_env_session_fk
      FOREIGN KEY (env_session_id) REFERENCES orchestration_env_sessions(id) ON DELETE SET NULL;
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orchestration_action_ledger_env_binding_fk') THEN
    ALTER TABLE orchestration_action_ledger
      ADD CONSTRAINT orchestration_action_ledger_env_binding_fk
      FOREIGN KEY (env_binding_id) REFERENCES orchestration_env_bindings(id) ON DELETE SET NULL;
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orchestration_action_ledger_before_env_snapshot_fk') THEN
    ALTER TABLE orchestration_action_ledger
      ADD CONSTRAINT orchestration_action_ledger_before_env_snapshot_fk
      FOREIGN KEY (before_env_snapshot_id) REFERENCES orchestration_env_snapshots(id) ON DELETE SET NULL;
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orchestration_action_ledger_after_env_snapshot_fk') THEN
    ALTER TABLE orchestration_action_ledger
      ADD CONSTRAINT orchestration_action_ledger_after_env_snapshot_fk
      FOREIGN KEY (after_env_snapshot_id) REFERENCES orchestration_env_snapshots(id) ON DELETE SET NULL;
  END IF;
END $$;
