-- 0081_add_orchestration_kernel (down)
-- Drop the final orchestration kernel tables.

ALTER TABLE IF EXISTS orchestration_runs
  DROP CONSTRAINT IF EXISTS orchestration_runs_root_task_fk;

ALTER TABLE IF EXISTS orchestration_task_results
  DROP CONSTRAINT IF EXISTS orchestration_task_results_attempt_fk,
  DROP CONSTRAINT IF EXISTS orchestration_task_results_task_run_fk;

ALTER TABLE IF EXISTS orchestration_artifacts
  DROP CONSTRAINT IF EXISTS orchestration_artifacts_attempt_fk,
  DROP CONSTRAINT IF EXISTS orchestration_artifacts_task_run_fk;

ALTER TABLE IF EXISTS orchestration_planning_intents
  DROP CONSTRAINT IF EXISTS orchestration_planning_intents_checkpoint_run_fk,
  DROP CONSTRAINT IF EXISTS orchestration_planning_intents_task_run_fk;

ALTER TABLE IF EXISTS orchestration_task_dependencies
  DROP CONSTRAINT IF EXISTS orchestration_task_dependencies_successor_fk,
  DROP CONSTRAINT IF EXISTS orchestration_task_dependencies_predecessor_fk;

ALTER TABLE IF EXISTS orchestration_task_attempts
  DROP CONSTRAINT IF EXISTS orchestration_task_attempts_checkpoint_fk,
  DROP CONSTRAINT IF EXISTS orchestration_task_attempts_manifest_fk,
  DROP CONSTRAINT IF EXISTS orchestration_task_attempts_task_run_fk;

ALTER TABLE IF EXISTS orchestration_input_manifests
  DROP CONSTRAINT IF EXISTS orchestration_input_manifests_task_run_fk;

ALTER TABLE IF EXISTS orchestration_human_checkpoints
  DROP CONSTRAINT IF EXISTS orchestration_human_checkpoints_task_run_fk;

ALTER TABLE IF EXISTS orchestration_tasks
  DROP CONSTRAINT IF EXISTS orchestration_tasks_latest_result_fk,
  DROP CONSTRAINT IF EXISTS orchestration_tasks_waiting_checkpoint_fk;

DROP TABLE IF EXISTS orchestration_workers;
DROP TABLE IF EXISTS orchestration_idempotency_records;
DROP TABLE IF EXISTS orchestration_projection_snapshots;
DROP TABLE IF EXISTS orchestration_events;
DROP TABLE IF EXISTS orchestration_planning_intents;
DROP TABLE IF EXISTS orchestration_task_dependencies;
DROP TABLE IF EXISTS orchestration_artifacts;
DROP TABLE IF EXISTS orchestration_task_attempts;
DROP TABLE IF EXISTS orchestration_input_manifests;
DROP TABLE IF EXISTS orchestration_task_results;
DROP TABLE IF EXISTS orchestration_human_checkpoints;
DROP TABLE IF EXISTS orchestration_tasks;
DROP TABLE IF EXISTS orchestration_runs;
