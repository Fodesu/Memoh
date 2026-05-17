-- 0006_add_orchestration_runtime
-- Remove SQLite orchestration runtime schemas.

PRAGMA foreign_keys = OFF;

DROP TABLE IF EXISTS orchestration_side_effect_approval_tokens;
DROP TABLE IF EXISTS orchestration_action_ledger;
DROP TABLE IF EXISTS orchestration_task_verifications;
DROP TABLE IF EXISTS orchestration_env_snapshots;
DROP TABLE IF EXISTS orchestration_env_bindings;
DROP TABLE IF EXISTS orchestration_env_lease_reservations;
DROP TABLE IF EXISTS orchestration_env_sessions;
DROP TABLE IF EXISTS orchestration_env_resources;
DROP TABLE IF EXISTS orchestration_container_images;
DROP TABLE IF EXISTS orchestration_workers;
DROP TABLE IF EXISTS orchestration_idempotency_records;
DROP TABLE IF EXISTS orchestration_projection_snapshots;
DROP TABLE IF EXISTS orchestration_events;
DROP TABLE IF EXISTS orchestration_task_attempts;
DROP TABLE IF EXISTS orchestration_task_dependencies;
DROP TABLE IF EXISTS orchestration_intents;
DROP TABLE IF EXISTS orchestration_human_checkpoints;
DROP TABLE IF EXISTS orchestration_artifacts;
DROP TABLE IF EXISTS orchestration_task_results;
DROP TABLE IF EXISTS orchestration_input_manifests;
DROP TABLE IF EXISTS orchestration_tasks;
DROP TABLE IF EXISTS orchestration_runs;

PRAGMA foreign_keys = ON;
