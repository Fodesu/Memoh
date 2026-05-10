-- 0088_extend_orchestration_action_ledger_env_refs
-- Remove env references and effect classification from the action ledger.

DELETE FROM orchestration_action_ledger
WHERE action_kind <> 'tool_call';

ALTER TABLE orchestration_action_ledger
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_after_env_snapshot_fk,
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_before_env_snapshot_fk,
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_env_binding_fk,
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_env_session_fk,
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_effect_class_check,
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_action_kind_check;

DROP INDEX IF EXISTS idx_orchestration_action_ledger_effect;
DROP INDEX IF EXISTS idx_orchestration_action_ledger_env_session;
DROP INDEX IF EXISTS idx_orchestration_action_ledger_attempt_tool_call_unique;
DROP INDEX IF EXISTS idx_orchestration_action_ledger_verification_tool_call_unique;

ALTER TABLE orchestration_action_ledger
  DROP COLUMN IF EXISTS after_env_snapshot_id,
  DROP COLUMN IF EXISTS before_env_snapshot_id,
  DROP COLUMN IF EXISTS env_binding_id,
  DROP COLUMN IF EXISTS env_session_id,
  DROP COLUMN IF EXISTS effect_class;

ALTER TABLE orchestration_action_ledger
  ADD CONSTRAINT orchestration_action_ledger_action_kind_check CHECK (action_kind IN ('tool_call'));

CREATE UNIQUE INDEX IF NOT EXISTS idx_orchestration_action_ledger_attempt_tool_call_unique
  ON orchestration_action_ledger(attempt_id, tool_call_id)
  WHERE attempt_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_orchestration_action_ledger_verification_tool_call_unique
  ON orchestration_action_ledger(verification_id, tool_call_id)
  WHERE verification_id IS NOT NULL;
