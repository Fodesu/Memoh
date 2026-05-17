-- 0006_add_orchestration_runtime
-- Add SQLite equivalents for the orchestration core, environment runtime,
-- and side-effect approval token schemas.

PRAGMA foreign_keys = ON;

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
