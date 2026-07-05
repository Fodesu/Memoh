-- external_lookup
-- Production query: db/postgres/queries/messages.sql GetVisibleMessageCursorByExternalIDBySession
SELECT
  m.id,
  m.turn_position,
  m.turn_message_seq,
  m.created_at
FROM bot_history_messages m
WHERE m.session_id = $1
  AND m.turn_visible = true
  AND m.turn_id IS NOT NULL
  AND m.turn_position IS NOT NULL
  AND m.turn_message_seq IS NOT NULL
  AND m.source_message_id = $2
ORDER BY m.turn_position DESC, m.turn_message_seq DESC, m.created_at DESC, m.id DESC
LIMIT 1;
