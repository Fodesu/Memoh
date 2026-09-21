// When a retry or edit fails, the store decides whether history must be
// reloaded. Two failures need it: the server refused the replacement as stale,
// or the run streamed output and then failed without writing its round.

// The server refused the replacement because the client's picture of the
// tail is stale: the named turn is no longer the latest, or it has no reply
// on record. The only repair is to reload history; the composer keeps its
// text and shows the code's message.
const STALE_TURN_ERROR_CODES = new Set([
  'session_runtime.turn_not_latest',
  'session_runtime.turn_incomplete',
])

export function isStaleTurnErrorCode(code: string | undefined): boolean {
  return Boolean(code && STALE_TURN_ERROR_CODES.has(code))
}

// A replacement run that streamed output and then failed without writing its
// round leaves the screen showing the failed reply while history still holds
// the tail it was meant to replace. Nothing on screen can be retried or
// edited (the failed turn was never written), so history has to be reloaded
// for the old tail to come back. The run view rides on the failure as its
// feedback; any other feedback shape is left to the code-based path.
export function runLeftNoHistory(feedback: unknown): boolean {
  if (!feedback || typeof feedback !== 'object') return false
  const run = feedback as { run_id?: unknown, status?: unknown, persisted_turn?: unknown }
  return typeof run.run_id === 'string'
    && typeof run.status === 'string'
    && run.status !== 'completed'
    && !run.persisted_turn
}
