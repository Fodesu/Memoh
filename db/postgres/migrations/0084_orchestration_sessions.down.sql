-- 0084_orchestration_sessions
-- Remove bot session transcript links from orchestration execution records.

DROP INDEX IF EXISTS idx_orchestration_task_verifications_session_id;
DROP INDEX IF EXISTS idx_orchestration_task_attempts_session_id;

ALTER TABLE orchestration_task_verifications
  DROP COLUMN IF EXISTS session_id;

ALTER TABLE orchestration_task_attempts
  DROP COLUMN IF EXISTS session_id;

ALTER TABLE bot_sessions
  DROP COLUMN IF EXISTS finalized_at;

ALTER TABLE bot_sessions
  DROP CONSTRAINT IF EXISTS bot_sessions_type_check;

ALTER TABLE bot_sessions
  ADD CONSTRAINT bot_sessions_type_check
  CHECK (type IN ('chat', 'heartbeat', 'schedule', 'subagent', 'discuss'));
