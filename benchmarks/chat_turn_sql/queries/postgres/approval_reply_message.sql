-- approval_reply_message
-- Production query: db/postgres/queries/tool_approval.sql GetPendingToolApprovalByReplyMessage
SELECT *
FROM tool_approval_requests
WHERE bot_id = $1
  AND session_id = $2
  AND prompt_external_message_id = $3
  AND status = 'pending'
ORDER BY created_at DESC
LIMIT 1;
