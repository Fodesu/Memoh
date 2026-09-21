// Package reconcile holds the business-agnostic half of a declarative
// reconciler: a loop that claims due rows under a lease and hands them to a
// handler with bounded concurrency, lease renewal for long operations,
// exponential backoff with a fast budget and a slow cadence, keyed in-process
// event fan-out, and a poll-until-final helper.
//
// The first user is internal/botworkspace; internal/botsetup builds on the
// same primitives. Nothing here knows what a row means: the store decides
// which rows are due, the handler decides what to do with one.
package reconcile
