-- 0082_add_orchestration_env_runtime
-- Remove orchestration env runtime tables, env preconditions, action ledger env references, and container image catalog.

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

ALTER TABLE orchestration_input_manifests
  DROP COLUMN IF EXISTS captured_env_preconditions;

ALTER TABLE orchestration_tasks
  DROP COLUMN IF EXISTS env_preconditions;

DROP INDEX IF EXISTS idx_orchestration_container_images_tenant_status;
DROP TABLE IF EXISTS orchestration_container_images;

DROP INDEX IF EXISTS idx_orchestration_env_snapshots_run_kind;
DROP INDEX IF EXISTS idx_orchestration_env_snapshots_attempt;
DROP INDEX IF EXISTS idx_orchestration_env_snapshots_session;
DROP TABLE IF EXISTS orchestration_env_snapshots;

DROP INDEX IF EXISTS idx_orchestration_env_bindings_active_session_unique;
DROP INDEX IF EXISTS idx_orchestration_env_bindings_session;
DROP INDEX IF EXISTS idx_orchestration_env_bindings_task_attempt;
DROP INDEX IF EXISTS idx_orchestration_env_bindings_run;
DROP TABLE IF EXISTS orchestration_env_bindings;

DROP INDEX IF EXISTS idx_orchestration_env_lease_reservations_attempt;
DROP INDEX IF EXISTS idx_orchestration_env_lease_reservations_tenant;
DROP INDEX IF EXISTS idx_orchestration_env_lease_reservations_queue;
DROP TABLE IF EXISTS orchestration_env_lease_reservations;

DROP INDEX IF EXISTS idx_orchestration_env_sessions_attempt;
DROP INDEX IF EXISTS idx_orchestration_env_sessions_lease_expiry;
DROP INDEX IF EXISTS idx_orchestration_env_sessions_tenant_status;
DROP INDEX IF EXISTS idx_orchestration_env_sessions_resource_status;
DROP TABLE IF EXISTS orchestration_env_sessions;

DROP INDEX IF EXISTS idx_orchestration_env_resources_tenant_kind;
DROP TABLE IF EXISTS orchestration_env_resources;
