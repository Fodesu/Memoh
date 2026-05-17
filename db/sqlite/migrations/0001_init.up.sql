-- 0001_init
-- SQLite initial schema generated from the PostgreSQL baseline.

PRAGMA foreign_keys = ON;

-- users: Memoh user principal
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT,
  email TEXT,
  password_hash TEXT,
  role TEXT NOT NULL DEFAULT 'member',
  display_name TEXT,
  avatar_url TEXT,
  timezone TEXT NOT NULL DEFAULT 'UTC',
  data_root TEXT,
  last_login_at TEXT,
  is_active INTEGER NOT NULL DEFAULT 1,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT users_email_unique UNIQUE (email),
  CONSTRAINT users_username_unique UNIQUE (username)
);

-- channel_identities: unified inbound identity subject
CREATE TABLE IF NOT EXISTS channel_identities (
  id TEXT PRIMARY KEY,
  user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  channel_type TEXT NOT NULL,
  channel_subject_id TEXT NOT NULL,
  display_name TEXT,
  avatar_url TEXT,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT channel_identities_channel_type_subject_unique UNIQUE (channel_type, channel_subject_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_identities_user_id ON channel_identities(user_id);

-- user_channel_bindings: outbound delivery config
CREATE TABLE IF NOT EXISTS user_channel_bindings (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel_type TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT user_channel_bindings_unique UNIQUE (user_id, channel_type)
);

CREATE INDEX IF NOT EXISTS idx_user_channel_bindings_user_id ON user_channel_bindings(user_id);

CREATE TABLE IF NOT EXISTS providers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  client_type TEXT NOT NULL DEFAULT 'openai-completions',
  icon TEXT,
  enable INTEGER NOT NULL DEFAULT 1,
  config TEXT NOT NULL DEFAULT '{}',
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT providers_name_unique UNIQUE (name),
  CONSTRAINT providers_client_type_check CHECK (client_type IN (
    'openai-responses',
    'openai-completions',
    'anthropic-messages',
    'google-generative-ai',
    'openai-codex',
    'github-copilot',
    'edge-speech',
    'openai-speech',
    'openai-transcription',
    'openrouter-speech',
    'openrouter-transcription',
    'elevenlabs-speech',
    'elevenlabs-transcription',
    'deepgram-speech',
    'deepgram-transcription',
    'minimax-speech',
    'volcengine-speech',
    'alibabacloud-speech',
    'microsoft-speech',
    'google-speech',
    'google-transcription'
  ))
);

CREATE TABLE IF NOT EXISTS search_providers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  enable INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT search_providers_name_unique UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS models (
  id TEXT PRIMARY KEY,
  model_id TEXT NOT NULL,
  name TEXT,
  provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
  type TEXT NOT NULL DEFAULT 'chat',
  config TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT models_provider_id_model_id_unique UNIQUE (provider_id, model_id),
  CONSTRAINT models_type_check CHECK (type IN ('chat', 'embedding', 'speech', 'transcription'))
);

CREATE TABLE IF NOT EXISTS model_variants (
  id TEXT PRIMARY KEY,
  model_uuid TEXT NOT NULL REFERENCES models(id) ON DELETE CASCADE,
  variant_id TEXT NOT NULL,
  weight INTEGER NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_model_variants_model_uuid ON model_variants(model_uuid);
CREATE INDEX IF NOT EXISTS idx_model_variants_variant_id ON model_variants(variant_id);

CREATE TABLE IF NOT EXISTS memory_providers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT memory_providers_name_unique UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS bots (
  id TEXT PRIMARY KEY,
  owner_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  display_name TEXT,
  avatar_url TEXT,
  timezone TEXT,
  is_active INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'ready',
  acl_default_effect TEXT NOT NULL DEFAULT 'allow',
  language TEXT NOT NULL DEFAULT 'auto',
  reasoning_enabled INTEGER NOT NULL DEFAULT 0,
  reasoning_effort TEXT NOT NULL DEFAULT 'medium',
  chat_model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  search_provider_id TEXT REFERENCES search_providers(id) ON DELETE SET NULL,
  memory_provider_id TEXT REFERENCES memory_providers(id) ON DELETE SET NULL,
  heartbeat_enabled INTEGER NOT NULL DEFAULT 0,
  heartbeat_interval INTEGER NOT NULL DEFAULT 30,
  heartbeat_prompt TEXT NOT NULL DEFAULT '',
  heartbeat_model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  compaction_enabled INTEGER NOT NULL DEFAULT 0,
  compaction_threshold INTEGER NOT NULL DEFAULT 100000,
  compaction_ratio INTEGER NOT NULL DEFAULT 80,
  compaction_model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  title_model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  image_model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  discuss_probe_model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  tts_model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  transcription_model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  persist_full_tool_results INTEGER NOT NULL DEFAULT 0,
  show_tool_calls_in_im INTEGER NOT NULL DEFAULT 0,
  tool_approval_config TEXT NOT NULL DEFAULT '{"enabled":false,"write":{"require_approval":true,"bypass_globs":["/data/**","/tmp/**"],"force_review_globs":[]},"edit":{"require_approval":true,"bypass_globs":["/data/**","/tmp/**"],"force_review_globs":[]},"exec":{"require_approval":false,"bypass_commands":[],"force_review_commands":[]}}',
  display_enabled INTEGER NOT NULL DEFAULT 0,
  overlay_provider TEXT NOT NULL DEFAULT '',
  overlay_enabled INTEGER NOT NULL DEFAULT 0,
  overlay_config TEXT NOT NULL DEFAULT '{}',
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT bots_type_check CHECK (type IN ('personal', 'public')),
  CONSTRAINT bots_status_check CHECK (status IN ('creating', 'ready', 'deleting')),
  CONSTRAINT bots_acl_default_effect_check CHECK (acl_default_effect IN ('allow', 'deny')),
  CONSTRAINT bots_reasoning_effort_check CHECK (reasoning_effort IN ('low', 'medium', 'high'))
);

CREATE INDEX IF NOT EXISTS idx_bots_owner_user_id ON bots(owner_user_id);

CREATE TABLE IF NOT EXISTS bot_acl_rules (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  priority INTEGER NOT NULL DEFAULT 100,
  enabled INTEGER NOT NULL DEFAULT 1,
  description TEXT,
  action TEXT NOT NULL,
  effect TEXT NOT NULL,
  subject_kind TEXT NOT NULL,
  channel_identity_id TEXT REFERENCES channel_identities(id) ON DELETE CASCADE,
  subject_channel_type TEXT,
  source_channel TEXT,
  source_conversation_type TEXT,
  source_conversation_id TEXT,
  source_thread_id TEXT,
  created_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT bot_acl_rules_action_check CHECK (action IN ('chat.trigger')),
  CONSTRAINT bot_acl_rules_effect_check CHECK (effect IN ('allow', 'deny')),
  CONSTRAINT bot_acl_rules_subject_kind_check CHECK (subject_kind IN ('all', 'channel_identity', 'channel_type')),
  CONSTRAINT bot_acl_rules_source_conversation_type_check CHECK (
    source_conversation_type IS NULL OR source_conversation_type IN ('private', 'group', 'thread')
  ),
  CONSTRAINT bot_acl_rules_source_scope_check CHECK (
    (source_conversation_id IS NULL AND source_thread_id IS NULL)
    OR source_channel IS NOT NULL
  ),
  CONSTRAINT bot_acl_rules_source_thread_check CHECK (
    source_thread_id IS NULL OR source_conversation_id IS NOT NULL
  ),
  CONSTRAINT bot_acl_rules_subject_value_check CHECK (
    (subject_kind = 'all' AND channel_identity_id IS NULL AND subject_channel_type IS NULL) OR
    (subject_kind = 'channel_identity' AND channel_identity_id IS NOT NULL AND subject_channel_type IS NULL) OR
    (subject_kind = 'channel_type' AND channel_identity_id IS NULL AND subject_channel_type IS NOT NULL)
  )
);

CREATE INDEX IF NOT EXISTS idx_bot_acl_rules_bot_id ON bot_acl_rules(bot_id);
CREATE INDEX IF NOT EXISTS idx_bot_acl_rules_channel_identity_id ON bot_acl_rules(channel_identity_id);
CREATE INDEX IF NOT EXISTS idx_bot_acl_rules_subject_channel_type ON bot_acl_rules(subject_channel_type);

CREATE TABLE IF NOT EXISTS mcp_connections (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  is_active INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'unknown',
  tools_cache TEXT NOT NULL DEFAULT '[]',
  last_probed_at TEXT,
  status_message TEXT NOT NULL DEFAULT '',
  auth_type TEXT NOT NULL DEFAULT 'none',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT mcp_connections_type_check CHECK (type IN ('stdio', 'http', 'sse')),
  CONSTRAINT mcp_connections_unique UNIQUE (bot_id, name)
);

CREATE INDEX IF NOT EXISTS idx_mcp_connections_bot_id ON mcp_connections(bot_id);

CREATE TABLE IF NOT EXISTS mcp_oauth_tokens (
  id TEXT PRIMARY KEY,
  connection_id TEXT NOT NULL UNIQUE REFERENCES mcp_connections(id) ON DELETE CASCADE,
  resource_metadata_url TEXT NOT NULL DEFAULT '',
  authorization_server_url TEXT NOT NULL DEFAULT '',
  authorization_endpoint TEXT NOT NULL DEFAULT '',
  token_endpoint TEXT NOT NULL DEFAULT '',
  registration_endpoint TEXT NOT NULL DEFAULT '',
  scopes_supported TEXT NOT NULL DEFAULT '{}',
  client_id TEXT NOT NULL DEFAULT '',
  client_secret TEXT NOT NULL DEFAULT '',
  access_token TEXT NOT NULL DEFAULT '',
  refresh_token TEXT NOT NULL DEFAULT '',
  token_type TEXT NOT NULL DEFAULT 'Bearer',
  expires_at TEXT,
  scope TEXT NOT NULL DEFAULT '',
  pkce_code_verifier TEXT NOT NULL DEFAULT '',
  state_param TEXT NOT NULL DEFAULT '',
  resource_uri TEXT NOT NULL DEFAULT '',
  redirect_uri TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mcp_oauth_tokens_connection_id ON mcp_oauth_tokens(connection_id);

-- Bot history is bot-scoped (one history container per bot).

CREATE TABLE IF NOT EXISTS bot_channel_configs (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  channel_type TEXT NOT NULL,
  credentials TEXT NOT NULL DEFAULT '{}',
  external_identity TEXT,
  self_identity TEXT NOT NULL DEFAULT '{}',
  routing TEXT NOT NULL DEFAULT '{}',
  capabilities TEXT NOT NULL DEFAULT '{}',
  disabled INTEGER NOT NULL DEFAULT 0,
  verified_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT bot_channel_unique UNIQUE (bot_id, channel_type)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_bot_channel_external_identity
  ON bot_channel_configs(channel_type, external_identity);

CREATE INDEX IF NOT EXISTS idx_bot_channel_bot_id ON bot_channel_configs(bot_id);

-- channel_identity_bind_codes: one-time codes for channel identity->user linking
CREATE TABLE IF NOT EXISTS channel_identity_bind_codes (
  id TEXT PRIMARY KEY,
  token TEXT NOT NULL,
  issued_by_user_id TEXT NOT NULL REFERENCES users(id),
  channel_type TEXT,
  expires_at TEXT,
  used_at TEXT,
  used_by_channel_identity_id TEXT REFERENCES channel_identities(id),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT channel_identity_bind_codes_token_unique UNIQUE (token)
);

CREATE INDEX IF NOT EXISTS idx_channel_identity_bind_codes_channel_type ON channel_identity_bind_codes(channel_type);

-- bot_channel_routes: route mapping for inbound channel threads to bot history.
CREATE TABLE IF NOT EXISTS bot_channel_routes (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  channel_type TEXT NOT NULL,
  channel_config_id TEXT REFERENCES bot_channel_configs(id) ON DELETE SET NULL,
  external_conversation_id TEXT NOT NULL,
  external_thread_id TEXT,
  conversation_type TEXT,
  default_reply_target TEXT,
  active_session_id TEXT,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_bot_channel_routes_unique
  ON bot_channel_routes (bot_id, channel_type, external_conversation_id, COALESCE(external_thread_id, ''));
CREATE INDEX IF NOT EXISTS idx_bot_channel_routes_bot ON bot_channel_routes(bot_id);

-- bot_sessions: chat sessions within a bot, optionally linked to a channel route.
CREATE TABLE IF NOT EXISTS bot_sessions (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  route_id TEXT REFERENCES bot_channel_routes(id) ON DELETE SET NULL,
  channel_type TEXT,
  type TEXT NOT NULL DEFAULT 'chat' CHECK (type IN ('chat', 'heartbeat', 'schedule', 'subagent', 'discuss')),
  title TEXT NOT NULL DEFAULT '',
  metadata TEXT NOT NULL DEFAULT '{}',
  parent_session_id TEXT REFERENCES bot_sessions(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_bot_sessions_bot_id ON bot_sessions(bot_id);
CREATE INDEX IF NOT EXISTS idx_bot_sessions_route_id ON bot_sessions(route_id);
CREATE INDEX IF NOT EXISTS idx_bot_sessions_bot_active ON bot_sessions(bot_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_bot_sessions_parent ON bot_sessions(parent_session_id) WHERE parent_session_id IS NOT NULL;

-- bot_session_events: DCP pipeline event store for cold-start replay.
CREATE TABLE IF NOT EXISTS bot_session_events (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES bot_sessions(id) ON DELETE CASCADE,
  event_kind TEXT NOT NULL CHECK (event_kind IN ('message', 'edit', 'delete', 'service')),
  event_data TEXT NOT NULL,
  external_message_id TEXT,
  sender_channel_identity_id TEXT,
  received_at_ms BIGINT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_session_events_session_received
  ON bot_session_events (session_id, received_at_ms);
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_events_dedup
  ON bot_session_events (session_id, event_kind, external_message_id)
  WHERE external_message_id IS NOT NULL AND external_message_id != '';

-- bot_history_messages: unified message history under bot scope.
CREATE TABLE IF NOT EXISTS bot_history_messages (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  session_id TEXT REFERENCES bot_sessions(id) ON DELETE SET NULL,
  sender_channel_identity_id TEXT REFERENCES channel_identities(id),
  sender_account_user_id TEXT REFERENCES users(id),
  source_message_id TEXT,
  source_reply_to_message_id TEXT,
  role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system', 'tool')),
  content TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  usage TEXT,
  model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  compact_id TEXT,
  event_id TEXT REFERENCES bot_session_events(id) ON DELETE SET NULL,
  display_text TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bot_history_messages_bot_created ON bot_history_messages(bot_id, created_at);
CREATE INDEX IF NOT EXISTS idx_bot_history_messages_compact ON bot_history_messages(compact_id);
CREATE INDEX IF NOT EXISTS idx_bot_history_messages_session
  ON bot_history_messages(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_bot_history_messages_session_source
  ON bot_history_messages(session_id, source_message_id);
CREATE INDEX IF NOT EXISTS idx_bot_history_messages_session_reply
  ON bot_history_messages(session_id, source_reply_to_message_id);

CREATE TABLE IF NOT EXISTS tool_approval_requests (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES bot_sessions(id) ON DELETE CASCADE,
  route_id TEXT REFERENCES bot_channel_routes(id) ON DELETE SET NULL,
  channel_identity_id TEXT REFERENCES channel_identities(id) ON DELETE SET NULL,
  tool_call_id TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  tool_input TEXT NOT NULL,
  short_id INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  decision_reason TEXT NOT NULL DEFAULT '',
  requested_by_channel_identity_id TEXT REFERENCES channel_identities(id) ON DELETE SET NULL,
  decided_by_channel_identity_id TEXT REFERENCES channel_identities(id) ON DELETE SET NULL,
  requested_message_id TEXT REFERENCES bot_history_messages(id) ON DELETE SET NULL,
  prompt_message_id TEXT REFERENCES bot_history_messages(id) ON DELETE SET NULL,
  prompt_external_message_id TEXT NOT NULL DEFAULT '',
  source_platform TEXT NOT NULL DEFAULT '',
  reply_target TEXT NOT NULL DEFAULT '',
  conversation_type TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  decided_at TEXT,
  CONSTRAINT tool_approval_tool_name_check CHECK (tool_name IN ('write', 'edit', 'exec')),
  CONSTRAINT tool_approval_status_check CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'cancelled')),
  CONSTRAINT tool_approval_short_id_unique UNIQUE (session_id, short_id),
  CONSTRAINT tool_approval_tool_call_unique UNIQUE (session_id, tool_call_id)
);

CREATE INDEX IF NOT EXISTS idx_tool_approval_bot_status_created
  ON tool_approval_requests(bot_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_tool_approval_session_status_created
  ON tool_approval_requests(session_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_tool_approval_prompt_external
  ON tool_approval_requests(prompt_external_message_id)
  WHERE prompt_external_message_id != '';

CREATE TABLE IF NOT EXISTS containers (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  container_id TEXT NOT NULL,
  container_name TEXT NOT NULL,
  image TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'created',
  namespace TEXT NOT NULL DEFAULT 'default',
  auto_start INTEGER NOT NULL DEFAULT 1,
  container_path TEXT NOT NULL DEFAULT '/data',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_started_at TEXT,
  last_stopped_at TEXT,
  CONSTRAINT containers_container_id_unique UNIQUE (container_id),
  CONSTRAINT containers_container_name_unique UNIQUE (container_name)
);

CREATE INDEX IF NOT EXISTS idx_containers_bot_id ON containers(bot_id);

CREATE TABLE IF NOT EXISTS snapshots (
  id TEXT PRIMARY KEY,
  container_id TEXT NOT NULL REFERENCES containers(container_id) ON DELETE CASCADE,
  runtime_snapshot_name TEXT NOT NULL,
  display_name TEXT,
  parent_runtime_snapshot_name TEXT,
  snapshotter TEXT NOT NULL,
  source TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_snapshots_container_runtime_name
  ON snapshots(container_id, runtime_snapshot_name);
CREATE INDEX IF NOT EXISTS idx_snapshots_container_created_at
  ON snapshots(container_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_snapshots_runtime_name
  ON snapshots(runtime_snapshot_name);

CREATE TABLE IF NOT EXISTS container_versions (
  id TEXT PRIMARY KEY,
  container_id TEXT NOT NULL REFERENCES containers(container_id) ON DELETE CASCADE,
  snapshot_id TEXT NOT NULL REFERENCES snapshots(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (container_id, version)
);

CREATE INDEX IF NOT EXISTS idx_container_versions_container_id ON container_versions(container_id);
CREATE INDEX IF NOT EXISTS idx_container_versions_snapshot_id ON container_versions(snapshot_id);

CREATE TABLE IF NOT EXISTS lifecycle_events (
  id TEXT PRIMARY KEY,
  container_id TEXT NOT NULL REFERENCES containers(container_id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_lifecycle_events_container_id ON lifecycle_events(container_id);
CREATE INDEX IF NOT EXISTS idx_lifecycle_events_event_type ON lifecycle_events(event_type);

CREATE TABLE IF NOT EXISTS schedule (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  pattern TEXT NOT NULL,
  max_calls INTEGER,
  current_calls INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  enabled INTEGER NOT NULL DEFAULT 1,
  command TEXT NOT NULL,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_schedule_bot_id ON schedule(bot_id);
CREATE INDEX IF NOT EXISTS idx_schedule_enabled ON schedule(enabled);

-- storage_providers: pluggable object storage backends
CREATE TABLE IF NOT EXISTS storage_providers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT storage_providers_name_unique UNIQUE (name),
  CONSTRAINT storage_providers_provider_check CHECK (provider IN ('localfs', 's3', 'gcs'))
);

-- bot_storage_bindings: per-bot storage backend selection
CREATE TABLE IF NOT EXISTS bot_storage_bindings (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  storage_provider_id TEXT NOT NULL REFERENCES storage_providers(id) ON DELETE CASCADE,
  base_path TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT bot_storage_bindings_unique UNIQUE (bot_id)
);

CREATE INDEX IF NOT EXISTS idx_bot_storage_bindings_bot_id ON bot_storage_bindings(bot_id);

-- bot_history_message_assets: soft link (message -> content_hash only).
-- MIME, size, storage_key are derived from storage at read time.
CREATE TABLE IF NOT EXISTS bot_history_message_assets (
  id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL REFERENCES bot_history_messages(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT 'attachment',
  ordinal INTEGER NOT NULL DEFAULT 0,
  content_hash TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT message_asset_content_unique UNIQUE (message_id, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_message_assets_message_id ON bot_history_message_assets(message_id);


-- bot_heartbeat_logs: structured execution records for periodic heartbeat checks.
CREATE TABLE IF NOT EXISTS bot_heartbeat_logs (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  session_id TEXT REFERENCES bot_sessions(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'alert', 'error')),
  result_text TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  usage TEXT,
  model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_heartbeat_logs_bot_started ON bot_heartbeat_logs(bot_id, started_at DESC);

CREATE TABLE IF NOT EXISTS bot_history_message_compacts (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  session_id TEXT REFERENCES bot_sessions(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ok', 'error')),
  summary TEXT NOT NULL DEFAULT '',
  message_count INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  usage TEXT,
  model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_compacts_bot_session ON bot_history_message_compacts(bot_id, session_id, started_at DESC);

-- schedule_logs: structured execution records for scheduled tasks.
CREATE TABLE IF NOT EXISTS schedule_logs (
  id TEXT PRIMARY KEY,
  schedule_id TEXT NOT NULL REFERENCES schedule(id) ON DELETE CASCADE,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  session_id TEXT REFERENCES bot_sessions(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'error')),
  result_text TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  usage TEXT,
  model_id TEXT REFERENCES models(id) ON DELETE SET NULL,
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_schedule_logs_schedule ON schedule_logs(schedule_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_schedule_logs_bot ON schedule_logs(bot_id, started_at DESC);

-- email_providers: pluggable email service backends
CREATE TABLE IF NOT EXISTS email_providers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT email_providers_name_unique UNIQUE (name)
);

-- email_oauth_tokens: stored OAuth2 tokens for Gmail email providers
CREATE TABLE IF NOT EXISTS email_oauth_tokens (
  id TEXT PRIMARY KEY,
  email_provider_id TEXT NOT NULL UNIQUE REFERENCES email_providers(id) ON DELETE CASCADE,
  email_address TEXT NOT NULL DEFAULT '',
  access_token TEXT NOT NULL DEFAULT '',
  refresh_token TEXT NOT NULL DEFAULT '',
  expires_at TEXT,
  scope TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_email_oauth_tokens_state ON email_oauth_tokens(state) WHERE state != '';

-- bot_email_bindings: per-bot email provider binding with read/write/delete permissions
CREATE TABLE IF NOT EXISTS bot_email_bindings (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  email_provider_id TEXT NOT NULL REFERENCES email_providers(id) ON DELETE CASCADE,
  email_address TEXT NOT NULL,
  can_read INTEGER NOT NULL DEFAULT TRUE,
  can_write INTEGER NOT NULL DEFAULT TRUE,
  can_delete INTEGER NOT NULL DEFAULT FALSE,
  config TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT bot_email_bindings_unique UNIQUE (bot_id, email_provider_id)
);

CREATE INDEX IF NOT EXISTS idx_bot_email_bindings_bot_id ON bot_email_bindings(bot_id);
CREATE INDEX IF NOT EXISTS idx_bot_email_bindings_provider_id ON bot_email_bindings(email_provider_id);

-- email_outbox: outbound email audit log
CREATE TABLE IF NOT EXISTS email_outbox (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES email_providers(id) ON DELETE CASCADE,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  message_id TEXT NOT NULL DEFAULT '',
  from_address TEXT NOT NULL DEFAULT '',
  to_addresses TEXT NOT NULL DEFAULT '[]',
  subject TEXT NOT NULL DEFAULT '',
  body_text TEXT NOT NULL DEFAULT '',
  body_html TEXT NOT NULL DEFAULT '',
  attachments TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
  error TEXT NOT NULL DEFAULT '',
  sent_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_email_outbox_provider_id ON email_outbox(provider_id);
CREATE INDEX IF NOT EXISTS idx_email_outbox_bot_id ON email_outbox(bot_id, created_at DESC);

-- provider_oauth_tokens: OAuth2 tokens for LLM providers (e.g. OpenAI Codex OAuth)
CREATE TABLE IF NOT EXISTS provider_oauth_tokens (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL UNIQUE REFERENCES providers(id) ON DELETE CASCADE,
  access_token TEXT NOT NULL DEFAULT '',
  refresh_token TEXT NOT NULL DEFAULT '',
  expires_at TEXT,
  scope TEXT NOT NULL DEFAULT '',
  token_type TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT '',
  pkce_code_verifier TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_provider_oauth_tokens_state ON provider_oauth_tokens(state) WHERE state != '';

-- user_provider_oauth_tokens: per-user OAuth2 tokens for providers with user-scoped auth (e.g. GitHub Copilot)
CREATE TABLE IF NOT EXISTS user_provider_oauth_tokens (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  access_token TEXT NOT NULL DEFAULT '',
  refresh_token TEXT NOT NULL DEFAULT '',
  expires_at TEXT,
  scope TEXT NOT NULL DEFAULT '',
  token_type TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT '',
  pkce_code_verifier TEXT NOT NULL DEFAULT '',
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT user_provider_oauth_tokens_provider_user_unique UNIQUE (provider_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_provider_oauth_tokens_state ON user_provider_oauth_tokens(state) WHERE state != '';

-- orchestration_runs: phase-1 orchestration kernel runs
CREATE TABLE IF NOT EXISTS orchestration_runs (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  owner_subject TEXT NOT NULL,
  lifecycle_status TEXT NOT NULL CHECK (lifecycle_status IN ('created', 'running', 'waiting_human', 'cancelling', 'completed', 'failed', 'cancelled')),
  intent_status TEXT NOT NULL CHECK (intent_status IN ('idle', 'active')),
  status_version INTEGER NOT NULL DEFAULT 1,
  planner_epoch INTEGER NOT NULL DEFAULT 1,
  last_event_seq INTEGER NOT NULL DEFAULT 0,
  root_task_id TEXT NOT NULL,
  goal TEXT NOT NULL DEFAULT '',
  input TEXT NOT NULL DEFAULT '{}',
  output_schema TEXT NOT NULL DEFAULT '{}',
  requested_control_policy TEXT NOT NULL DEFAULT '{}',
  control_policy TEXT NOT NULL DEFAULT '{}',
  source_metadata TEXT NOT NULL DEFAULT '{}',
  policies TEXT NOT NULL DEFAULT '{}',
  created_by TEXT NOT NULL,
  terminal_reason TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_orchestration_runs_owner_created_at ON orchestration_runs(owner_subject, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orchestration_runs_lifecycle_status ON orchestration_runs(lifecycle_status);

-- orchestration_tasks: phase-1 orchestration kernel tasks
CREATE TABLE IF NOT EXISTS orchestration_tasks (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  decomposed_from_task_id TEXT,
  kind TEXT NOT NULL DEFAULT 'step',
  goal TEXT NOT NULL DEFAULT '',
  inputs TEXT NOT NULL DEFAULT '{}',
  planner_epoch INTEGER NOT NULL DEFAULT 1,
  superseded_by_planner_epoch INTEGER,
  worker_profile TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 0,
  retry_policy TEXT NOT NULL DEFAULT '{}',
  verification_policy TEXT NOT NULL DEFAULT '{}',
  env_preconditions TEXT NOT NULL DEFAULT '{"required": false}',
  status TEXT NOT NULL CHECK (status IN ('created', 'ready', 'dispatching', 'running', 'verifying', 'waiting_human', 'completed', 'blocked', 'failed', 'cancelled')),
  status_version INTEGER NOT NULL DEFAULT 1,
  waiting_checkpoint_id TEXT,
  waiting_scope TEXT NOT NULL DEFAULT '' CHECK (waiting_scope IN ('', 'task', 'run')),
  latest_result_id TEXT,
  ready_at TEXT,
  blocked_reason TEXT NOT NULL DEFAULT '',
  terminal_reason TEXT NOT NULL DEFAULT '',
  blackboard_scope TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_tasks_id_run_unique UNIQUE (id, run_id)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_tasks_run_created_at ON orchestration_tasks(run_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_tasks_run_status ON orchestration_tasks(run_id, status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_tasks_waiting_checkpoint ON orchestration_tasks(waiting_checkpoint_id) WHERE waiting_checkpoint_id IS NOT NULL;

-- orchestration_input_manifests: immutable dispatch-time task input slices
CREATE TABLE IF NOT EXISTS orchestration_input_manifests (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  captured_task_inputs TEXT NOT NULL DEFAULT '{}',
  captured_artifact_versions TEXT NOT NULL DEFAULT '[]',
  captured_blackboard_revisions TEXT NOT NULL DEFAULT '[]',
  captured_env_preconditions TEXT NOT NULL DEFAULT '{"required": false}',
  projection_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_input_manifests_id_run_task_unique UNIQUE (id, run_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_input_manifests_task_created_at ON orchestration_input_manifests(task_id, created_at DESC, id DESC);

-- orchestration_task_results: authoritative durable task outputs
CREATE TABLE IF NOT EXISTS orchestration_task_results (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL UNIQUE REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id TEXT,
  status TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('completed', 'failed')),
  summary TEXT NOT NULL DEFAULT '',
  failure_class TEXT NOT NULL DEFAULT '',
  request_replan INTEGER NOT NULL DEFAULT 0,
  artifact_intents TEXT NOT NULL DEFAULT '[]',
  structured_output TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_task_results_id_run_task_unique UNIQUE (id, run_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_task_results_run_created_at ON orchestration_task_results(run_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orchestration_task_results_attempt ON orchestration_task_results(attempt_id) WHERE attempt_id IS NOT NULL;

-- orchestration_artifacts: authoritative committed artifact metadata
CREATE TABLE IF NOT EXISTS orchestration_artifacts (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id TEXT,
  kind TEXT NOT NULL,
  uri TEXT NOT NULL,
  version TEXT NOT NULL,
  digest TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_artifacts_id_run_task_unique UNIQUE (id, run_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_artifacts_run_created_at ON orchestration_artifacts(run_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_artifacts_task_created_at ON orchestration_artifacts(task_id, created_at, id);

-- orchestration_human_checkpoints: authoritative HITL checkpoints
CREATE TABLE IF NOT EXISTS orchestration_human_checkpoints (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  blocks_run INTEGER NOT NULL DEFAULT 0,
  planner_epoch INTEGER NOT NULL DEFAULT 1,
  superseded_by_planner_epoch INTEGER,
  status TEXT NOT NULL CHECK (status IN ('open', 'resolved', 'timed_out', 'cancelled', 'superseded')),
  status_version INTEGER NOT NULL DEFAULT 1,
  question TEXT NOT NULL DEFAULT '',
  options TEXT NOT NULL DEFAULT '[]',
  default_action TEXT NOT NULL DEFAULT '{}',
  resume_policy TEXT NOT NULL DEFAULT '{}',
  timeout_at TEXT,
  resolved_by TEXT NOT NULL DEFAULT '',
  resolved_mode TEXT NOT NULL DEFAULT '' CHECK (resolved_mode IN ('', 'select_option', 'freeform', 'use_default')),
  resolved_option_id TEXT NOT NULL DEFAULT '',
  resolved_freeform_input TEXT NOT NULL DEFAULT '',
  resolved_at TEXT,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_human_checkpoints_id_run_unique UNIQUE (id, run_id),
  CONSTRAINT orchestration_human_checkpoints_id_run_task_unique UNIQUE (id, run_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_human_checkpoints_run_created_at ON orchestration_human_checkpoints(run_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_human_checkpoints_run_status ON orchestration_human_checkpoints(run_id, status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_human_checkpoints_open_timeout
  ON orchestration_human_checkpoints(timeout_at, created_at, id)
  WHERE status = 'open' AND timeout_at IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_orchestration_human_checkpoints_open_run_barrier_unique
  ON orchestration_human_checkpoints(run_id)
  WHERE blocks_run = 1 AND status = 'open';

-- orchestration_intents: authoritative planner/replanner work queue
CREATE TABLE IF NOT EXISTS orchestration_intents (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  checkpoint_id TEXT,
  kind TEXT NOT NULL CHECK (kind IN ('start_run', 'checkpoint_resume', 'attempt_finalize', 'replan')),
  status TEXT NOT NULL CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
  base_planner_epoch INTEGER NOT NULL DEFAULT 0,
  claim_epoch INTEGER NOT NULL DEFAULT 0,
  claim_token TEXT NOT NULL DEFAULT '',
  claimed_by TEXT NOT NULL DEFAULT '',
  lease_expires_at TEXT,
  last_heartbeat_at TEXT,
  failure_reason TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_intents_checkpoint_requires_task CHECK (checkpoint_id IS NULL OR task_id IS NOT NULL),
  CONSTRAINT orchestration_intents_id_run_unique UNIQUE (id, run_id)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_intents_run_created_at ON orchestration_intents(run_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_intents_status_created_at ON orchestration_intents(status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_intents_lease_expires_at ON orchestration_intents(lease_expires_at) WHERE lease_expires_at IS NOT NULL;

-- orchestration_task_dependencies: authoritative task DAG edges
CREATE TABLE IF NOT EXISTS orchestration_task_dependencies (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  predecessor_task_id TEXT NOT NULL,
  successor_task_id TEXT NOT NULL,
  planner_epoch INTEGER NOT NULL DEFAULT 1,
  superseded_by_planner_epoch INTEGER,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_task_dependencies_no_self_edge CHECK (predecessor_task_id <> successor_task_id),
  CONSTRAINT orchestration_task_dependencies_unique UNIQUE (run_id, predecessor_task_id, successor_task_id, planner_epoch)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_task_dependencies_successor ON orchestration_task_dependencies(successor_task_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_task_dependencies_predecessor ON orchestration_task_dependencies(predecessor_task_id, created_at, id);

-- orchestration_task_attempts: authoritative execution attempts with lease/fencing state
CREATE TABLE IF NOT EXISTS orchestration_task_attempts (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_no INTEGER NOT NULL,
  worker_id TEXT NOT NULL DEFAULT '',
  executor_id TEXT NOT NULL DEFAULT '',
  worker_lease_token TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('created', 'claimed', 'binding', 'running', 'completed', 'failed', 'lost')),
  claim_epoch INTEGER NOT NULL DEFAULT 0,
  claim_token TEXT NOT NULL DEFAULT '',
  lease_expires_at TEXT,
  last_heartbeat_at TEXT,
  input_manifest_id TEXT REFERENCES orchestration_input_manifests(id) ON DELETE SET NULL,
  park_checkpoint_id TEXT REFERENCES orchestration_human_checkpoints(id) ON DELETE SET NULL,
  failure_class TEXT NOT NULL DEFAULT '',
  terminal_reason TEXT NOT NULL DEFAULT '',
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_task_attempts_task_attempt_no_unique UNIQUE (task_id, attempt_no),
  CONSTRAINT orchestration_task_attempts_id_run_task_unique UNIQUE (id, run_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_task_attempts_run_created_at ON orchestration_task_attempts(run_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_task_attempts_task_created_at ON orchestration_task_attempts(task_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_task_attempts_status_created_at ON orchestration_task_attempts(status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_task_attempts_lease_expires_at ON orchestration_task_attempts(lease_expires_at) WHERE lease_expires_at IS NOT NULL;

-- orchestration_events: committed orchestration event timeline
CREATE TABLE IF NOT EXISTS orchestration_events (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT,
  attempt_id TEXT,
  checkpoint_id TEXT,
  seq INTEGER NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  aggregate_version INTEGER NOT NULL,
  type TEXT NOT NULL,
  causation_event_id TEXT,
  correlation_id TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  published_at TEXT,
  CONSTRAINT orchestration_events_run_seq_unique UNIQUE (run_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_events_run_seq ON orchestration_events(run_id, seq);
CREATE INDEX IF NOT EXISTS idx_orchestration_events_aggregate_seq ON orchestration_events(run_id, aggregate_type, aggregate_id, seq DESC);
CREATE INDEX IF NOT EXISTS idx_orchestration_events_task_seq ON orchestration_events(task_id, seq) WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orchestration_events_checkpoint_seq ON orchestration_events(checkpoint_id, seq) WHERE checkpoint_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orchestration_events_unpublished ON orchestration_events(run_id, seq) WHERE published_at IS NULL;

-- orchestration_projection_snapshots: materialized projection snapshots keyed by seq
CREATE TABLE IF NOT EXISTS orchestration_projection_snapshots (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  projection_kind TEXT NOT NULL CHECK (projection_kind IN ('tasks', 'checkpoints', 'artifacts', 'run')),
  seq INTEGER NOT NULL,
  payload TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_projection_snapshots_unique UNIQUE (run_id, projection_kind, seq)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_projection_snapshots_lookup ON orchestration_projection_snapshots(run_id, projection_kind, seq DESC);

-- orchestration_idempotency_records: request dedupe for mutating control APIs
CREATE TABLE IF NOT EXISTS orchestration_idempotency_records (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  caller_subject TEXT NOT NULL,
  method TEXT NOT NULL,
  target_id TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'in_progress' CHECK (state IN ('in_progress', 'completed')),
  response_payload TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_idempotency_records_unique UNIQUE (tenant_id, caller_subject, method, target_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_idempotency_records_lookup ON orchestration_idempotency_records(tenant_id, caller_subject, method, target_id, idempotency_key);

-- orchestration_workers: runtime worker leases and capabilities
CREATE TABLE IF NOT EXISTS orchestration_workers (
  id TEXT PRIMARY KEY,
  executor_id TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '',
  capabilities TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'unavailable')),
  lease_token TEXT NOT NULL DEFAULT '',
  last_heartbeat_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  lease_expires_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_orchestration_workers_status_lease_expires_at ON orchestration_workers(status, lease_expires_at);

-- orchestration_container_images: tenant-scoped image catalog entries
CREATE TABLE IF NOT EXISTS orchestration_container_images (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  owner_subject TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  source_type TEXT NOT NULL DEFAULT 'registry' CHECK (source_type IN ('registry', 'dockerfile')),
  image_ref TEXT NOT NULL DEFAULT '',
  dockerfile TEXT NOT NULL DEFAULT '',
  build_options TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'pending', 'building', 'failed', 'archived')),
  digest TEXT NOT NULL DEFAULT '',
  last_build_error TEXT NOT NULL DEFAULT '',
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_container_images_tenant_name_unique UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_container_images_tenant_status
  ON orchestration_container_images (tenant_id, status, name, id);

-- orchestration_env_resources: capacity-managed runtime targets
CREATE TABLE IF NOT EXISTS orchestration_env_resources (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  owner_subject TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('container', 'browser', 'desktop', 'phone', 'other')),
  name TEXT NOT NULL,
  config TEXT NOT NULL DEFAULT '{}',
  capacity INTEGER NOT NULL DEFAULT 1 CHECK (capacity > 0),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'archived')),
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_env_resources_tenant_name_unique UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_env_resources_tenant_kind
  ON orchestration_env_resources (tenant_id, kind, status, name);

-- orchestration_env_sessions: leased runtime instances
CREATE TABLE IF NOT EXISTS orchestration_env_sessions (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  resource_id TEXT NOT NULL REFERENCES orchestration_env_resources(id) ON DELETE RESTRICT,
  status TEXT NOT NULL DEFAULT 'reserved' CHECK (
    status IN ('reserved', 'committed', 'aborted', 'held', 'released', 'expired', 'reclaimed')
  ),
  lease_holder_kind TEXT NOT NULL CHECK (
    lease_holder_kind IN ('worker', 'verifier', 'orchestrator', 'human')
  ),
  lease_holder_id TEXT NOT NULL DEFAULT '',
  lease_token TEXT NOT NULL DEFAULT '',
  lease_epoch INTEGER NOT NULL DEFAULT 1 CHECK (lease_epoch > 0),
  lease_acquired_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  lease_expires_at TEXT,
  run_id TEXT REFERENCES orchestration_runs(id) ON DELETE SET NULL,
  task_id TEXT REFERENCES orchestration_tasks(id) ON DELETE SET NULL,
  attempt_id TEXT,
  runtime_handle TEXT NOT NULL DEFAULT '{}',
  metadata TEXT NOT NULL DEFAULT '{}',
  released_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_orchestration_env_sessions_resource_status
  ON orchestration_env_sessions (resource_id, status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_env_sessions_tenant_status
  ON orchestration_env_sessions (tenant_id, status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_env_sessions_lease_expiry
  ON orchestration_env_sessions (lease_expires_at, id)
  WHERE status IN ('reserved', 'committed', 'held') AND lease_expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orchestration_env_sessions_attempt
  ON orchestration_env_sessions (attempt_id, id)
  WHERE attempt_id IS NOT NULL;

-- orchestration_env_lease_reservations: admission queue for saturated resources
CREATE TABLE IF NOT EXISTS orchestration_env_lease_reservations (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  resource_id TEXT NOT NULL REFERENCES orchestration_env_resources(id) ON DELETE CASCADE,
  requester_kind TEXT NOT NULL CHECK (
    requester_kind IN ('worker', 'verifier', 'orchestrator', 'human')
  ),
  requester_id TEXT NOT NULL DEFAULT '',
  run_id TEXT REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id TEXT,
  priority INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (
    status IN ('pending', 'committed', 'aborted', 'expired')
  ),
  committed_session_id TEXT REFERENCES orchestration_env_sessions(id) ON DELETE SET NULL,
  requested_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at TEXT,
  committed_at TEXT,
  aborted_at TEXT,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_orchestration_env_lease_reservations_queue
  ON orchestration_env_lease_reservations (resource_id, status, priority DESC, requested_at, id)
  WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_orchestration_env_lease_reservations_tenant
  ON orchestration_env_lease_reservations (tenant_id, status, requested_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_env_lease_reservations_attempt
  ON orchestration_env_lease_reservations (attempt_id, id)
  WHERE attempt_id IS NOT NULL;

-- orchestration_env_bindings: active/held task-to-session bindings
CREATE TABLE IF NOT EXISTS orchestration_env_bindings (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id TEXT,
  session_id TEXT NOT NULL REFERENCES orchestration_env_sessions(id) ON DELETE CASCADE,
  purpose TEXT NOT NULL DEFAULT 'primary' CHECK (purpose IN ('primary', 'secondary')),
  status TEXT NOT NULL DEFAULT 'active' CHECK (
    status IN ('active', 'held', 'released', 'reclaimed')
  ),
  held_for_checkpoint_id TEXT REFERENCES orchestration_human_checkpoints(id) ON DELETE SET NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  released_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_orchestration_env_bindings_run
  ON orchestration_env_bindings (run_id, status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_env_bindings_task_attempt
  ON orchestration_env_bindings (task_id, attempt_id, status, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_env_bindings_session
  ON orchestration_env_bindings (session_id, status, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_orchestration_env_bindings_active_session_unique
  ON orchestration_env_bindings (session_id)
  WHERE status IN ('active', 'held');

-- orchestration_env_snapshots: point-in-time runtime captures
CREATE TABLE IF NOT EXISTS orchestration_env_snapshots (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  session_id TEXT NOT NULL REFERENCES orchestration_env_sessions(id) ON DELETE CASCADE,
  run_id TEXT REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id TEXT,
  kind TEXT NOT NULL CHECK (
    kind IN ('pre_action', 'post_action', 'checkpoint', 'periodic', 'manual')
  ),
  effect_class TEXT NOT NULL DEFAULT '' CHECK (
    effect_class IN ('', 'env_local_read', 'env_local_mutation', 'external_read', 'external_write', 'external_irreversible')
  ),
  runtime_ref TEXT NOT NULL DEFAULT '{}',
  digest TEXT NOT NULL DEFAULT '',
  size_bytes INTEGER NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_orchestration_env_snapshots_session
  ON orchestration_env_snapshots (session_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_env_snapshots_attempt
  ON orchestration_env_snapshots (attempt_id, created_at, id)
  WHERE attempt_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orchestration_env_snapshots_run_kind
  ON orchestration_env_snapshots (run_id, kind, created_at, id)
  WHERE run_id IS NOT NULL;

-- orchestration_task_verifications: authoritative verifier work queue
CREATE TABLE IF NOT EXISTS orchestration_task_verifications (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL UNIQUE REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  result_id TEXT NOT NULL UNIQUE REFERENCES orchestration_task_results(id) ON DELETE CASCADE,
  attempt_no INTEGER NOT NULL DEFAULT 1,
  worker_id TEXT NOT NULL DEFAULT '',
  executor_id TEXT NOT NULL DEFAULT '',
  worker_lease_token TEXT NOT NULL DEFAULT '',
  verifier_profile TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('created', 'claimed', 'running', 'completed', 'failed', 'lost')),
  claim_epoch INTEGER NOT NULL DEFAULT 0,
  claim_token TEXT NOT NULL DEFAULT '',
  lease_expires_at TEXT,
  last_heartbeat_at TEXT,
  verdict TEXT NOT NULL DEFAULT '' CHECK (verdict IN ('', 'accepted', 'rejected')),
  summary TEXT NOT NULL DEFAULT '',
  failure_class TEXT NOT NULL DEFAULT '',
  terminal_reason TEXT NOT NULL DEFAULT '',
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_task_verifications_id_run_task_unique UNIQUE (id, run_id, task_id),
  CONSTRAINT orchestration_task_verifications_id_run_result_unique UNIQUE (id, run_id, result_id)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_task_verifications_run_status ON orchestration_task_verifications(run_id, status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_task_verifications_claim_queue ON orchestration_task_verifications(status, verifier_profile, created_at, id) WHERE status = 'created';
CREATE INDEX IF NOT EXISTS idx_orchestration_task_verifications_lease_expiry ON orchestration_task_verifications(lease_expires_at, id) WHERE status IN ('claimed', 'running') AND lease_expires_at IS NOT NULL;

-- orchestration_action_ledger: durable external action trace
CREATE TABLE IF NOT EXISTS orchestration_action_ledger (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id TEXT REFERENCES orchestration_task_attempts(id) ON DELETE CASCADE,
  verification_id TEXT REFERENCES orchestration_task_verifications(id) ON DELETE CASCADE,
  action_kind TEXT NOT NULL DEFAULT 'tool_call' CHECK (
    action_kind IN ('tool_call', 'env_acquire', 'env_release', 'env_hold', 'env_snapshot')
  ),
  status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
  effect_class TEXT NOT NULL DEFAULT '' CHECK (
    effect_class IN ('', 'env_local_read', 'env_local_mutation', 'external_read', 'external_write', 'external_irreversible')
  ),
  env_session_id TEXT REFERENCES orchestration_env_sessions(id) ON DELETE SET NULL,
  env_binding_id TEXT REFERENCES orchestration_env_bindings(id) ON DELETE SET NULL,
  before_env_snapshot_id TEXT REFERENCES orchestration_env_snapshots(id) ON DELETE SET NULL,
  after_env_snapshot_id TEXT REFERENCES orchestration_env_snapshots(id) ON DELETE SET NULL,
  tool_name TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  input_payload TEXT NOT NULL DEFAULT 'null',
  output_payload TEXT NOT NULL DEFAULT 'null',
  error_payload TEXT NOT NULL DEFAULT 'null',
  summary TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_action_ledger_exactly_one_subject CHECK (
    (attempt_id IS NOT NULL AND verification_id IS NULL)
    OR (attempt_id IS NULL AND verification_id IS NOT NULL)
  )
);

CREATE INDEX IF NOT EXISTS idx_orchestration_action_ledger_run_started_at ON orchestration_action_ledger(run_id, started_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_action_ledger_task_started_at ON orchestration_action_ledger(task_id, started_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_action_ledger_attempt_started_at ON orchestration_action_ledger(attempt_id, started_at, id) WHERE attempt_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orchestration_action_ledger_verification_started_at ON orchestration_action_ledger(verification_id, started_at, id) WHERE verification_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_orchestration_action_ledger_attempt_tool_call_unique
  ON orchestration_action_ledger(attempt_id, tool_call_id)
  WHERE attempt_id IS NOT NULL AND action_kind = 'tool_call';
CREATE UNIQUE INDEX IF NOT EXISTS idx_orchestration_action_ledger_verification_tool_call_unique
  ON orchestration_action_ledger(verification_id, tool_call_id)
  WHERE verification_id IS NOT NULL AND action_kind = 'tool_call';
CREATE INDEX IF NOT EXISTS idx_orchestration_action_ledger_env_session
  ON orchestration_action_ledger(env_session_id, started_at, id)
  WHERE env_session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orchestration_action_ledger_effect
  ON orchestration_action_ledger(run_id, effect_class, started_at, id)
  WHERE effect_class <> '';

-- orchestration_side_effect_approval_tokens: HITL / policy grants
CREATE TABLE IF NOT EXISTS orchestration_side_effect_approval_tokens (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id TEXT NOT NULL REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id TEXT NOT NULL REFERENCES orchestration_task_attempts(id) ON DELETE CASCADE,
  claim_epoch INTEGER NOT NULL CHECK (claim_epoch > 0),
  env_session_id TEXT REFERENCES orchestration_env_sessions(id) ON DELETE SET NULL,
  env_lease_epoch INTEGER NOT NULL DEFAULT 0 CHECK (env_lease_epoch >= 0),
  effect_class TEXT NOT NULL DEFAULT 'external_irreversible' CHECK (effect_class IN ('external_irreversible')),
  token_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'consumed', 'revoked', 'expired')),
  approved_by TEXT NOT NULL DEFAULT '',
  approval_reason TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  consumed_action_id TEXT REFERENCES orchestration_action_ledger(id) ON DELETE SET NULL,
  expires_at TEXT,
  consumed_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT orchestration_side_effect_tokens_hash_unique UNIQUE (token_hash)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_side_effect_tokens_attempt
  ON orchestration_side_effect_approval_tokens(attempt_id, status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_side_effect_tokens_env
  ON orchestration_side_effect_approval_tokens(env_session_id, env_lease_epoch, status, id)
  WHERE env_session_id IS NOT NULL;
