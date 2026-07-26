-- 0121_tool_approval_resume_policy
-- Distinguish durable native continuations from process-local approval waiters.

ALTER TABLE public.tool_approval_requests
  ADD COLUMN IF NOT EXISTS resume_policy TEXT NOT NULL DEFAULT 'unknown';

ALTER TABLE public.tool_approval_requests
  DROP CONSTRAINT IF EXISTS tool_approval_resume_policy_check;

ALTER TABLE public.tool_approval_requests
  ADD CONSTRAINT tool_approval_resume_policy_check
  CHECK (resume_policy IN ('unknown', 'native_continuation', 'live_waiter'));

ALTER TABLE public.tool_approval_requests
  ALTER COLUMN resume_policy SET DEFAULT 'native_continuation';
