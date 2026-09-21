-- Declarative post-create setup. Intent columns are written by the API layer;
-- observation columns and the lease are written by the reconciler only.

-- name: UpsertBotSetupIntent :one
-- Record a new setup intent. A new row starts at generation 1; an existing
-- row bumps the generation, replaces the spec, resets the retry budget and
-- makes the row due now.
INSERT INTO bot_setups (bot_id, spec, requested_by_user_id)
VALUES (sqlc.arg(bot_id), sqlc.arg(spec), sqlc.narg(requested_by_user_id))
ON CONFLICT (bot_id) DO UPDATE SET
  desired_generation   = bot_setups.desired_generation + 1,
  spec                 = EXCLUDED.spec,
  requested_by_user_id = COALESCE(EXCLUDED.requested_by_user_id, bot_setups.requested_by_user_id),
  state                = 'pending',
  attempts             = 0,
  next_attempt_at      = now(),
  version              = bot_setups.version + 1,
  updated_at           = now()
RETURNING *;

-- name: RetryBotSetupIntent :one
-- Ask for another pass over the same spec: bump the generation and reset the
-- retry budget. Steps already done stay done (see ResetBotSetupStepsNotDone).
UPDATE bot_setups
SET
  desired_generation = desired_generation + 1,
  state              = 'pending',
  attempts           = 0,
  next_attempt_at    = now(),
  version            = version + 1,
  updated_at         = now()
WHERE team_id = public.memoh_current_team_id() AND bot_id = sqlc.arg(bot_id)
RETURNING *;

-- name: GetBotSetup :one
SELECT * FROM bot_setups
WHERE team_id = public.memoh_current_team_id() AND bot_id = sqlc.arg(bot_id);

-- name: ClaimBotSetups :many
-- Claim due rows for one reconcile pass: not done, backoff elapsed, lease free
-- or expired. Recording an intent resets state to 'pending', so state alone
-- says whether the latest intent has been answered. SKIP LOCKED keeps
-- concurrent Server instances from claiming the same row.
UPDATE bot_setups
SET
  lease_owner = sqlc.arg(lease_owner),
  lease_until = now() + make_interval(secs => sqlc.arg(lease_seconds)::double precision),
  version     = version + 1,
  updated_at  = now()
WHERE bot_id IN (
  SELECT bot_id FROM bot_setups
  WHERE team_id = public.memoh_current_team_id()
    AND next_attempt_at <= now()
    AND (lease_until IS NULL OR lease_until < now())
    AND state <> 'done'
  ORDER BY next_attempt_at
  LIMIT sqlc.arg(lim)::int
  FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: RenewBotSetupLease :execrows
UPDATE bot_setups
SET
  lease_until = now() + make_interval(secs => sqlc.arg(lease_seconds)::double precision),
  updated_at  = now()
WHERE team_id = public.memoh_current_team_id()
  AND bot_id = sqlc.arg(bot_id)
  AND lease_owner = sqlc.arg(lease_owner);

-- name: UpdateBotSetupObserved :one
-- Write an observation. The lease must still be held by the caller; the
-- version pins the row the caller read so a concurrent intent bump is not
-- silently overwritten.
UPDATE bot_setups
SET
  state               = sqlc.arg(state),
  observed_generation = sqlc.arg(observed_generation),
  attempts            = sqlc.arg(attempts),
  next_attempt_at     = sqlc.arg(next_attempt_at),
  lease_owner         = CASE WHEN sqlc.arg(release_lease)::boolean THEN '' ELSE lease_owner END,
  lease_until         = CASE WHEN sqlc.arg(release_lease)::boolean THEN NULL ELSE lease_until END,
  version             = version + 1,
  updated_at          = now()
WHERE team_id = public.memoh_current_team_id()
  AND bot_id = sqlc.arg(bot_id)
  AND lease_owner = sqlc.arg(lease_owner)
  AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: ReleaseBotSetupLease :execrows
UPDATE bot_setups
SET
  lease_owner = '',
  lease_until = NULL,
  updated_at  = now()
WHERE team_id = public.memoh_current_team_id()
  AND bot_id = sqlc.arg(bot_id)
  AND lease_owner = sqlc.arg(lease_owner);

-- name: UpsertBotSetupStep :one
INSERT INTO bot_setup_steps (bot_id, step, status, generation, attempts, last_error)
VALUES (sqlc.arg(bot_id), sqlc.arg(step), sqlc.arg(status), sqlc.arg(generation), sqlc.arg(attempts), sqlc.arg(last_error))
ON CONFLICT (bot_id, step) DO UPDATE SET
  status     = EXCLUDED.status,
  generation = EXCLUDED.generation,
  attempts   = EXCLUDED.attempts,
  last_error = EXCLUDED.last_error,
  updated_at = now()
RETURNING *;

-- name: ResetBotSetupStepsNotDone :exec
-- A retry re-runs every managed step that has not completed; done steps keep
-- their result and skipped steps stay skipped.
UPDATE bot_setup_steps
SET status = 'pending', attempts = 0, last_error = '', updated_at = now()
WHERE team_id = public.memoh_current_team_id()
  AND bot_id = sqlc.arg(bot_id)
  AND status IN ('pending', 'running', 'retrying', 'failed');

-- name: ListBotSetupSteps :many
SELECT * FROM bot_setup_steps
WHERE team_id = public.memoh_current_team_id() AND bot_id = sqlc.arg(bot_id)
ORDER BY step;
