-- 0083_add_orchestration_env_preconditions
-- Record planner env preconditions on tasks and what the kernel actually
-- captured into each dispatch input manifest. Used by Stage 3-E to drive
-- env session reservation, binding, and verifier replay.

ALTER TABLE orchestration_tasks
  ADD COLUMN IF NOT EXISTS env_preconditions JSONB NOT NULL DEFAULT '{"required": false}'::jsonb;

ALTER TABLE orchestration_input_manifests
  ADD COLUMN IF NOT EXISTS captured_env_preconditions JSONB NOT NULL DEFAULT '{"required": false}'::jsonb;
