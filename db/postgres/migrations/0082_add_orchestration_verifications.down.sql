-- 0082_add_orchestration_verifications (down)
-- Drop orchestration verifier work queue, worker lease snapshots, and run-level checkpoint fencing.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'orchestration_tasks') AND EXISTS (
    SELECT 1
    FROM orchestration_tasks
    WHERE status = 'verifying'
  ) THEN
    RAISE EXCEPTION 'cannot roll back 0082 while orchestration_tasks.status contains verifying rows';
  END IF;
END $$;

DROP TABLE IF EXISTS orchestration_task_verifications;

DROP INDEX IF EXISTS idx_orchestration_human_checkpoints_open_run_barrier_unique;

ALTER TABLE orchestration_task_attempts
  DROP COLUMN IF EXISTS worker_lease_token;

ALTER TABLE orchestration_tasks
  DROP CONSTRAINT IF EXISTS orchestration_tasks_status_check;

ALTER TABLE orchestration_tasks
  ADD CONSTRAINT orchestration_tasks_status_check
  CHECK (status IN ('created', 'ready', 'dispatching', 'running', 'waiting_human', 'verifying', 'completed', 'blocked', 'failed', 'cancelled'));
