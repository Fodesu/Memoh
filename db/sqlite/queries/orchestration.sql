-- name: CreateOrchestrationRun :one
INSERT INTO orchestration_runs (
  id,
  tenant_id,
  owner_subject,
  lifecycle_status,
  planning_status,
  status_version,
  planner_epoch,
  last_event_seq,
  root_task_id,
  goal,
  input,
  requested_control_policy,
  control_policy,
  source_metadata,
  policies,
  created_by,
  terminal_reason
) VALUES (
  sqlc.arg(id),
  sqlc.arg(tenant_id),
  sqlc.arg(owner_subject),
  sqlc.arg(lifecycle_status),
  sqlc.arg(planning_status),
  sqlc.arg(status_version),
  sqlc.arg(planner_epoch),
  sqlc.arg(last_event_seq),
  sqlc.arg(root_task_id),
  sqlc.arg(goal),
  sqlc.arg(input),
  sqlc.arg(requested_control_policy),
  sqlc.arg(control_policy),
  sqlc.arg(source_metadata),
  sqlc.arg(policies),
  sqlc.arg(created_by),
  sqlc.arg(terminal_reason)
) RETURNING *;

-- name: GetOrchestrationRunByID :one
SELECT *
FROM orchestration_runs
WHERE id = sqlc.arg(id);

-- name: GetOrchestrationRunByIDForUpdate :one
SELECT *
FROM orchestration_runs
WHERE id = sqlc.arg(id)
;

-- name: ListOrchestrationRunsByBot :many
SELECT *
FROM orchestration_runs
WHERE tenant_id = sqlc.arg(tenant_id)
  AND owner_subject = sqlc.arg(owner_subject)
  AND COALESCE(json_extract(source_metadata, '$.bot_id'), '') = sqlc.arg(bot_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(limit_count);

-- name: ListTerminalizableRunningOrchestrationRuns :many
SELECT r.*
FROM orchestration_runs r
WHERE r.lifecycle_status = 'running'
  AND NOT EXISTS (
    SELECT 1
    FROM orchestration_planning_intents i
    WHERE i.run_id = r.id
      AND i.status IN ('pending', 'processing', 'failed')
  )
  AND NOT EXISTS (
    SELECT 1
    FROM orchestration_task_attempts a
    WHERE a.run_id = r.id
      AND a.status IN ('created', 'claimed', 'binding', 'running')
  )
  AND NOT EXISTS (
    SELECT 1
    FROM orchestration_human_checkpoints c
    WHERE c.run_id = r.id
      AND c.status = 'open'
  )
  AND NOT EXISTS (
    -- excludes runs with any non-terminal task OR any failed task (failed is
    -- absent from the IN list, so a failed task triggers this NOT EXISTS).
    SELECT 1
    FROM orchestration_tasks t
    WHERE t.run_id = r.id
      AND t.superseded_by_planner_epoch IS NULL
      AND t.status NOT IN ('completed', 'cancelled')
  )
  AND EXISTS (
    SELECT 1
    FROM orchestration_tasks t
    WHERE t.run_id = r.id
      AND t.superseded_by_planner_epoch IS NULL
      AND t.role = 'final'
      AND t.status = 'completed'
  )
ORDER BY r.updated_at ASC, r.id ASC
LIMIT sqlc.arg(limit_count);

-- name: AllocateOrchestrationRunEventSeqs :one
UPDATE orchestration_runs
SET last_event_seq = last_event_seq + sqlc.arg(delta),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING last_event_seq;

-- name: MarkOrchestrationRunRunning :one
UPDATE orchestration_runs
SET lifecycle_status = 'running',
    status_version = status_version + 1,
    terminal_reason = '',
    finished_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationRunWaitingHuman :one
UPDATE orchestration_runs
SET lifecycle_status = 'waiting_human',
    status_version = status_version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationRunCancelling :one
UPDATE orchestration_runs
SET lifecycle_status = 'cancelling',
    status_version = status_version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationRunPlanningActive :one
UPDATE orchestration_runs
SET planning_status = 'active',
    status_version = status_version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationRunPlanningIdle :one
UPDATE orchestration_runs
SET planning_status = 'idle',
    status_version = status_version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: AdvanceOrchestrationRunPlannerEpoch :one
UPDATE orchestration_runs
SET planner_epoch = planner_epoch + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationRunCompleted :one
UPDATE orchestration_runs
SET lifecycle_status = 'completed',
    status_version = status_version + 1,
    updated_at = CURRENT_TIMESTAMP,
    finished_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationRunFailed :one
UPDATE orchestration_runs
SET lifecycle_status = 'failed',
    status_version = status_version + 1,
    terminal_reason = sqlc.arg(terminal_reason),
    updated_at = CURRENT_TIMESTAMP,
    finished_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationRunCancelled :one
UPDATE orchestration_runs
SET lifecycle_status = 'cancelled',
    status_version = status_version + 1,
    terminal_reason = sqlc.arg(terminal_reason),
    updated_at = CURRENT_TIMESTAMP,
    finished_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateOrchestrationTask :one
INSERT INTO orchestration_tasks (
  id,
  run_id,
  decomposed_from_task_id,
  kind,
  role,
  goal,
  inputs,
  planner_epoch,
  worker_profile,
  priority,
  retry_policy,
  verification_policy,
  status,
  status_version,
  waiting_scope,
  blocked_reason,
  terminal_reason,
  blackboard_scope
) VALUES (
  sqlc.arg(id),
  sqlc.arg(run_id),
  sqlc.arg(decomposed_from_task_id),
  sqlc.arg(kind),
  sqlc.arg(role),
  sqlc.arg(goal),
  sqlc.arg(inputs),
  sqlc.arg(planner_epoch),
  sqlc.arg(worker_profile),
  sqlc.arg(priority),
  sqlc.arg(retry_policy),
  sqlc.arg(verification_policy),
  sqlc.arg(status),
  sqlc.arg(status_version),
  sqlc.arg(waiting_scope),
  sqlc.arg(blocked_reason),
  sqlc.arg(terminal_reason),
  sqlc.arg(blackboard_scope)
) RETURNING *;

-- name: GetOrchestrationTaskByID :one
SELECT *
FROM orchestration_tasks
WHERE id = sqlc.arg(id);

-- name: GetOrchestrationTaskByIDForUpdate :one
SELECT *
FROM orchestration_tasks
WHERE id = sqlc.arg(id)
;

-- name: ListCurrentOrchestrationTasksByRun :many
SELECT *
FROM orchestration_tasks
WHERE run_id = sqlc.arg(run_id)
ORDER BY created_at ASC, id ASC;

-- name: ListSchedulableOrchestrationTasks :many
SELECT t.*
FROM orchestration_tasks t
JOIN orchestration_runs r ON r.id = t.run_id
WHERE t.status = 'ready'
  AND t.superseded_by_planner_epoch IS NULL
  AND r.lifecycle_status = 'running'
ORDER BY t.priority DESC, t.ready_at ASC NULLS FIRST, t.created_at ASC, t.id ASC;

-- name: ListDependencyBlockedOrchestrationTasks :many
SELECT t.*
FROM orchestration_tasks t
JOIN orchestration_runs r ON r.id = t.run_id
WHERE t.status = 'created'
  AND t.superseded_by_planner_epoch IS NULL
  AND r.lifecycle_status = 'running'
ORDER BY t.created_at ASC, t.id ASC;

-- name: CreateOrchestrationTaskDependency :one
INSERT INTO orchestration_task_dependencies (
  id,
  run_id,
  predecessor_task_id,
  successor_task_id,
  planner_epoch,
  superseded_by_planner_epoch
) VALUES (
  sqlc.arg(id),
  sqlc.arg(run_id),
  sqlc.arg(predecessor_task_id),
  sqlc.arg(successor_task_id),
  sqlc.arg(planner_epoch),
  sqlc.arg(superseded_by_planner_epoch)
) RETURNING *;

-- name: ListActiveOrchestrationTaskDependenciesBySuccessor :many
SELECT *
FROM orchestration_task_dependencies
WHERE successor_task_id = sqlc.arg(successor_task_id)
  AND superseded_by_planner_epoch IS NULL
ORDER BY created_at ASC, id ASC;

-- name: ListActiveOrchestrationTaskDependenciesByPredecessor :many
SELECT *
FROM orchestration_task_dependencies
WHERE predecessor_task_id = sqlc.arg(predecessor_task_id)
  AND superseded_by_planner_epoch IS NULL
ORDER BY created_at ASC, id ASC;

-- name: ListCurrentOrchestrationTaskDependenciesByRun :many
SELECT *
FROM orchestration_task_dependencies
WHERE run_id = sqlc.arg(run_id)
ORDER BY created_at ASC, id ASC;

-- name: GetNextOrchestrationTaskAttemptNo :one
SELECT COALESCE(MAX(attempt_no), 0) + 1 AS next_attempt_no
FROM orchestration_task_attempts
WHERE task_id = sqlc.arg(task_id);

-- name: CountActiveOrchestrationTaskAttemptsByTask :one
SELECT COUNT(*)
FROM orchestration_task_attempts
WHERE task_id = sqlc.arg(task_id)
  AND status IN ('created', 'claimed', 'binding', 'running');

-- name: CountNonTerminalOrchestrationTasksByRun :one
SELECT COUNT(*)
FROM orchestration_tasks
WHERE run_id = sqlc.arg(run_id)
  AND superseded_by_planner_epoch IS NULL
  AND status NOT IN ('completed', 'failed', 'cancelled');

-- name: CountFailedOrchestrationTasksByRun :one
SELECT COUNT(*)
FROM orchestration_tasks
WHERE run_id = sqlc.arg(run_id)
  AND superseded_by_planner_epoch IS NULL
  AND status = 'failed';

-- name: CountCompletedFinalOrchestrationTasksByRun :one
SELECT COUNT(*)
FROM orchestration_tasks
WHERE run_id = sqlc.arg(run_id)
  AND superseded_by_planner_epoch IS NULL
  AND role = 'final'
  AND status = 'completed';

-- name: CountOpenRunBlockingCheckpointsByRun :one
SELECT COUNT(*)
FROM orchestration_human_checkpoints
WHERE run_id = sqlc.arg(run_id)
  AND blocks_run = TRUE
  AND status = 'open';

-- name: GetOrchestrationWorkerByIDForUpdate :one
SELECT *
FROM orchestration_workers
WHERE id = sqlc.arg(id)
;

-- name: MarkOrchestrationTaskWaitingHuman :one
UPDATE orchestration_tasks
SET status = 'waiting_human',
    status_version = status_version + 1,
    waiting_checkpoint_id = sqlc.arg(waiting_checkpoint_id),
    waiting_scope = sqlc.arg(waiting_scope),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskReadyFromCheckpoint :one
UPDATE orchestration_tasks
SET status = 'ready',
    status_version = status_version + 1,
    waiting_checkpoint_id = NULL,
    waiting_scope = '',
    ready_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskBlocked :one
UPDATE orchestration_tasks
SET status = 'blocked',
    status_version = status_version + 1,
    blocked_reason = sqlc.arg(blocked_reason),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskSuperseded :one
UPDATE orchestration_tasks
SET superseded_by_planner_epoch = sqlc.arg(superseded_by_planner_epoch),
    status_version = status_version + 1,
    waiting_checkpoint_id = NULL,
    waiting_scope = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND superseded_by_planner_epoch IS NULL
RETURNING *;

-- name: MarkOrchestrationTaskDispatching :one
UPDATE orchestration_tasks
SET status = 'dispatching',
    status_version = status_version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskRunning :one
UPDATE orchestration_tasks
SET status = 'running',
    status_version = status_version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskVerifying :one
UPDATE orchestration_tasks
SET status = 'verifying',
    status_version = status_version + 1,
    latest_result_id = sqlc.arg(latest_result_id),
    waiting_checkpoint_id = NULL,
    waiting_scope = '',
    terminal_reason = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskCompleted :one
UPDATE orchestration_tasks
SET status = 'completed',
    status_version = status_version + 1,
    latest_result_id = sqlc.arg(latest_result_id),
    terminal_reason = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskFailed :one
UPDATE orchestration_tasks
SET status = 'failed',
    status_version = status_version + 1,
    latest_result_id = sqlc.arg(latest_result_id),
    terminal_reason = sqlc.arg(terminal_reason),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskReadyForRetry :one
UPDATE orchestration_tasks
SET status = 'ready',
    status_version = status_version + 1,
    latest_result_id = sqlc.arg(latest_result_id),
    waiting_checkpoint_id = NULL,
    waiting_scope = '',
    blocked_reason = '',
    terminal_reason = '',
    ready_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskCreatedForResume :one
UPDATE orchestration_tasks
SET status = 'created',
    status_version = status_version + 1,
    latest_result_id = sqlc.arg(latest_result_id),
    waiting_checkpoint_id = NULL,
    waiting_scope = '',
    blocked_reason = '',
    terminal_reason = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationTaskCancelled :one
UPDATE orchestration_tasks
SET status = 'cancelled',
    status_version = status_version + 1,
    waiting_checkpoint_id = NULL,
    waiting_scope = '',
    terminal_reason = sqlc.arg(terminal_reason),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateOrchestrationPlanningIntent :one
INSERT INTO orchestration_planning_intents (
  id,
  run_id,
  task_id,
  checkpoint_id,
  kind,
  status,
  base_planner_epoch,
  payload
) VALUES (
  sqlc.arg(id),
  sqlc.arg(run_id),
  sqlc.arg(task_id),
  sqlc.arg(checkpoint_id),
  sqlc.arg(kind),
  sqlc.arg(status),
  sqlc.arg(base_planner_epoch),
  sqlc.arg(payload)
) RETURNING *;

-- name: GetOrchestrationPlanningIntentByID :one
SELECT *
FROM orchestration_planning_intents
WHERE id = sqlc.arg(id);

-- name: GetDatabaseClockTimestamp :one
SELECT CURRENT_TIMESTAMP;

-- name: ClaimNextOrchestrationPlanningIntent :one
UPDATE orchestration_planning_intents
SET status = 'processing',
    claim_epoch = claim_epoch + 1,
    claim_token = sqlc.arg(claim_token),
    claimed_by = sqlc.arg(claimed_by),
    lease_expires_at = datetime(CURRENT_TIMESTAMP, printf('+%d seconds', sqlc.arg(lease_ttl_seconds))),
    last_heartbeat_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = (
  SELECT id
  FROM orchestration_planning_intents
  WHERE (
      status = 'pending'
      AND (lease_expires_at IS NULL OR lease_expires_at <= CURRENT_TIMESTAMP)
    ) OR (
      status = 'processing'
      AND lease_expires_at IS NOT NULL
      AND lease_expires_at <= CURRENT_TIMESTAMP
    )
  ORDER BY created_at ASC, id ASC
  LIMIT 1
)
RETURNING *;

-- name: HeartbeatOrchestrationPlanningIntent :one
UPDATE orchestration_planning_intents
SET lease_expires_at = datetime(CURRENT_TIMESTAMP, printf('+%d seconds', sqlc.arg(lease_ttl_seconds))),
    last_heartbeat_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'processing'
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: CompleteOrchestrationPlanningIntent :one
UPDATE orchestration_planning_intents
SET status = 'completed',
    claim_token = '',
    claimed_by = '',
    lease_expires_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'processing'
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: FailOrchestrationPlanningIntent :one
UPDATE orchestration_planning_intents
SET status = 'failed',
    failure_reason = sqlc.arg(failure_reason),
    claim_token = '',
    claimed_by = '',
    lease_expires_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'processing'
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: CountActiveOrchestrationPlanningIntentsByRun :one
SELECT COUNT(*)
FROM orchestration_planning_intents
WHERE run_id = sqlc.arg(run_id)
  AND status IN ('pending', 'processing');

-- name: CountFailedOrchestrationPlanningIntentsByRun :one
SELECT COUNT(*)
FROM orchestration_planning_intents
WHERE run_id = sqlc.arg(run_id)
  AND status = 'failed';

-- name: CreateOrchestrationInputManifest :one
INSERT INTO orchestration_input_manifests (
  id,
  run_id,
  task_id,
  captured_task_inputs,
  captured_artifact_versions,
  captured_blackboard_revisions,
  projection_hash
) VALUES (
  sqlc.arg(id),
  sqlc.arg(run_id),
  sqlc.arg(task_id),
  sqlc.arg(captured_task_inputs),
  sqlc.arg(captured_artifact_versions),
  sqlc.arg(captured_blackboard_revisions),
  sqlc.arg(projection_hash)
) RETURNING *;

-- name: GetOrchestrationInputManifestByID :one
SELECT *
FROM orchestration_input_manifests
WHERE id = sqlc.arg(id);

-- name: CreateOrchestrationTaskAttempt :one
INSERT INTO orchestration_task_attempts (
  id,
  run_id,
  task_id,
  attempt_no,
  status,
  input_manifest_id,
  park_checkpoint_id
) VALUES (
  sqlc.arg(id),
  sqlc.arg(run_id),
  sqlc.arg(task_id),
  sqlc.arg(attempt_no),
  sqlc.arg(status),
  sqlc.arg(input_manifest_id),
  sqlc.arg(park_checkpoint_id)
) RETURNING *;

-- name: GetOrchestrationTaskAttemptByID :one
SELECT *
FROM orchestration_task_attempts
WHERE id = sqlc.arg(id);

-- name: GetOrchestrationTaskAttemptByIDForUpdate :one
SELECT *
FROM orchestration_task_attempts
WHERE id = sqlc.arg(id)
;

-- name: ListCurrentOrchestrationTaskAttemptsByRun :many
SELECT *
FROM orchestration_task_attempts
WHERE run_id = sqlc.arg(run_id)
ORDER BY created_at ASC, id ASC;

-- name: AttachOrchestrationTaskAttemptSession :one
UPDATE orchestration_task_attempts
SET session_id = sqlc.arg(session_id), updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateOrchestrationTaskVerification :one
INSERT INTO orchestration_task_verifications (
  id,
  run_id,
  task_id,
  result_id,
  attempt_no,
  verifier_profile,
  status
) VALUES (
  sqlc.arg(id),
  sqlc.arg(run_id),
  sqlc.arg(task_id),
  sqlc.arg(result_id),
  sqlc.arg(attempt_no),
  sqlc.arg(verifier_profile),
  sqlc.arg(status)
) RETURNING *;

-- name: GetOrchestrationTaskVerificationByID :one
SELECT *
FROM orchestration_task_verifications
WHERE id = sqlc.arg(id);

-- name: GetOrchestrationTaskVerificationByIDForUpdate :one
SELECT *
FROM orchestration_task_verifications
WHERE id = sqlc.arg(id)
;

-- name: ListCurrentOrchestrationTaskVerificationsByRun :many
SELECT *
FROM orchestration_task_verifications
WHERE run_id = sqlc.arg(run_id)
ORDER BY created_at ASC, id ASC;

-- name: AttachOrchestrationTaskVerificationSession :one
UPDATE orchestration_task_verifications
SET session_id = sqlc.arg(session_id), updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ClaimNextCreatedOrchestrationTaskVerification :one
UPDATE orchestration_task_verifications
SET status = 'claimed',
    worker_id = sqlc.arg(worker_id),
    executor_id = sqlc.arg(executor_id),
    worker_lease_token = sqlc.arg(worker_lease_token),
    claim_epoch = claim_epoch + 1,
    claim_token = sqlc.arg(claim_token),
    lease_expires_at = datetime(CURRENT_TIMESTAMP, printf('+%d seconds', sqlc.arg(lease_ttl_seconds))),
    last_heartbeat_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = (
  SELECT verifications.id
  FROM orchestration_task_verifications AS verifications
  JOIN orchestration_tasks AS tasks
    ON tasks.id = verifications.task_id
  JOIN orchestration_runs AS runs
    ON runs.id = verifications.run_id
  WHERE verifications.status = 'created'
    AND tasks.status = 'verifying'
    AND tasks.superseded_by_planner_epoch IS NULL
    AND runs.lifecycle_status = 'running'
    AND verifications.verifier_profile IN (SELECT value FROM json_each(sqlc.arg(verifier_profiles)))
  ORDER BY verifications.created_at ASC, verifications.id ASC
  LIMIT 1
)
RETURNING *;

-- name: HeartbeatOrchestrationTaskVerification :one
UPDATE orchestration_task_verifications
SET lease_expires_at = datetime(CURRENT_TIMESTAMP, printf('+%d seconds', sqlc.arg(lease_ttl_seconds))),
    last_heartbeat_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('claimed', 'running')
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: MarkOrchestrationTaskVerificationRunning :one
UPDATE orchestration_task_verifications
SET status = 'running',
    started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'claimed'
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: MarkOrchestrationTaskVerificationCompleted :one
UPDATE orchestration_task_verifications
SET status = 'completed',
    verdict = sqlc.arg(verdict),
    summary = sqlc.arg(summary),
    failure_class = sqlc.arg(failure_class),
    terminal_reason = sqlc.arg(terminal_reason),
    lease_expires_at = NULL,
    finished_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('claimed', 'running')
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: MarkOrchestrationTaskVerificationFailed :one
UPDATE orchestration_task_verifications
SET status = 'failed',
    verdict = sqlc.arg(verdict),
    summary = sqlc.arg(summary),
    failure_class = sqlc.arg(failure_class),
    terminal_reason = sqlc.arg(terminal_reason),
    lease_expires_at = NULL,
    finished_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('claimed', 'running')
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: ReleaseOrchestrationTaskVerificationClaim :one
UPDATE orchestration_task_verifications
SET status = 'created',
    worker_id = '',
    executor_id = '',
    worker_lease_token = '',
    claim_token = '',
    lease_expires_at = NULL,
    last_heartbeat_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'claimed'
  AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: RequeueOrchestrationTaskVerification :one
UPDATE orchestration_task_verifications
SET status = 'created',
    worker_id = '',
    executor_id = '',
    worker_lease_token = '',
    claim_token = '',
    lease_expires_at = NULL,
    last_heartbeat_at = NULL,
    started_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('claimed', 'running')
  AND claim_epoch = sqlc.arg(claim_epoch)
RETURNING *;

-- name: CancelOrchestrationTaskVerification :one
UPDATE orchestration_task_verifications
SET status = 'lost',
    worker_id = '',
    executor_id = '',
    worker_lease_token = '',
    claim_token = '',
    verdict = sqlc.arg(verdict),
    summary = sqlc.arg(summary),
    failure_class = sqlc.arg(failure_class),
    terminal_reason = sqlc.arg(terminal_reason),
    lease_expires_at = NULL,
    last_heartbeat_at = NULL,
    finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('created', 'claimed', 'running')
RETURNING *;

-- name: MarkOrchestrationTaskVerificationLost :one
UPDATE orchestration_task_verifications
SET status = 'lost',
    claim_token = '',
    verdict = sqlc.arg(verdict),
    summary = sqlc.arg(summary),
    failure_class = sqlc.arg(failure_class),
    terminal_reason = sqlc.arg(terminal_reason),
    lease_expires_at = NULL,
    finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('claimed', 'running')
  AND claim_epoch = sqlc.arg(claim_epoch)
RETURNING *;

-- name: ListExpiredOrchestrationTaskVerifications :many
SELECT *
FROM orchestration_task_verifications
WHERE status IN ('claimed', 'running')
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at <= CURRENT_TIMESTAMP
ORDER BY lease_expires_at ASC, id ASC;

-- name: ClaimNextCreatedOrchestrationTaskAttempt :one
UPDATE orchestration_task_attempts
SET status = 'claimed',
    worker_id = sqlc.arg(worker_id),
    executor_id = sqlc.arg(executor_id),
    worker_lease_token = sqlc.arg(worker_lease_token),
    claim_epoch = claim_epoch + 1,
    claim_token = sqlc.arg(claim_token),
    lease_expires_at = datetime(CURRENT_TIMESTAMP, printf('+%d seconds', sqlc.arg(lease_ttl_seconds))),
    last_heartbeat_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = (
  SELECT attempts.id
  FROM orchestration_task_attempts AS attempts
  JOIN orchestration_tasks AS tasks
    ON tasks.id = attempts.task_id
  JOIN orchestration_runs AS runs
    ON runs.id = attempts.run_id
  WHERE attempts.status = 'created'
    AND tasks.status = 'dispatching'
    AND tasks.waiting_checkpoint_id IS NULL
    AND tasks.superseded_by_planner_epoch IS NULL
    AND runs.lifecycle_status = 'running'
    AND tasks.worker_profile IN (SELECT value FROM json_each(sqlc.arg(worker_profiles)))
  ORDER BY attempts.created_at ASC, attempts.id ASC
  LIMIT 1
)
RETURNING *;

-- name: HeartbeatOrchestrationTaskAttempt :one
UPDATE orchestration_task_attempts
SET lease_expires_at = datetime(CURRENT_TIMESTAMP, printf('+%d seconds', sqlc.arg(lease_ttl_seconds))),
    last_heartbeat_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('claimed', 'binding', 'running')
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: MarkOrchestrationTaskAttemptBinding :one
UPDATE orchestration_task_attempts
SET status = 'binding',
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'claimed'
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: MarkOrchestrationTaskAttemptRunning :one
UPDATE orchestration_task_attempts
SET status = 'running',
    started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'binding'
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: MarkOrchestrationTaskAttemptCompleted :one
UPDATE orchestration_task_attempts
SET status = 'completed',
    lease_expires_at = NULL,
    finished_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'running'
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: MarkOrchestrationTaskAttemptFailed :one
UPDATE orchestration_task_attempts
SET status = 'failed',
    failure_class = sqlc.arg(failure_class),
    terminal_reason = sqlc.arg(terminal_reason),
    lease_expires_at = NULL,
    finished_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'running'
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: MarkOrchestrationTaskAttemptParked :one
UPDATE orchestration_task_attempts
SET status = 'parked',
    park_checkpoint_id = sqlc.arg(park_checkpoint_id),
    lease_expires_at = NULL,
    finished_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'running'
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: RetireOrchestrationTaskAttemptFailed :one
UPDATE orchestration_task_attempts
SET status = 'failed',
    failure_class = sqlc.arg(failure_class),
    terminal_reason = sqlc.arg(terminal_reason),
    lease_expires_at = NULL,
    finished_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('claimed', 'binding')
  AND claim_token = sqlc.arg(claim_token)
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: RetireCreatedOrchestrationTaskAttemptFailed :one
UPDATE orchestration_task_attempts
SET status = 'failed',
    failure_class = sqlc.arg(failure_class),
    terminal_reason = sqlc.arg(terminal_reason),
    finished_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'created'
RETURNING *;

-- name: PreemptRunningOrchestrationTaskAttemptFailed :one
UPDATE orchestration_task_attempts
SET status = 'failed',
    failure_class = sqlc.arg(failure_class),
    terminal_reason = sqlc.arg(terminal_reason),
    claim_token = '',
    lease_expires_at = NULL,
    finished_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'running'
  AND claim_epoch = sqlc.arg(claim_epoch)
RETURNING *;

-- name: ReleaseOrchestrationTaskAttemptClaim :one
UPDATE orchestration_task_attempts
SET status = 'created',
    worker_id = '',
    executor_id = '',
    worker_lease_token = '',
    claim_token = '',
    lease_expires_at = NULL,
    last_heartbeat_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('claimed', 'binding')
  AND claim_token = sqlc.arg(claim_token)
RETURNING *;

-- name: MarkOrchestrationTaskAttemptLost :one
UPDATE orchestration_task_attempts
SET status = 'lost',
    claim_token = '',
    terminal_reason = sqlc.arg(terminal_reason),
    lease_expires_at = NULL,
    finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status IN ('claimed', 'binding', 'running')
  AND claim_epoch = sqlc.arg(claim_epoch)
RETURNING *;

-- name: ListExpiredOrchestrationTaskAttempts :many
SELECT *
FROM orchestration_task_attempts
WHERE status IN ('claimed', 'binding', 'running')
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at <= CURRENT_TIMESTAMP
ORDER BY lease_expires_at ASC, id ASC;

-- name: CountActiveOrchestrationTaskAttemptsByRun :one
SELECT COUNT(*)
FROM orchestration_task_attempts
WHERE run_id = sqlc.arg(run_id)
  AND status IN ('created', 'claimed', 'binding', 'running');

-- name: UpsertOrchestrationWorker :one
INSERT INTO orchestration_workers (
  id,
  executor_id,
  display_name,
  capabilities,
  status,
  lease_token,
  last_heartbeat_at,
  lease_expires_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(executor_id),
  sqlc.arg(display_name),
  sqlc.arg(capabilities),
  sqlc.arg(status),
  sqlc.arg(lease_token),
  CURRENT_TIMESTAMP,
  datetime(CURRENT_TIMESTAMP, printf('+%d seconds', sqlc.arg(lease_ttl_seconds)))
) ON CONFLICT (id) DO UPDATE
SET executor_id = EXCLUDED.executor_id,
    display_name = EXCLUDED.display_name,
    capabilities = EXCLUDED.capabilities,
    status = EXCLUDED.status,
    lease_token = EXCLUDED.lease_token,
    last_heartbeat_at = EXCLUDED.last_heartbeat_at,
    lease_expires_at = EXCLUDED.lease_expires_at,
    updated_at = CURRENT_TIMESTAMP
WHERE orchestration_workers.lease_token = EXCLUDED.lease_token
   OR orchestration_workers.lease_expires_at <= CURRENT_TIMESTAMP
RETURNING *;

-- name: HeartbeatOrchestrationWorker :one
UPDATE orchestration_workers
SET status = sqlc.arg(status),
    last_heartbeat_at = CURRENT_TIMESTAMP,
    lease_expires_at = datetime(CURRENT_TIMESTAMP, printf('+%d seconds', sqlc.arg(lease_ttl_seconds))),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND lease_token = sqlc.arg(lease_token)
  AND lease_expires_at > CURRENT_TIMESTAMP
RETURNING *;

-- name: ListOrchestrationWorkersByIDs :many
SELECT *
FROM orchestration_workers
WHERE id IN (SELECT value FROM json_each(sqlc.arg(ids)))
ORDER BY id ASC;

-- name: CreateOrchestrationTaskResult :one
INSERT INTO orchestration_task_results (
  run_id,
  task_id,
  attempt_id,
  status,
  summary,
  failure_class,
  request_replan,
  artifact_intents,
  structured_output
) VALUES (
  sqlc.arg(run_id),
  sqlc.arg(task_id),
  sqlc.arg(attempt_id),
  sqlc.arg(status),
  sqlc.arg(summary),
  sqlc.arg(failure_class),
  sqlc.arg(request_replan),
  sqlc.arg(artifact_intents),
  sqlc.arg(structured_output)
) ON CONFLICT (task_id) DO UPDATE
SET run_id = EXCLUDED.run_id,
    attempt_id = EXCLUDED.attempt_id,
    status = EXCLUDED.status,
    summary = EXCLUDED.summary,
    failure_class = EXCLUDED.failure_class,
    request_replan = EXCLUDED.request_replan,
    artifact_intents = EXCLUDED.artifact_intents,
    structured_output = EXCLUDED.structured_output,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: GetOrchestrationTaskResultByID :one
SELECT *
FROM orchestration_task_results
WHERE id = sqlc.arg(id);

-- name: ListCurrentOrchestrationTaskResultsByRun :many
SELECT *
FROM orchestration_task_results
WHERE run_id = sqlc.arg(run_id)
ORDER BY created_at ASC, id ASC;

-- name: ListCurrentOrchestrationArtifactsByRun :many
SELECT *
FROM orchestration_artifacts
WHERE run_id = sqlc.arg(run_id)
ORDER BY created_at ASC, id ASC;

-- name: ListOrchestrationArtifactsByTask :many
SELECT *
FROM orchestration_artifacts
WHERE task_id = sqlc.arg(task_id)
ORDER BY created_at ASC, id ASC;

-- name: CreateOrchestrationArtifact :one
INSERT INTO orchestration_artifacts (
  id,
  run_id,
  task_id,
  attempt_id,
  kind,
  uri,
  version,
  digest,
  content_type,
  summary,
  metadata
) VALUES (
  sqlc.arg(id),
  sqlc.arg(run_id),
  sqlc.arg(task_id),
  sqlc.arg(attempt_id),
  sqlc.arg(kind),
  sqlc.arg(uri),
  sqlc.arg(version),
  sqlc.arg(digest),
  sqlc.arg(content_type),
  sqlc.arg(summary),
  sqlc.arg(metadata)
) RETURNING *;

-- name: CreateOrchestrationHumanCheckpoint :one
INSERT INTO orchestration_human_checkpoints (
  id,
  run_id,
  task_id,
  blocks_run,
  kind,
  reason_code,
  triggered_by,
  severity,
  planner_epoch,
  status,
  status_version,
  question,
  options,
  default_action,
  resume_policy,
  timeout_at,
  metadata
) VALUES (
  sqlc.arg(id),
  sqlc.arg(run_id),
  sqlc.arg(task_id),
  sqlc.arg(blocks_run),
  sqlc.arg(kind),
  sqlc.arg(reason_code),
  sqlc.arg(triggered_by),
  sqlc.arg(severity),
  sqlc.arg(planner_epoch),
  sqlc.arg(status),
  sqlc.arg(status_version),
  sqlc.arg(question),
  sqlc.arg(options),
  sqlc.arg(default_action),
  sqlc.arg(resume_policy),
  sqlc.arg(timeout_at),
  sqlc.arg(metadata)
) RETURNING *;

-- name: GetOrchestrationHumanCheckpointByID :one
SELECT *
FROM orchestration_human_checkpoints
WHERE id = sqlc.arg(id);

-- name: GetOrchestrationHumanCheckpointByIDForUpdate :one
SELECT *
FROM orchestration_human_checkpoints
WHERE id = sqlc.arg(id)
;

-- name: ListCurrentOrchestrationCheckpointsByRun :many
SELECT *
FROM orchestration_human_checkpoints
WHERE run_id = sqlc.arg(run_id)
ORDER BY created_at ASC, id ASC;

-- name: MarkOrchestrationTaskDependencySuperseded :one
UPDATE orchestration_task_dependencies
SET superseded_by_planner_epoch = sqlc.arg(superseded_by_planner_epoch),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND superseded_by_planner_epoch IS NULL
RETURNING *;

-- name: ResolveOrchestrationHumanCheckpoint :one
UPDATE orchestration_human_checkpoints
SET status = 'resolved',
    status_version = status_version + 1,
    resolved_by = sqlc.arg(resolved_by),
    resolved_option = sqlc.arg(resolved_option),
    resolved_response = sqlc.arg(resolved_response),
    resolved_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationHumanCheckpointCancelled :one
UPDATE orchestration_human_checkpoints
SET status = 'cancelled',
    status_version = status_version + 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: MarkOrchestrationHumanCheckpointSuperseded :one
UPDATE orchestration_human_checkpoints
SET status = 'superseded',
    status_version = status_version + 1,
    superseded_by_planner_epoch = sqlc.arg(superseded_by_planner_epoch),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
  AND status = 'open'
RETURNING *;

-- name: CreateOrchestrationEvent :one
INSERT INTO orchestration_events (
  run_id,
  task_id,
  attempt_id,
  checkpoint_id,
  seq,
  aggregate_type,
  aggregate_id,
  aggregate_version,
  type,
  causation_event_id,
  correlation_id,
  idempotency_key,
  payload
) VALUES (
  sqlc.arg(run_id),
  sqlc.arg(task_id),
  sqlc.arg(attempt_id),
  sqlc.arg(checkpoint_id),
  sqlc.arg(seq),
  sqlc.arg(aggregate_type),
  sqlc.arg(aggregate_id),
  sqlc.arg(aggregate_version),
  sqlc.arg(type),
  sqlc.arg(causation_event_id),
  sqlc.arg(correlation_id),
  sqlc.arg(idempotency_key),
  sqlc.arg(payload)
) RETURNING *;

-- name: ListOrchestrationRunEvents :many
SELECT *
FROM orchestration_events
WHERE run_id = sqlc.arg(run_id)
  AND seq > sqlc.arg(after_seq)
  AND seq <= sqlc.arg(until_seq)
ORDER BY seq ASC
LIMIT sqlc.arg(limit_count);

-- name: ListUnpublishedOrchestrationRunEvents :many
SELECT *
FROM orchestration_events
WHERE published_at IS NULL
ORDER BY run_id ASC, seq ASC
LIMIT sqlc.arg(limit_count)
;

-- name: MarkOrchestrationRunEventPublished :exec
UPDATE orchestration_events
SET published_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id) AND published_at IS NULL;

-- name: CreateOrchestrationProjectionSnapshot :one
INSERT INTO orchestration_projection_snapshots (
  run_id,
  projection_kind,
  seq,
  payload
) VALUES (
  sqlc.arg(run_id),
  sqlc.arg(projection_kind),
  sqlc.arg(seq),
  sqlc.arg(payload)
) RETURNING *;

-- name: GetOrchestrationProjectionSnapshotAtOrBeforeSeq :one
SELECT *
FROM orchestration_projection_snapshots
WHERE run_id = sqlc.arg(run_id)
  AND projection_kind = sqlc.arg(projection_kind)
  AND seq <= sqlc.arg(seq)
ORDER BY seq DESC
LIMIT 1;

-- name: TryCreateOrchestrationIdempotencyRecord :one
INSERT INTO orchestration_idempotency_records (
  tenant_id,
  caller_subject,
  method,
  target_id,
  idempotency_key,
  request_hash,
  state
) VALUES (
  sqlc.arg(tenant_id),
  sqlc.arg(caller_subject),
  sqlc.arg(method),
  sqlc.arg(target_id),
  sqlc.arg(idempotency_key),
  sqlc.arg(request_hash),
  'in_progress'
) ON CONFLICT (tenant_id, caller_subject, method, target_id, idempotency_key) DO NOTHING
RETURNING *;

-- name: GetOrchestrationIdempotencyRecordForUpdate :one
SELECT *
FROM orchestration_idempotency_records
WHERE tenant_id = sqlc.arg(tenant_id)
  AND caller_subject = sqlc.arg(caller_subject)
  AND method = sqlc.arg(method)
  AND target_id = sqlc.arg(target_id)
  AND idempotency_key = sqlc.arg(idempotency_key)
;

-- name: CompleteOrchestrationIdempotencyRecord :one
UPDATE orchestration_idempotency_records
SET state = 'completed',
    response_payload = sqlc.arg(response_payload),
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = sqlc.arg(tenant_id)
  AND caller_subject = sqlc.arg(caller_subject)
  AND method = sqlc.arg(method)
  AND target_id = sqlc.arg(target_id)
  AND idempotency_key = sqlc.arg(idempotency_key)
RETURNING *;
