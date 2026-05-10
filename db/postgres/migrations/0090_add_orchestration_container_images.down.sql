-- 0090_add_orchestration_container_images
-- Remove the orchestration image catalog.

DROP INDEX IF EXISTS idx_orchestration_container_images_tenant_status;
DROP TABLE IF EXISTS orchestration_container_images;
