-- user_input_reply_message
-- Production query: db/postgres/queries/user_input.sql GetPendingUserInputByReplyMessage
SELECT *
FROM user_input_requests
WHERE bot_id = $1
  AND session_id = $2
  AND prompt_external_message_id = $3
  AND status = 'pending'
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at DESC
LIMIT 1;
