-- 0081_add_orchestration_core
-- Drop the orchestration core schema, verifier queue, action ledger, leases, and event outbox index.

DROP TABLE IF EXISTS orchestration_action_ledger;
DROP TABLE IF EXISTS orchestration_task_verifications;
DROP INDEX IF EXISTS idx_orchestration_events_unpublished;
DROP INDEX IF EXISTS idx_orchestration_human_checkpoints_open_run_barrier_unique;

DROP INDEX IF EXISTS idx_orchestration_human_checkpoints_open_timeout;
DROP TABLE IF EXISTS orchestration_workers;
DROP TABLE IF EXISTS orchestration_idempotency_records;
DROP TABLE IF EXISTS orchestration_projection_snapshots;
DROP TABLE IF EXISTS orchestration_events;
DROP TABLE IF EXISTS orchestration_intents;
DROP TABLE IF EXISTS orchestration_task_dependencies;
DROP TABLE IF EXISTS orchestration_artifacts;
DROP TABLE IF EXISTS orchestration_task_results CASCADE;
DROP TABLE IF EXISTS orchestration_task_attempts;
DROP TABLE IF EXISTS orchestration_input_manifests;
DROP TABLE IF EXISTS orchestration_human_checkpoints CASCADE;
DROP TABLE IF EXISTS orchestration_tasks CASCADE;
DROP TABLE IF EXISTS orchestration_runs;
