# Session input queues

Steer and follow-up are live session-runtime queues. They are deliberately not
part of the PostgreSQL durable model.

## Runtime ownership

The configured `session_runtime.backend` selects the storage implementation:

- `memory` keeps queue state in the process heap. It is fast and has no
  external dependency, but all queue items are lost when the process exits.
- `redis` keeps queue state in Redis using per-session keys and optimistic
  transactions. It is shared by runtime instances, but it is still transient:
  Redis expiry, flush, or loss can discard items.

Both implementations expose the same runtime API. Steer and follow-up remain
separate Go types, methods, and Redis keys; an item from one queue cannot be
read or mutated through the other queue API.

## Queue semantics

An item starts as `accepted`, may be `claimed`, and becomes `applied` after the
runtime consumes it. Cancellation is terminal. Reordering and editing are
allowed only while an item is accepted and pending. Invocation IDs provide
best-effort replay protection for the lifetime of the live queue state; an
accepted result is an acknowledgement from the selected runtime backend, not a
durable receipt.

Steer items capture the active run ID at admission and are claimable only by
that run's owner, generation, and fencing token. Follow-up items capture the
run that was active when they were enqueued. At a terminal boundary the
application claims the next follow-up and starts a normal new turn; applying
the claim is idempotent, and a failed start releases the claim for retry.

## PostgreSQL boundary

PostgreSQL remains authoritative for ordinary run admission, ownership and
fencing, history, user-input/approval state, and other durable application
records. It does not store queue payloads, queue claims, follow-up
continuation provenance, or queue step-commit records. The queue feature was
never added to the canonical schema or migration chain; deployments upgrade
directly from the existing `0145` schema.

## Recovery and availability

Because queues are live state, a process restart with the memory backend (or a
Redis data loss event) may leave no pending item to recover. This is an explicit
availability trade-off for low-latency input handling. Normal run/history
durability and fencing are unaffected. Clients should treat queue errors as
runtime availability errors and retry with a new invocation ID only when the
original result was not observed.
