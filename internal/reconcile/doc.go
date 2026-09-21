// Package reconcile holds the business-agnostic half of a declarative
// reconciler: a loop that claims due rows under a lease and hands them to a
// handler with bounded concurrency, lease renewal for long operations,
// exponential backoff with a fast budget and a slow cadence, keyed in-process
// event fan-out, and a poll-until-final helper.
//
// The first user is internal/botworkspace; the package is shaped so a second
// per-bot intent table (the post-create setup) can reuse it without a copy.
// Nothing here knows what a row means: the store decides which rows are due,
// the handler decides what to do with one.
//
// Broker and Await overlap with internal/agent/decision.Waiter, which pairs a
// one-shot notification with a store poll. A reconciler needs a stream of
// progress events per key plus a separate "is it final yet" poll, so the two
// are kept apart rather than stretching Waiter's one-value contract.
package reconcile
