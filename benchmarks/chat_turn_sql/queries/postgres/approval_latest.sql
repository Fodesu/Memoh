-- approval_latest
-- Production query: db/postgres/queries/tool_approval.sql GetLatestPendingToolApprovalBySession
SELECT *
FROM tool_approval_requests
WHERE bot_id = $1
  AND session_id = $2
  AND status = 'pending'
ORDER BY created_at DESC, short_id DESC
LIMIT 1;
