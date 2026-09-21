-- 0156_bot_setups
-- Declarative post-create setup for bots. POST /bots may carry a setup spec
-- (settings, access grants, later the Agent install); the server records it
-- as an intent and a reconciler applies it step by step once the workspace is
-- running, so a client that disconnects never leaves a half-configured bot.
-- One row per bot holds the spec and the lease; one row per step holds where
-- that step stands, so progress is visible and a retry only redoes what is
-- not done.

BEGIN;

CREATE TABLE IF NOT EXISTS public.bot_setups (
    bot_id               UUID        PRIMARY KEY,
    team_id              UUID        NOT NULL DEFAULT public.memoh_current_team_id()
                                     REFERENCES public.teams(id) ON DELETE RESTRICT,
    -- Intent, written only by the API layer: what the bot should end up with
    -- (settings, grants, agent), and who asked, so grants are attributed.
    desired_generation   BIGINT      NOT NULL DEFAULT 1,
    spec                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    requested_by_user_id UUID        REFERENCES public.users(id) ON DELETE SET NULL,
    -- Observation, written only by the reconciler while holding the lease.
    -- state is the row as a whole; per-step detail lives in bot_setup_steps.
    -- attempts is the fast retry budget consumed for the current generation.
    state                TEXT        NOT NULL DEFAULT 'pending',
    observed_generation  BIGINT      NOT NULL DEFAULT 0,
    attempts             INTEGER     NOT NULL DEFAULT 0,
    next_attempt_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_owner          TEXT        NOT NULL DEFAULT '',
    lease_until          TIMESTAMPTZ,
    version              BIGINT      NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Team-scoped key in the name the team migration derives, so the chain
    -- and 0001_init agree; it is also the target of bot_setup_steps' FK.
    CONSTRAINT memoh_team_key_f535610d5617 UNIQUE (team_id, bot_id),
    CONSTRAINT bot_setups_state_check
        CHECK (state IN ('pending', 'running', 'waiting', 'done', 'failed'))
);

-- bots is under FORCE ROW LEVEL SECURITY; add the reference NOT VALID so the
-- constraint is never validated through the policy-scoped scan.
ALTER TABLE public.bot_setups DROP CONSTRAINT IF EXISTS bot_setups_bot_id_fkey;
ALTER TABLE public.bot_setups
    ADD CONSTRAINT bot_setups_bot_id_fkey
    FOREIGN KEY (team_id, bot_id)
    REFERENCES public.bots(team_id, id) ON DELETE CASCADE
    NOT VALID;

CREATE INDEX IF NOT EXISTS idx_bot_setups_due
    ON public.bot_setups (team_id, next_attempt_at);

-- One row per (bot, step), created for every step when an intent is recorded:
-- pending for the steps the spec manages, skipped for the rest, so a client
-- always sees the full list. Written only by the reconciler afterwards.
CREATE TABLE IF NOT EXISTS public.bot_setup_steps (
    bot_id      UUID        NOT NULL,
    team_id     UUID        NOT NULL DEFAULT public.memoh_current_team_id()
                            REFERENCES public.teams(id) ON DELETE RESTRICT,
    step        TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'pending',
    -- generation is the intent this status belongs to; attempts counts this
    -- step's own failures; last_error is the redacted last failure, or a note
    -- on a partial success (a grant that could not be created).
    generation  BIGINT      NOT NULL DEFAULT 0,
    attempts    INTEGER     NOT NULL DEFAULT 0,
    last_error  TEXT        NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (bot_id, step),
    CONSTRAINT memoh_team_key_b5390cc5401f UNIQUE (team_id, bot_id, step),
    CONSTRAINT bot_setup_steps_step_check
        CHECK (step IN ('settings', 'grants', 'agent', 'claim', 'install', 'enable')),
    CONSTRAINT bot_setup_steps_status_check
        CHECK (status IN ('pending', 'running', 'retrying', 'failed', 'done', 'skipped')),
    CONSTRAINT bot_setup_steps_setup_fkey
        FOREIGN KEY (team_id, bot_id) REFERENCES public.bot_setups(team_id, bot_id) ON DELETE CASCADE
);

ALTER TABLE public.bot_setups ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.bot_setups FORCE ROW LEVEL SECURITY;
ALTER TABLE public.bot_setup_steps ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.bot_setup_steps FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS bot_setups_team_select ON public.bot_setups;
DROP POLICY IF EXISTS bot_setups_team_insert ON public.bot_setups;
DROP POLICY IF EXISTS bot_setups_team_update ON public.bot_setups;
DROP POLICY IF EXISTS bot_setups_team_delete ON public.bot_setups;
CREATE POLICY bot_setups_team_select ON public.bot_setups
    FOR SELECT USING (team_id = public.memoh_current_team_id());
CREATE POLICY bot_setups_team_insert ON public.bot_setups
    FOR INSERT WITH CHECK (team_id = public.memoh_current_team_id());
CREATE POLICY bot_setups_team_update ON public.bot_setups
    FOR UPDATE
    USING (team_id = public.memoh_current_team_id())
    WITH CHECK (team_id = public.memoh_current_team_id());
CREATE POLICY bot_setups_team_delete ON public.bot_setups
    FOR DELETE USING (team_id = public.memoh_current_team_id());

DROP POLICY IF EXISTS bot_setup_steps_team_select ON public.bot_setup_steps;
DROP POLICY IF EXISTS bot_setup_steps_team_insert ON public.bot_setup_steps;
DROP POLICY IF EXISTS bot_setup_steps_team_update ON public.bot_setup_steps;
DROP POLICY IF EXISTS bot_setup_steps_team_delete ON public.bot_setup_steps;
CREATE POLICY bot_setup_steps_team_select ON public.bot_setup_steps
    FOR SELECT USING (team_id = public.memoh_current_team_id());
CREATE POLICY bot_setup_steps_team_insert ON public.bot_setup_steps
    FOR INSERT WITH CHECK (team_id = public.memoh_current_team_id());
CREATE POLICY bot_setup_steps_team_update ON public.bot_setup_steps
    FOR UPDATE
    USING (team_id = public.memoh_current_team_id())
    WITH CHECK (team_id = public.memoh_current_team_id());
CREATE POLICY bot_setup_steps_team_delete ON public.bot_setup_steps
    FOR DELETE USING (team_id = public.memoh_current_team_id());

COMMIT;
