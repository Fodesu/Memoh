-- approval_list
-- Production query: db/postgres/queries/tool_approval.sql ListToolApprovalsBySession
SELECT *
FROM tool_approval_requests
WHERE bot_id = $1
  AND session_id = $2
ORDER BY created_at ASC, short_id ASC;
