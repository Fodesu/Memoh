-- after_page
-- Production path: GetVisibleMessageCursorByIDBySession + ListMessagesAfterCursorBySession.
-- Template keeps the same external args as after_page for SQL microbench/explain.
SELECT
  m.id,
  m.bot_id,
  m.session_id,
  m.sender_channel_identity_id,
  m.sender_account_user_id AS sender_user_id,
  m.source_message_id AS external_message_id,
  m.source_reply_to_message_id,
  m.role,
  m.content,
  m.metadata,
  m.usage,
  m.session_mode,
  m.runtime_type,
  m.event_id,
  m.display_text,
  m.created_at,
  ci.display_name AS sender_display_name,
  ci.avatar_url AS sender_avatar_url,
  s.channel_type AS platform
FROM bot_history_messages m
LEFT JOIN channel_identities ci ON ci.id = m.sender_channel_identity_id
LEFT JOIN bot_sessions s ON s.id = m.session_id
WHERE m.session_id = $1
  AND m.turn_visible = true
  AND m.turn_id IS NOT NULL
  AND m.turn_position IS NOT NULL
  AND m.turn_message_seq IS NOT NULL
  AND (m.turn_position, m.turn_message_seq, m.created_at, m.id)
    > (
      SELECT c.turn_position, c.turn_message_seq, c.created_at, c.id
      FROM bot_history_messages c
      WHERE c.session_id = $1
        AND c.turn_visible = true
        AND c.turn_id IS NOT NULL
        AND c.turn_position IS NOT NULL
        AND c.turn_message_seq IS NOT NULL
        AND c.id = $3
      LIMIT 1
    )
ORDER BY m.turn_position ASC, m.turn_message_seq ASC, m.created_at ASC, m.id ASC
LIMIT $2;
