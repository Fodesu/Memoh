-- 0121_tool_approval_continuation_mode
-- Distinguish durable continuations from process-local approval waiters.

ALTER TABLE public.tool_approval_requests
  ADD COLUMN IF NOT EXISTS continuation_mode TEXT NOT NULL DEFAULT 'unknown';

ALTER TABLE public.tool_approval_requests
  DROP CONSTRAINT IF EXISTS tool_approval_continuation_mode_check;

ALTER TABLE public.tool_approval_requests
  ADD CONSTRAINT tool_approval_continuation_mode_check
  CHECK (continuation_mode IN ('unknown', 'durable', 'local_waiter'));

ALTER TABLE public.tool_approval_requests
  ALTER COLUMN continuation_mode SET DEFAULT 'durable';
