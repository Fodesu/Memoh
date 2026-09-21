package sessionruntime

import (
	"context"
	"errors"
	"strings"
	"time"
)

// RecordPersistedTurn publishes the history turn a run has written. The
// application calls it from the persistence path, so the record precedes
// FinishRun and survives into the terminal view: finishRunState and the
// terminal reconcilers only rewrite status and error fields. A run that is no
// longer active or no longer owned by this process cannot be updated; the
// caller logs that outcome and keeps its persisted history regardless.
func (m *Manager) RecordPersistedTurn(ctx context.Context, handle RunHandle, turn PersistedTurnView) error {
	if m == nil || m.backend == nil {
		return nil
	}
	turn.TurnID = strings.TrimSpace(turn.TurnID)
	if turn.TurnID == "" {
		return errors.New("persisted turn id is required")
	}
	turn.RequestMessageID = strings.TrimSpace(turn.RequestMessageID)
	turn.AssistantMessageID = strings.TrimSpace(turn.AssistantMessageID)
	_, _, err := m.updateActiveAndPublish(ctx, handle, func(snapshot Snapshot, now time.Time) (Snapshot, bool, error) {
		run := snapshot.CurrentRunView
		if !runMatchesHandle(run, handle) || !m.runOwnerMatches(run) || !isActiveRunStatus(run.Status) {
			return snapshot, false, ErrRunOwnershipLost
		}
		recorded := turn
		run.PersistedTurn = &recorded
		run.UpdatedAt = now
		snapshot.Seq++
		snapshot.UpdatedAt = now
		return snapshot, true, nil
	}, func(snapshot Snapshot) RuntimeDelta {
		// A patch, not the full view: step-committing runs record a turn on
		// every step, and republishing the whole message stream each time
		// would cost every subscriber O(steps × content).
		run := snapshot.CurrentRunView
		updatedAt := run.UpdatedAt
		recorded := *run.PersistedTurn
		return RuntimeDelta{Run: &CurrentRunPatch{RunID: run.RunID, UpdatedAt: &updatedAt, PersistedTurn: &recorded}}
	})
	return err
}
