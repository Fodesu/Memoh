-- 0085_add_orchestration_event_outbox_index
-- Drop the partial outbox index added in the up migration.

DROP INDEX IF EXISTS idx_orchestration_events_unpublished;
