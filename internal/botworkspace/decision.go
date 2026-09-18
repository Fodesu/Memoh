package botworkspace

import "time"

// Action is what the reconciler does with a claimed row.
type Action int

const (
	// ActionNone means observation already matches intent; the reconciler only
	// records that the generation is caught up.
	ActionNone Action = iota
	// ActionProvision brings the workspace to running.
	ActionProvision
	// ActionTeardown removes the workspace.
	ActionTeardown
	// ActionWait means the row is not due (backoff still running).
	ActionWait
)

func (a Action) String() string {
	switch a {
	case ActionProvision:
		return "provision"
	case ActionTeardown:
		return "teardown"
	case ActionWait:
		return "wait"
	default:
		return "none"
	}
}

// Decide compares intent with observation. It looks only at the present state,
// never at history, so an interrupted pass (lease expired mid-provisioning)
// simply continues from what the backend reports.
func Decide(w Workspace, now time.Time) Action {
	switch w.Desired {
	case DesiredAbsent:
		if w.Observed == ObservedAbsent {
			return ActionNone
		}
		return ActionTeardown
	default:
		switch w.Observed {
		case ObservedRunning, ObservedStopped:
			return ActionNone
		case ObservedFailed:
			if w.NextAttemptAt.After(now) {
				return ActionWait
			}
			return ActionProvision
		default:
			return ActionProvision
		}
	}
}

// MayDeleteData is the single authorization rule for destroying a workspace
// that carries data. It is deliberately narrow:
//
//  1. The user asked for the workspace to be absent (bot deleted, workspace
//     deleted). preserve_data is honoured by the teardown itself.
//  2. The workspace never became ready and no preserved-data archive exists,
//     so a half-provisioned container can be replaced before retrying.
//
// Everything else keeps the container: a workspace that was ready once is
// reused or repaired, never recreated behind the user's back.
func MayDeleteData(w Workspace, hasPreservedData bool) bool {
	if w.Desired == DesiredAbsent {
		return true
	}
	return !w.EverReady && !hasPreservedData
}

// NextBackoff returns when the next attempt may run after attempt number
// `attempts` (1-based) failed. Exponential from base, capped.
func NextBackoff(now time.Time, attempts int32, base, capDuration time.Duration) time.Time {
	if attempts < 1 {
		attempts = 1
	}
	d := base
	for i := int32(1); i < attempts && d < capDuration; i++ {
		d *= 2
	}
	if d > capDuration {
		d = capDuration
	}
	return now.Add(d)
}

// farFuture parks a row that must not be retried until the intent changes.
var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// DeriveBotStatus maps the workspace state onto bots.status. ok is false when
// the bot's status must not be touched (the workspace is absent on purpose or
// the bot is being deleted; those transitions belong to the bot lifecycle).
func DeriveBotStatus(w Workspace) (string, bool) {
	if w.Desired == DesiredAbsent {
		return "", false
	}
	switch w.Observed {
	case ObservedRunning, ObservedStopped:
		return BotStatusReady, true
	case ObservedFailed:
		if w.EverReady {
			return BotStatusReady, true
		}
		return BotStatusFailed, true
	default:
		if w.EverReady {
			return BotStatusReady, true
		}
		return BotStatusCreating, true
	}
}
