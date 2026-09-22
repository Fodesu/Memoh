// Package toolexec runs the tool calls a model makes and assembles the step
// messages that carry them. It is the tool-execution surface Twilight removed
// from its SDK in felinics/twilight#53 (commit 391b059): the SDK stops at
// sdk.ToolDefinition, sdk.ToolCall, sdk.ToolArguments and sdk.ToolOutput, and
// how a call is found, approved, run and written back is the caller's own
// decision.
//
// The code is copied from twilight's sdk package at commit 44a22e5, the last
// revision before the removal, with the package name changed and the SDK types
// it still uses referenced through the sdk import. Behaviour is unchanged:
// approvals resolve sequentially in call order, approved tools run in parallel,
// a deferred approval keeps the results computed before it, and a call whose
// arguments are not a JSON document is answered to the model without running.
//
// Two fields deviate from the copy: ToolApprovalResult.Metadata and
// ToolApprovalRequestPart.Metadata stay map[string]any. Memoh's approval
// handler carries structured records there (the ask_user UI payload, the
// operation summary, permission options, the execution location) that its
// event stream and decision store consume as JSON values; the SDK's closing of
// that field to strings happened after the executor left it.
//
// adapter.go is Memoh's own: it bridges handlers written against the earlier
// untyped contract (input any, output any) onto the typed one, and converts
// between the typed argument/output values and the plain JSON values Memoh's
// events, decisions and history rows carry.
package toolexec
