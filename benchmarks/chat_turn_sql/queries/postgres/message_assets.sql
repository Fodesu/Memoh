-- message_assets
-- Production query: db/postgres/queries/media.sql ListMessageAssetsBatch
SELECT
  id,
  message_id,
  role,
  ordinal,
  content_hash,
  name,
  metadata,
  created_at
FROM bot_history_message_assets
WHERE message_id = ANY($1::uuid[])
ORDER BY message_id, ordinal, created_at, id;
