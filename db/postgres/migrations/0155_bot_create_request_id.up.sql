-- 0155_bot_create_request_id
-- Idempotent bot creation. A client sends the same request id when it retries
-- a POST /bots whose response it never saw; the unique index turns the retry
-- into a lookup of the bot already created instead of a second bot (or a name
-- collision). Scoped per owner so one user's key never resolves to another
-- user's bot. Bots created without a key keep NULL and are not indexed.

ALTER TABLE public.bots ADD COLUMN IF NOT EXISTS create_request_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_bots_create_request
    ON public.bots (team_id, owner_user_id, create_request_id)
    WHERE create_request_id IS NOT NULL;
