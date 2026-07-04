-- user_input_latest
-- Production query: db/postgres/queries/user_input.sql GetLatestPendingUserInputBySession
SELECT *
FROM user_input_requests
WHERE bot_id = $1
  AND session_id = $2
  AND status = 'pending'
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at DESC, short_id DESC
LIMIT 1;
