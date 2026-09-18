-- 0153_bot_workspaces
-- Drop the declarative workspace state and restore the previous bots status
-- set. Bots left in 'failed' are folded back to 'ready' first so the
-- narrower constraint can be re-added.

BEGIN;

DROP TABLE IF EXISTS public.bot_workspaces;

UPDATE public.bots SET status = 'ready' WHERE status = 'failed';
ALTER TABLE public.bots DROP CONSTRAINT IF EXISTS bots_status_check;
ALTER TABLE public.bots ADD CONSTRAINT bots_status_check
    CHECK (status IN ('creating', 'ready', 'deleting'));

COMMIT;
