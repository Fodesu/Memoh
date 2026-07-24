-- 0120_tool_approval_resume_policy
-- Remove the persisted approval resume policy.

ALTER TABLE public.tool_approval_requests
  DROP CONSTRAINT IF EXISTS tool_approval_resume_policy_check;

ALTER TABLE public.tool_approval_requests
  DROP COLUMN IF EXISTS resume_policy;
