-- 0086_add_orchestration_container_images
-- Add a tenant-scoped image catalog for orchestration env resources.

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
