-- user_input_short_id
-- Production query: db/postgres/queries/user_input.sql GetPendingUserInputBySessionShortID
SELECT *
FROM user_input_requests
WHERE bot_id = $1
  AND session_id = $2
  AND short_id = $3
  AND status = 'pending'
  AND (expires_at IS NULL OR expires_at > now());
