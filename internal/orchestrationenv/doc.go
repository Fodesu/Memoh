// Package orchestrationenv owns the orchestration env session runtime.
//
// The package layers on top of the durable orchestration_env_* tables
// (see db/postgres/migrations/0082_add_orchestration_env_runtime) and
// the kind-specific runtime backends (container workspace, browser
// gateway, etc.). It exposes a thin Manager surface to the
// orchestration kernel: register resources, acquire sessions, bind
// sessions to tasks/attempts, hold for HITL, snapshot, release, and
// reclaim expired leases.
//
// All session writers carry a (lease_token, lease_epoch) tuple. The
// manager fences out stale writers on every state transition: bumping
// the epoch on resume invalidates the previous holder, mirroring the
// claim_epoch model the orchestration kernel uses for task attempts.
//
// Backends are kind-specific drivers that own actual runtime
// allocation. The contract is small (Allocate / Snapshot / Release)
// so the manager stays portable across container, browser, and future
// desktop/phone runtimes. Backends are registered by Kind and looked
// up at allocation time. A no-op backend ships in this package for
// tests and single-process deployments that have no real env runtime
// wired in yet.
//
// Stage 3-B (this commit) introduces the contract and the no-op
// backend. Stage 3-C/D wire the real container and browser drivers,
// Stage 3-E plumbs the manager through the orchestration kernel
// dispatch path, and Stages 3-F/G/H/I add ledger integration, HITL
// hold/resume, approval tokens, and drift detection respectively.
package orchestrationenv
