-- user_input_pending_list
-- Production query: db/postgres/queries/user_input.sql ListPendingUserInputsBySession
SELECT *
FROM user_input_requests
WHERE bot_id = $1
  AND session_id = $2
  AND status = 'pending'
  AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at ASC, short_id ASC;
