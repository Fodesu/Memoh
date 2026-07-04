-- approval_pending_list
-- Production query: db/postgres/queries/tool_approval.sql ListPendingToolApprovalsBySession
SELECT *
FROM tool_approval_requests
WHERE bot_id = $1
  AND session_id = $2
  AND status = 'pending'
ORDER BY created_at ASC, short_id ASC;
