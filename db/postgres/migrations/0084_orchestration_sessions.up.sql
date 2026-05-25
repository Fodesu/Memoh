-- 0084_orchestration_sessions
-- Attach orchestration execution attempts and verifications to bot session transcripts.

ALTER TABLE bot_sessions
  DROP CONSTRAINT IF EXISTS bot_sessions_type_check;

ALTER TABLE bot_sessions
  ADD CONSTRAINT bot_sessions_type_check
  CHECK (type IN ('chat', 'heartbeat', 'schedule', 'subagent', 'discuss', 'orchestration_attempt', 'orchestration_verification'));

ALTER TABLE bot_sessions
  ADD COLUMN IF NOT EXISTS finalized_at TIMESTAMPTZ;

ALTER TABLE orchestration_task_attempts
  ADD COLUMN IF NOT EXISTS session_id UUID REFERENCES bot_sessions(id) ON DELETE SET NULL;

ALTER TABLE orchestration_task_verifications
  ADD COLUMN IF NOT EXISTS session_id UUID REFERENCES bot_sessions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_orchestration_task_attempts_session_id
  ON orchestration_task_attempts(session_id) WHERE session_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_orchestration_task_verifications_session_id
  ON orchestration_task_verifications(session_id) WHERE session_id IS NOT NULL;
