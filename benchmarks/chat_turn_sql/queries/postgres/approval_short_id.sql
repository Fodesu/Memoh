-- approval_short_id
-- Production query: db/postgres/queries/tool_approval.sql GetPendingToolApprovalBySessionShortID
SELECT *
FROM tool_approval_requests
WHERE bot_id = $1
  AND session_id = $2
  AND short_id = $3
  AND status = 'pending';
