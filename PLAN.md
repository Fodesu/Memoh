# Orchestration Implementation Plan

## Goal

Land the full RFC vision of the Memoh orchestration layer. The current Postgres kernel is the foundation. The remaining work is to bring NATS event bus, blackboard, EnvSession runtime, resource accounting, and experience layer online so the system matches the architecture described in `RFC.md`.

The orchestration layer should end up able to:

- create durable runs from user goals
- plan/replan into DAGs through planner epochs
- dispatch task-scoped attempts with claim/lease fencing
- ingest worker and env facts through JetStream
- emit committed run events through a durable outbox
- coordinate task-scoped state through a reconstructable blackboard
- lease, snapshot, and recover external environment sessions
- account for resources through quota/ledger/holds
- promote verified outcomes into a long-term experience store
- present all of this through an operator-grade web UI

This plan sequences that work in stages. Each stage is independently shippable and leaves the system in a correct state.

## Current Repo Reality

### Implemented Packages And Entrypoints

- `internal/orchestration`
  - core service
  - run/task/attempt/checkpoint/event state machine
  - planner intent processing
  - scheduler/dispatch functions
  - worker/verifier claim and lease fencing
  - verification runtime
  - executor contract interfaces
  - service, integration, contract, and blackbox tests
- `internal/orchestrationexec`
  - LLM planner
  - LLM replanner
  - LLM worker runtime
  - LLM verifier runtime
  - strict JSON decoders and prompt builders
- `cmd/workerd`
  - always-on worker daemon
  - claims ready attempts
  - heartbeats worker/attempt leases
  - submits completed/failed results
- `cmd/verifyd`
  - always-on verifier daemon
  - claims verification work
  - heartbeats verifier leases
  - submits verification decisions
- `internal/agent/tools/orchestration.go`
  - user bot tool integration for starting/querying/cancelling orchestration runs
- `internal/handlers/orchestration.go`
  - HTTP API used by frontend and tests
- `apps/web/src/pages/orchestration`
  - orchestration page, DAG view, run selector, inspector, thinking/activity, outputs, checkpoints

There is no `cmd/orchestrator` in this branch. The server process owns orchestration control-plane state, while `workerd` and `verifyd` are separate execution daemons.

### Implemented Data Model

Current durable tables cover:

- `orchestration_runs`
- `orchestration_planning_intents`
- `orchestration_tasks`
- `orchestration_task_dependencies`
- `orchestration_task_attempts`
- `orchestration_task_results`
- `orchestration_input_manifests`
- `orchestration_events`
- `orchestration_artifacts`
- `orchestration_workers`
- `orchestration_task_verifications`
- `orchestration_human_checkpoints`
- `orchestration_projection_snapshots`
- `orchestration_idempotency_records`
- `orchestration_action_ledger`

Current durable model does not yet include:

- `orchestration_env_resources`
- `orchestration_env_sessions`
- `orchestration_env_bindings`
- `orchestration_env_snapshots`
- `orchestration_resource_quotas`
- `orchestration_resource_ledger`
- `orchestration_resource_holds`
- `orchestration_experience_records`

## Confirmed Decisions

- Postgres remains the only authoritative orchestration state source. NATS, blackboard, and artifact stores are delivery and projection layers, not truth.
- `workerd` and `verifyd` stay as always-on Docker Compose services through Stage 1. On-demand scheduling can be revisited after JetStream consumers are in place.
- NATS JetStream becomes the runtime delivery backbone in Stage 1. Existing Compose `nats` service is repurposed as the actual event bus.
- NATS KV blackboard becomes the runtime shared view in Stage 2. It must be reconstructable from Postgres at any time.
- EnvResource/EnvSession/EnvBinding/EnvSnapshot become first-class state in Stage 3, not ad-hoc fields on tasks.
- Resource quotas, ledger, and holds become authoritative in Stage 4 before any non-trivial multi-tenant capacity work.
- Experience records, promotion pipeline, and retrieval hooks become real in Stage 5.
- Frontend visibility for each stage's new state is part of that stage's done criteria, not a separate effort.

## Current Runtime Model

```mermaid
flowchart TB
    Bot[User Bot] --> Tool[orchestration tool]
    Tool --> API[HTTP handlers]
    API --> Service[internal/orchestration Service]
    Service --> PG[(Postgres)]

    Service --> Planner[orchestrationexec planner/replanner]
    Workerd[cmd/workerd] --> Service
    Workerd --> WorkerRuntime[orchestrationexec worker runtime]
    Verifyd[cmd/verifyd] --> Service
    Verifyd --> VerifierRuntime[orchestrationexec verifier runtime]

    Planner --> Agent[existing Agent runtime]
    WorkerRuntime --> Agent
    VerifierRuntime --> Agent

    Service -.committed events.-> Outbox[orchestrationoutbox dispatcher]
    Outbox -.run.event.*.-> Bus[(NATS JetStream)]
    Workerd -.attempt.fact.*.-> Bus
    Verifyd -.verification.fact.*.-> Bus
    Bus -.WatchRun subscribe.-> Service
    Bus -.attempt.fact.* subscribe.-> Facts[orchestrationfacts consumer]
    Facts -.validate.-> PG

    Web[Web orchestration UI] --> API
```

The dotted edges are the Stage 1 surface: Postgres still owns truth, but the outbox and the daemons publish through the bus so live consumers (`WatchRun`, future fact consumer) do not have to poll.

## State Machines To Preserve

### Run

- `created`
- `running`
- `waiting_human`
- `cancelling`
- terminal: `completed`, `failed`, `cancelled`

`planning_status`:

- `idle`
- `active`

### Task

- `created`
- `ready`
- `dispatching`
- `running`
- `verifying`
- `waiting_human`
- terminal: `completed`, `failed`, `blocked`, `cancelled`

### Attempt

- `created`
- `claimed`
- `running`
- terminal: `completed`, `failed`, `timed_out`, `cancelled`, `lost`

### Verification

- claim/start/finalize flow is fenced like attempts.
- verifier decisions map to pass/retry/replan/fatal style behavior.

### HumanCheckpoint

- `open`
- `resolved`
- `timed_out`
- `cancelled`
- `superseded`

Timeout rules:

- `timeout_at` requires a valid `default_action`.
- timeout recovery marks the checkpoint `timed_out`
- timeout recovery emits `run.event.hitl.timed_out`
- timeout recovery enqueues `checkpoint_resume`

## Completed Work

### Kernel

- Durable schema and `sqlc` integration.
- Root run creation with eager root task.
- Task dependencies and topological dispatch.
- Planning intents:
  - `start_run`
  - `checkpoint_resume`
  - `attempt_finalize`
  - `replan`
- Planner epoch and append/supersede semantics.
- Idempotency records for public control paths.
- Projection snapshots used by current read APIs/UI.

### Planner / Replanner

- LLM planner for `start_run`.
- LLM replanner for:
  - `InjectRunHint(replan_request)`
  - worker `request_replan`
  - verifier replan requests
- Strict output contract:
  - unknown keys rejected
  - legacy aliases rejected
  - empty replacement plans rejected
  - invalid priorities rejected
  - DAG validation before persistence
- Runtime limits through `control_policy.runtime_limits`:
  - child tasks per plan
  - dependency edges per plan
  - total tasks per run
  - replans per run
  - replan depth
  - task goal length

### Executor / Worker / Verifier

- Worker registration and heartbeat.
- Attempt claim/start/heartbeat/finalize.
- Verification claim/start/finalize.
- Claim token and claim epoch fencing.
- Lease expiry recovery.
- Retry policy integration.
- Completion replay protection.
- Replay conflict surfaced as non-retryable to daemons.
- `workerd` / `verifyd` run as separate Compose services.

### HITL

- Create checkpoint.
- Resolve checkpoint.
- Checkpoint resume planning intent.
- Timeout/default-action recovery.
- Integration test coverage for timeout/default-action resume.

### Frontend

- Orchestration page exists and is navigable.
- DAG is custom, not VueFlow.
- Root task is shown as L0.
- Child tasks are topologically layered.
- Node inspector can show thinking/activity, task details, inputs, outputs, logs, checkpoints, artifacts.
- Stop run UI exists.
- Many internal/debug strings have been hidden or localized.
- Unused VueFlow components and dependency have been removed.

### Tests

Current coverage includes:

- service/unit tests for state-machine and contract behavior
- integration tests with PostgreSQL
- blackbox tests through HTTP and independent `workerd` / `verifyd`
- planner/replanner decoder tests
- LLM worker/verifier runtime tests
- migration up/down checks in CI

## Stage 0. Stabilize The Current Kernel

The existing Postgres kernel is the substrate every later stage depends on. Before adding JetStream or blackboard, lock down recovery, planner hardening, HITL semantics, and UI honesty so later stages do not paper over current bugs.

### 0.1 Executor And Recovery Closure

- Expand crash and lease-loss tests around:
  - completion commit failed after worker exit
  - ack lost after DB commit
  - worker restarted with same attempt context
  - worker submits after lease expiry
  - verifier equivalents
- Make recovery distinguish four classes explicitly: safe replay, retry budget available, hard replay conflict, stale writer.
- Keep `workerd` and `verifyd` retry loops bounded; replay conflicts stop locally without spinning.
- Add code comments to executor contract types so external executors can later be plugged in without re-reading state machine internals.

Done when stale writers cannot mutate terminal state, safe replay does not duplicate results or artifacts, and retry budget consistently produces the next attempt.

### 0.2 Planner / Replanner Hardening

- Add tests for: dependency cycle, unknown alias reference, over-limit total task count, over-limit replan count, over-limit replan depth, planner failure paths, and non-root replan failure isolation.
- Confirm planner/replanner prompt language matches user-visible result language.
- Keep prompts schema-first; remove any unused or aspirational fields.

Done when invalid planner output always fails the planning intent cleanly, no invalid DAG rows are persisted, and UI never has to render a raw planner payload by default.

### 0.3 HITL Barrier Semantics

- Lock down task-level vs run-level checkpoint behavior.
- Add tests for multiple concurrent checkpoints in a single run.
- Add tests for resolving stale or superseded checkpoints.
- Implement run-wide retirement barrier so siblings are properly parked or cancelled when `blocks_run = true`.
- Persist the chosen barrier source through `(planner_epoch, id)` ordering so recovery is deterministic.

Done when run-wide checkpoints behave per `RFC.md` invariants, and any sibling attempts are retired before the run is considered paused.

### 0.4 Observability And UI Honesty

- Normalize event/activity summaries for human reading.
- Keep activity rows short, expandable for details.
- Show tool and action result summaries before raw JSON.
- Distinguish task output, attempt logs, verifier notes, checkpoints, and artifacts in the inspector.
- Hide internal booleans and backend enums behind translated labels by default.

Done when a reviewer can read run state from the UI without consulting server logs.

### 0.5 Stabilization Hygiene

- Backend regression: `go test ./cmd/workerd ./cmd/verifyd ./internal/orchestration ./internal/orchestrationexec -count=1` plus DB-backed integration/blackbox tests with `TEST_POSTGRES_DSN`.
- Migration up/down both pass.
- Frontend builds in a clean `pnpm` environment.
- Generated artifacts (`sqlc`, swagger, SDK) are consistent with code.
- No unrelated local files (`.codex`, blackbox bin directories, backup tarballs) are committed.

## Stage 1. NATS Event Bus

Goal: separate fact ingress from committed run events and make `attempt.fact.*` and `run.event.*` first-class delivery channels, while keeping Postgres authoritative.

### 1.1 Stream Layout

- [x] Provision JetStream streams for:
  - `memoh.orch.run.event.*` (committed outbox)
  - `memoh.orch.attempt.fact.*` (worker/verifier fact ingress)
- [x] Subject helpers in `internal/orchestrationbus/subjects.go` build per-run / per-attempt subjects so consumers can filter without parsing payloads.
- [x] Stream defaults: file storage, `LimitsPolicy`, 24h retention for run events, 1h for attempt facts, MsgID-based deduplication windows.
- [x] Subject constants, envelope schemas, and bus interface live in `internal/orchestrationbus`. The package ships an `InMemoryBus` (single-process / tests) and a `JetStreamBus`.
- [ ] Pending: `env.fact.*` and `artifact.intent.*` streams ship with Stage 3.

### 1.2 Outbox Dispatcher

- [x] `internal/orchestrationoutbox` polls `orchestration_events WHERE published_at IS NULL`, publishes each row through the bus, and stamps `published_at` once accepted.
- [x] Partial index `idx_orchestration_events_unpublished` (`db/postgres/migrations/0081_*`) keeps the scan cheap.
- [x] JetStream `WithMsgID(event_id)` means redelivery after a crash never duplicates events for subscribers.
- [x] `Service.SetEventCommittedHook(dispatcher.Notify)` is registered in `cmd/agent`; the dispatcher's `Notify()` channel is exercised by tests. Wiring `notifyEventCommitted()` into every kernel commit path is tracked as a small follow-up; the polling fall-back already keeps publish latency under one tick.

### 1.3 Fact Ingress

- [x] `cmd/workerd` connects to the configured bus and emits `attempt.claimed`, `attempt.started`, `attempt.start_failed`, `attempt.completed`, `attempt.failed`, `attempt.lost` envelopes per attempt lifecycle.
- [x] `cmd/verifyd` emits the equivalent `verification.*` facts.
- [x] Both daemons fall back to the in-process bus when `[nats].url` is empty so single-process tests keep working.
- [x] `internal/orchestrationfacts` runs a kernel-side consumer that subscribes globally to every `attempt.fact.*` envelope, validates `claim_epoch` / `claim_token` against Postgres, and surfaces structured outcomes (`accepted`, `stale`, `orphan`, `mismatch`, `invalid`) for operators. The consumer is observe-only at this stage; Postgres remains the authoritative state machine, but the validator is in place so the next iteration can promote it to a control-plane writer without changing the envelope schema.
- [ ] Pending: have the kernel apply observed facts as state transitions instead of the daemons committing through direct service calls. This is gated on Stage 2 blackboard CAS so the control path can move off the direct daemon → kernel calls without losing fencing guarantees.

### 1.4 Public Event API Refactor

- [x] `WatchRun` subscribes to the bus (`orchestrationbus.Bus.SubscribeRunEvents`) when one is configured. The handler subscribes before reading the snapshot so live events cannot slip past the backfill window.
- [x] After backfill the watcher forwards bus envelopes, dedupes against `afterSeq`, and reconciles against Postgres every 5s so a transient bus interruption never silently strands a subscriber.
- [x] Polling fall-back kept for deployments where `[nats].url` is empty.
- [x] Web UI consumes `/orchestration/runs/{run_id}/watch` over fetch-based SSE (`apps/web/src/pages/orchestration/composables/use-run-event-stream.ts`). Each committed event triggers a 250ms-debounced inspector refetch; the legacy poll loop drops to a 5s safety-net interval that only fires when the stream is not in `open` state. Reconnect uses an exponential backoff capped at 8s and resumes from the last seen `seq`.

### 1.5 Tests And Operations

- [x] Unit tests for the bus (`internal/orchestrationbus/inmem_test.go`, `subjects_test.go`) and the dispatcher (`internal/orchestrationoutbox/outbox_test.go` covering happy path, malformed-row poison-pill, publish failure, notify wakeup).
- [x] Integration test `TestIntegrationWatchRunDeliversEventsThroughBusOutbox` runs a real Postgres + the dispatcher + an `InMemoryBus` and asserts that `WatchRun` delivers events end-to-end and the dispatcher drains `published_at` to zero.
- [ ] Pending: integration test against an actual JetStream instance (Docker compose `nats` already exposes one), and a blackbox test that exercises a full run with `workerd` / `verifyd` reaching the kernel only via bus once the server-side fact consumer lands.
- [ ] Pending: short operational doc covering stream inspection, draining stuck consumers, and replaying from a `seq`.

Done when `workerd`/`verifyd` drive runtime through JetStream, run timelines are reproducible from Postgres replay, and JetStream loss only delays delivery without changing committed truth.

## Stage 2. Blackboard

Goal: introduce a reconstructable shared runtime view with explicit ownership and CAS rules, and remove the implicit pattern of stuffing runtime state into ad-hoc rows.

### 2.1 Schema And Namespaces

- Define KV layout:
  - `bb.run.{run_id}` for run-scoped values
  - `bb.task.{task_id}` for task-scoped values
- Encode each value with `schema_version`, `writer_type`, `writer_id`, `attempt_id`, `claim_epoch`, `updated_at`, and `persistence_class` per `RFC.md`.
- Lock down keyspaces: `context.*`, `plan.*`, `deps.*`, `progress.*`, `result.*`, `artifacts.*`, `verifier.*`, `human.*`.

### 2.2 Writer Ownership And CAS

- Workers can only write their task scope.
- Verifiers can only write the verifier namespace.
- Orchestrator can write all scopes.
- All `result.*` writes must use KV revision CAS and carry the current `claim_epoch`.
- Stale writers (lost lease, superseded planner epoch, terminal task) are rejected at the blackboard layer, not just at Postgres.

### 2.3 Frozen Input Manifest Integration

- Dispatch must produce an `InputManifest` that captures blackboard keys and revisions, artifact refs and digests, and env preconditions.
- `TaskSpec` carries `InputProjection` plus `InputManifest`.
- Worker correctness logic only depends on `InputProjection`. Live blackboard reads are advisory.
- Verifier replay uses the same `InputManifest` to reconstruct inputs.

### 2.4 Rebuild Path

- Implement a deterministic `rebuild_blackboard(run_id)` that recomputes blackboard contents from `orchestration_runs`, `orchestration_tasks`, `orchestration_task_results`, `orchestration_artifacts`, and verifier notes.
- Add a CLI subcommand or admin endpoint that triggers rebuild.
- Recovery on orchestrator/server restart never assumes blackboard contents are intact; if KV is missing, rebuild from Postgres.

### 2.5 Tests

- Unit tests for KV CAS rules and stale-writer rejection.
- Integration tests for dispatch with frozen `InputManifest`.
- Blackbox test that wipes KV mid-run and confirms rebuild restores progress.

Done when blackboard is observable, writer ownership is enforced, and the system tolerates KV loss without losing committed state.

## Stage 3. Env Session Runtime

Goal: bring external environment state (browser context, desktop, phone, container) into a first-class lease/snapshot model so tasks can resume across attempts and side effects can be audited.

### 3.1 Schema

- Add tables and `sqlc` queries for:
  - `orchestration_env_resources`
  - `orchestration_env_sessions`
  - `orchestration_env_lease_reservations`
  - `orchestration_env_bindings`
  - `orchestration_env_snapshots`

### 3.2 Env Manager Interface

- Add `internal/env` with a backend-neutral `EnvManager` per `RFC.md`.
- Provide initial backends:
  - `internal/env/container` (current containerd workspace as an EnvResource)
  - `internal/env/browser` (browser gateway sessions)
- Implement reserve/commit/abort, bind, hold, snapshot, reset, release.
- Plumb env fences (`env_lease_epoch`, `env_lease_token`) through worker writes and side-effect execution.

### 3.3 Action Ledger Wiring

- Extend `orchestration_action_ledger` writes to be the authoritative log for env actions.
- Classify actions by `effect_class`: env_local_mutation, external_read, external_write, external_irreversible.
- Snapshot before/after env state for non-trivial actions.

### 3.4 HITL And Approval Tokens

- Hold env sessions across checkpoint waits when `resume_policy.resume_mode = resume_held_env`.
- Side-effect approval tokens bind to `(attempt_id, claim_epoch, env_session_id, env_lease_epoch)`.
- Enforce that `external_irreversible` actions require an approval token plus matching fences.

### 3.5 Drift Detection

- Workers periodically observe env snapshots.
- Verifier compares before/after snapshots against task expectations.
- Drift outcomes feed into retry/replan/HITL policy.

Done when an interactive task can pause for human input, hold its env session, resume in a new attempt, and side effects are audit-traceable.

## Stage 4. Resource Accounting

Goal: replace implicit capacity assumptions with explicit quota, ledger, and admission control so the system can be operated with predictable headroom.

### 4.1 Schema

- Add tables and `sqlc` queries for:
  - `orchestration_resource_quotas`
  - `orchestration_resource_ledger`
  - `orchestration_resource_holds`
  - `orchestration_artifact_reservations`

### 4.2 Admission

- Scheduler admission consults quota, current holds, and worker profile concurrency before claim.
- Held env sessions count toward owner quota until released or reclaimed.
- Tasks blocked on admission stay `ready` and enter an admission queue.

### 4.3 Reclaim And Fairness

- Implement reclaim for: idle reservations, held env sessions over TTL, parked attempts beyond hold TTL.
- Default fairness uses per-tenant weighted scheduling, then priority/age within tenant.
- All reclaim/expire/preempt events write to `orchestration_resource_ledger`.

### 4.4 Tests

- Property-style tests for ledger invariants (sum of holds equals active reservations).
- Integration tests for admission under contention.
- Blackbox test that runs hit quota and recover after release.

Done when capacity decisions are auditable from `orchestration_resource_ledger` alone.

## Stage 5. Experience Layer

Goal: turn verified outcomes and useful failure recoveries into a structured experience store that planner, scheduler, worker, and verifier can consult.

### 5.1 Schema

- Add tables for `orchestration_experience_records` and `orchestration_experience_feedback`.
- Define structured fields per `RFC.md`: `kind`, `scope`, `worker_profile`, `evidence_refs`, `structured_data`, `confidence`, `verified`, `version`.

### 5.2 Promotion Pipeline

- Implement extractor, verifier, deduper, scorer, and promoter stages over completed runs.
- Inputs: verified artifacts, run summary, failure/retry records, verifier outputs, human checkpoint resolutions.
- Output: promotable experience records with evidence references back to the source run.

### 5.3 Retrieval Hooks

- Planner consults experience for decomposition templates and worker profile recommendations.
- Scheduler consults experience for timeout/retry tuning.
- Verifier consults experience for known-good and known-bad signatures.

### 5.4 Governance

- Tool persistence policy (`ToolPersistencePolicy`) enforced at tool registration.
- `external_side_effect` outputs never auto-promote.
- Versioning, retention, and rollback for experience entries.

Done when re-running a similar goal produces measurable benefit from the experience store and degraded entries can be pruned without losing audit trail.

## Test Matrix

Each stage extends this matrix. Earlier stages must keep passing as later stages land.

### Unit

- planner decoder strictness
- replanner decoder strictness
- DAG validation
- runtime limits
- retry policy
- status transition helpers
- idempotency canonicalization
- outbox dispatcher idempotency and replay (Stage 1)
- blackboard CAS and stale writer rejection (Stage 2)
- env lease fencing and approval token binding (Stage 3)
- resource ledger invariants (Stage 4)
- experience promotion gates (Stage 5)

### Integration

- migration up/down
- checkpoint resolve/timeout
- planning intent recovery
- task dispatch/retry
- verification outcomes
- projection snapshot reads
- JetStream-backed fact ingress and committed event delivery (Stage 1)
- blackboard rebuild from Postgres (Stage 2)
- env reserve/commit/abort and snapshot capture (Stage 3)
- admission under contention and reclaim of stale holds (Stage 4)
- experience retrieval through planner/scheduler/verifier paths (Stage 5)

### Blackbox / E2E

- `StartRun -> plan -> dispatch -> workerd -> verifyd -> completed`
- worker failure -> replan -> replacement task -> completed
- verifier rejection -> replan -> completed
- checkpoint pause -> resolve -> resume
- checkpoint timeout -> default action -> resume
- worker crash / lease expiry -> retry or recover
- ack-loss replay for worker/verifier
- run completes with `workerd`/`verifyd` driven only via JetStream (Stage 1)
- run completes after blackboard KV is wiped mid-run (Stage 2)
- interactive task pauses, holds env session, resumes across attempts (Stage 3)
- run blocks on quota then resumes after release (Stage 4)
- repeated similar runs benefit from experience records (Stage 5)
- UI reads run/tasks/checkpoints/artifacts/events through public APIs at every stage

## Resource Expectations

Current always-on cost:

- `server`
- `workerd`
- `verifyd`
- `web`
- Postgres
- NATS JetStream (becomes a runtime dependency in Stage 1)
- optional `qdrant`/`sparse`/`browser`

`workerd` and `verifyd` are around 70-80 MB RSS each. That is Go daemon idle overhead from config, DB pool, model/runtime wiring, and container/workspace deps, not per-task worker memory. After Stage 1, JetStream-driven wakeups will reduce CPU spend during idle, but RSS stays similar. RSS reduction would require process consolidation or on-demand supervision and is not in this plan.

NATS adds modest baseline cost when used at scale: one JetStream stream per subject family with default replicas, plus durable consumer state. This stays well within typical dev/staging budgets.

## Stage Completion Checklists

Stage 0:

- [ ] backend regression and DB integration/blackbox tests pass
- [ ] migration up/down both pass
- [ ] frontend build passes in a clean `pnpm` environment
- [ ] orchestration page is usable on restored dev data
- [ ] HITL barrier semantics match `RFC.md` invariants

Stage 1:

- [x] JetStream streams provisioned with documented retention/dedup window in `internal/orchestrationbus/jetstream.go`
- [x] outbox dispatcher publishes committed events idempotently and stamps `published_at` per row
- [x] `WatchRun` subscribes to the bus before reading the snapshot and reconciles against Postgres on a 5s tick
- [x] replay-from-`seq` works for late subscribers via Postgres backfill plus bus dedupe on `afterSeq`
- [x] kernel-side fact consumer in `internal/orchestrationfacts` validates `attempt.fact.*` envelopes against Postgres and emits structured outcomes for operators
- [x] live UI thinking/activity stream consumes SSE-backed `WatchRun` (`apps/web/src/pages/orchestration/composables/use-run-event-stream.ts`) and falls back to a 5s poll only when the bus is unavailable
- [ ] `workerd` / `verifyd` drive control transitions through the bus instead of direct service calls (gated on Stage 2 blackboard CAS so fencing semantics survive the move)

Stage 2:

- [ ] blackboard KV layout matches `RFC.md`
- [ ] CAS rules and stale writer rejection are enforced
- [ ] dispatch produces frozen `InputManifest`
- [ ] `rebuild_blackboard(run_id)` succeeds for all states

Stage 3:

- [ ] env tables and `sqlc` queries shipped
- [ ] `EnvManager` interface and at least two backends in place
- [ ] action ledger captures classified env actions
- [ ] HITL hold/resume works for held env sessions
- [ ] approval tokens enforced for irreversible actions

Stage 4:

- [ ] resource tables and `sqlc` queries shipped
- [ ] admission consults quota and current holds
- [ ] reclaim/expire writes to ledger
- [ ] fairness behavior under contention is testable

Stage 5:

- [ ] experience tables and `sqlc` queries shipped
- [ ] promotion pipeline gates by verification status
- [ ] planner/scheduler/verifier consult experience
- [ ] retention and rollback supported

## Next Step

Where we are right now:

1. Stage 0 stabilization (executor recovery, planner hardening, HITL barriers, observability, hygiene) is the substrate the rest of this plan rides on. Keep its tests green as later stages land.
2. Stage 1 (NATS event bus) is partly landed: subject contract, in-memory + JetStream bus, committed-event outbox, bus-backed `WatchRun`, and fact emission from `workerd`/`verifyd` are all in. The remaining 1.x bullets are the server-side fact consumer, the SSE-driven UI stream, and the JetStream-backed blackbox test.
3. Stage 2 (blackboard) is the next net-new stage once the Stage 1 follow-ups settle: schema/namespaces, CAS, frozen `InputManifest`, and the deterministic rebuild path.
4. Stages 3, 4, and 5 follow in order. Every earlier stage's tests must keep passing as later stages land.

Each stage ends with docs and PR descriptions updated to reflect actual landed behavior, not target state.
