-- 0082_add_orchestration_env_runtime
-- Add orchestration env runtime tables, env preconditions, action ledger env references, and container image catalog.

CREATE TABLE IF NOT EXISTS orchestration_container_images (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id TEXT NOT NULL,
  owner_subject TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  source_type TEXT NOT NULL DEFAULT 'registry' CHECK (source_type IN ('registry', 'dockerfile')),
  image_ref TEXT NOT NULL DEFAULT '',
  dockerfile TEXT NOT NULL DEFAULT '',
  build_options JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'pending', 'building', 'failed', 'archived')),
  digest TEXT NOT NULL DEFAULT '',
  last_build_error TEXT NOT NULL DEFAULT '',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT orchestration_container_images_tenant_name_unique UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_container_images_tenant_status
  ON orchestration_container_images (tenant_id, status, name, id);

ALTER TABLE orchestration_container_images
  ADD COLUMN IF NOT EXISTS source_type TEXT NOT NULL DEFAULT 'registry'
    CHECK (source_type IN ('registry', 'dockerfile'));
ALTER TABLE orchestration_container_images
  ADD COLUMN IF NOT EXISTS dockerfile TEXT NOT NULL DEFAULT '';
ALTER TABLE orchestration_container_images
  ADD COLUMN IF NOT EXISTS build_options JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE orchestration_container_images
  ADD COLUMN IF NOT EXISTS digest TEXT NOT NULL DEFAULT '';
ALTER TABLE orchestration_container_images
  ADD COLUMN IF NOT EXISTS last_build_error TEXT NOT NULL DEFAULT '';
ALTER TABLE orchestration_container_images
  DROP CONSTRAINT IF EXISTS orchestration_container_images_status_check;
ALTER TABLE orchestration_container_images
  ADD CONSTRAINT orchestration_container_images_status_check
  CHECK (status IN ('ready', 'pending', 'building', 'failed', 'archived'));

CREATE TABLE IF NOT EXISTS orchestration_env_resources (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id TEXT NOT NULL,
  owner_subject TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('container', 'browser', 'desktop', 'phone', 'other')),
  name TEXT NOT NULL,
  config JSONB NOT NULL DEFAULT '{}'::jsonb,
  capacity INTEGER NOT NULL DEFAULT 1 CHECK (capacity > 0),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'archived')),
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT orchestration_env_resources_tenant_name_unique UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_orchestration_env_resources_tenant_kind
  ON orchestration_env_resources (tenant_id, kind, status, name);

CREATE TABLE IF NOT EXISTS orchestration_env_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id TEXT NOT NULL,
  resource_id UUID NOT NULL REFERENCES orchestration_env_resources(id) ON DELETE RESTRICT,
  status TEXT NOT NULL DEFAULT 'reserved' CHECK (
    status IN ('reserved', 'committed', 'aborted', 'held', 'released', 'expired', 'reclaimed')
  ),
  lease_holder_kind TEXT NOT NULL CHECK (
    lease_holder_kind IN ('worker', 'verifier', 'orchestrator', 'human')
  ),
  lease_holder_id TEXT NOT NULL DEFAULT '',
  lease_token TEXT NOT NULL DEFAULT '',
  lease_epoch BIGINT NOT NULL DEFAULT 1 CHECK (lease_epoch > 0),
  lease_acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  lease_expires_at TIMESTAMPTZ,
  run_id UUID REFERENCES orchestration_runs(id) ON DELETE SET NULL,
  task_id UUID REFERENCES orchestration_tasks(id) ON DELETE SET NULL,
  attempt_id UUID,
  runtime_handle JSONB NOT NULL DEFAULT '{}'::jsonb,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  released_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
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
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id TEXT NOT NULL,
  resource_id UUID NOT NULL REFERENCES orchestration_env_resources(id) ON DELETE CASCADE,
  requester_kind TEXT NOT NULL CHECK (
    requester_kind IN ('worker', 'verifier', 'orchestrator', 'human')
  ),
  requester_id TEXT NOT NULL DEFAULT '',
  run_id UUID REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id UUID REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id UUID,
  priority INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (
    status IN ('pending', 'committed', 'aborted', 'expired')
  ),
  committed_session_id UUID REFERENCES orchestration_env_sessions(id) ON DELETE SET NULL,
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ,
  committed_at TIMESTAMPTZ,
  aborted_at TIMESTAMPTZ,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
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
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id TEXT NOT NULL,
  run_id UUID NOT NULL REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id UUID NOT NULL REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id UUID,
  session_id UUID NOT NULL REFERENCES orchestration_env_sessions(id) ON DELETE CASCADE,
  purpose TEXT NOT NULL DEFAULT 'primary' CHECK (purpose IN ('primary', 'secondary')),
  status TEXT NOT NULL DEFAULT 'active' CHECK (
    status IN ('active', 'held', 'released', 'reclaimed')
  ),
  held_for_checkpoint_id UUID REFERENCES orchestration_human_checkpoints(id) ON DELETE SET NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  released_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
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
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id TEXT NOT NULL,
  session_id UUID NOT NULL REFERENCES orchestration_env_sessions(id) ON DELETE CASCADE,
  run_id UUID REFERENCES orchestration_runs(id) ON DELETE CASCADE,
  task_id UUID REFERENCES orchestration_tasks(id) ON DELETE CASCADE,
  attempt_id UUID,
  kind TEXT NOT NULL CHECK (
    kind IN ('pre_action', 'post_action', 'checkpoint', 'periodic', 'manual')
  ),
  effect_class TEXT NOT NULL DEFAULT '' CHECK (
    effect_class IN ('', 'env_local_read', 'env_local_mutation', 'external_read', 'external_write', 'external_irreversible')
  ),
  runtime_ref JSONB NOT NULL DEFAULT '{}'::jsonb,
  digest TEXT NOT NULL DEFAULT '',
  size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orchestration_env_snapshots_session
  ON orchestration_env_snapshots (session_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_orchestration_env_snapshots_attempt
  ON orchestration_env_snapshots (attempt_id, created_at, id)
  WHERE attempt_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orchestration_env_snapshots_run_kind
  ON orchestration_env_snapshots (run_id, kind, created_at, id)
  WHERE run_id IS NOT NULL;

ALTER TABLE orchestration_tasks
  ADD COLUMN IF NOT EXISTS env_preconditions JSONB NOT NULL DEFAULT '{"required": false}'::jsonb;

ALTER TABLE orchestration_input_manifests
  ADD COLUMN IF NOT EXISTS captured_env_preconditions JSONB NOT NULL DEFAULT '{"required": false}'::jsonb;

ALTER TABLE orchestration_action_ledger
  ADD COLUMN IF NOT EXISTS effect_class TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS env_session_id UUID,
  ADD COLUMN IF NOT EXISTS env_binding_id UUID,
  ADD COLUMN IF NOT EXISTS before_env_snapshot_id UUID,
  ADD COLUMN IF NOT EXISTS after_env_snapshot_id UUID;

ALTER TABLE orchestration_action_ledger
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_action_kind_check,
  ADD CONSTRAINT orchestration_action_ledger_action_kind_check CHECK (
    action_kind IN ('tool_call', 'env_acquire', 'env_release', 'env_hold', 'env_snapshot')
  );

ALTER TABLE orchestration_action_ledger
  DROP CONSTRAINT IF EXISTS orchestration_action_ledger_effect_class_check,
  ADD CONSTRAINT orchestration_action_ledger_effect_class_check CHECK (
    effect_class IN ('', 'env_local_read', 'env_local_mutation', 'external_read', 'external_write', 'external_irreversible')
  );

DROP INDEX IF EXISTS idx_orchestration_action_ledger_attempt_tool_call_unique;
DROP INDEX IF EXISTS idx_orchestration_action_ledger_verification_tool_call_unique;

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

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orchestration_action_ledger_env_session_fk') THEN
    ALTER TABLE orchestration_action_ledger
      ADD CONSTRAINT orchestration_action_ledger_env_session_fk
      FOREIGN KEY (env_session_id) REFERENCES orchestration_env_sessions(id) ON DELETE SET NULL;
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orchestration_action_ledger_env_binding_fk') THEN
    ALTER TABLE orchestration_action_ledger
      ADD CONSTRAINT orchestration_action_ledger_env_binding_fk
      FOREIGN KEY (env_binding_id) REFERENCES orchestration_env_bindings(id) ON DELETE SET NULL;
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orchestration_action_ledger_before_env_snapshot_fk') THEN
    ALTER TABLE orchestration_action_ledger
      ADD CONSTRAINT orchestration_action_ledger_before_env_snapshot_fk
      FOREIGN KEY (before_env_snapshot_id) REFERENCES orchestration_env_snapshots(id) ON DELETE SET NULL;
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orchestration_action_ledger_after_env_snapshot_fk') THEN
    ALTER TABLE orchestration_action_ledger
      ADD CONSTRAINT orchestration_action_ledger_after_env_snapshot_fk
      FOREIGN KEY (after_env_snapshot_id) REFERENCES orchestration_env_snapshots(id) ON DELETE SET NULL;
  END IF;
END $$;
