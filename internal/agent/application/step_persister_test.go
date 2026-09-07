package application

import (
	"context"

	messagepkg "github.com/felinics/memoh/internal/chat/message"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

// recordingStepPersister is shared by subagent step tests. Queue-specific
// coordinator tests were removed; this helper only exercises ordinary history
// step persistence.
type recordingStepPersister struct {
	*recordingMessageService
	steps []messagepkg.AgentStep
}

func (s *recordingStepPersister) PersistAgentStep(_ context.Context, step messagepkg.AgentStep) ([]messagepkg.Message, error) {
	s.steps = append(s.steps, step)
	result := make([]messagepkg.Message, len(step.Messages))
	for i, input := range step.Messages {
		result[i] = messagepkg.Message{ID: "committed", Role: input.Role, BotID: input.BotID, SessionID: input.SessionID, Metadata: input.Metadata, Content: input.Content}
	}
	return result, nil
}

func (s *recordingStepPersister) PersistAgentStepTx(ctx context.Context, _ dbstore.Queries, step messagepkg.AgentStep) ([]messagepkg.Message, error) {
	return s.PersistAgentStep(ctx, step)
}

func (s *recordingStepPersister) PersistAgentReplacementStepTx(ctx context.Context, _ dbstore.Queries, step messagepkg.AgentStep) ([]messagepkg.Message, error) {
	return s.PersistAgentStep(ctx, step)
}

func (*recordingStepPersister) FinalizeAgentReplacementTx(context.Context, dbstore.Queries, string, messagepkg.TurnReplacement, string, string) error {
	return nil
}
