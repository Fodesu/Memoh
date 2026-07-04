-- user_input_tool_calls
-- Production query: db/postgres/queries/user_input.sql ListUserInputsBySessionToolCalls
SELECT *
FROM user_input_requests
WHERE bot_id = $1
  AND session_id = $2
  AND tool_call_id = ANY($3::text[])
ORDER BY created_at ASC, short_id ASC;
