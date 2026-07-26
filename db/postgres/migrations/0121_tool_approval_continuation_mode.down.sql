-- 0121_tool_approval_continuation_mode
-- Remove the persisted approval continuation mode.

ALTER TABLE public.tool_approval_requests
  DROP CONSTRAINT IF EXISTS tool_approval_continuation_mode_check;

ALTER TABLE public.tool_approval_requests
  DROP COLUMN IF EXISTS continuation_mode;
