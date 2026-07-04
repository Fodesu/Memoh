-- approval_tool_calls
-- Production query: db/postgres/queries/tool_approval.sql ListToolApprovalsBySessionToolCalls
SELECT *
FROM tool_approval_requests
WHERE bot_id = $1
  AND session_id = $2
  AND tool_call_id = ANY($3::text[])
ORDER BY created_at ASC, short_id ASC;
