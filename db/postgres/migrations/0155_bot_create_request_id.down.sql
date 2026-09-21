-- 0155_bot_create_request_id
-- Drop the idempotency key for bot creation.

DROP INDEX IF EXISTS public.idx_bots_create_request;
ALTER TABLE public.bots DROP COLUMN IF EXISTS create_request_id;
