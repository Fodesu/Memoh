-- user_input_list
-- Production query: db/postgres/queries/user_input.sql ListUserInputsBySession
SELECT *
FROM user_input_requests
WHERE bot_id = $1
  AND session_id = $2
ORDER BY created_at ASC, short_id ASC;
