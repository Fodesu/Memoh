-- 0087_add_orchestration_env_preconditions

ALTER TABLE orchestration_input_manifests
  DROP COLUMN IF EXISTS captured_env_preconditions;

ALTER TABLE orchestration_tasks
  DROP COLUMN IF EXISTS env_preconditions;
