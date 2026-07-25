package decisionruntime

import (
	"context"
	"encoding/json"

	"github.com/memohai/memoh/internal/agent/application"
	userinput "github.com/memohai/memoh/internal/agent/decision/input"
	"github.com/memohai/memoh/internal/agent/turn"
)

// TurnService keeps decision routing behind the Channel-owned turn boundary.
// Ordinary turns and plain-text decision lookup stay delegated to the base
// service; approval and ask_user responses use the runtime owner router.
type TurnService struct {
	base   turn.Service
	router *Router
}

func NewTurnService(base turn.Service, router *Router) turn.Service {
	return &TurnService{base: base, router: router}
}

func (s *TurnService) StartTurn(ctx context.Context, cmd turn.StartTurnCommand) (turn.RunHandle, error) {
	return s.base.StartTurn(ctx, cmd)
}

func (s *TurnService) RespondToolApproval(ctx context.Context, input turn.ToolApprovalResponse, output chan<- json.RawMessage) error {
	return s.router.RespondToolApproval(ctx, application.ToolApprovalResponseInput{
		BotID:                      input.BotID,
		ThreadID:                   input.ThreadID,
		ActorChannelIdentityID:     input.ActorChannelIdentityID,
		ActorUserID:                input.ActorUserID,
		ApprovalID:                 input.ApprovalID,
		ExplicitID:                 input.ExplicitID,
		ReplyExternalMessageID:     input.ReplyExternalMessageID,
		Decision:                   input.Decision,
		Reason:                     input.Reason,
		ChatToken:                  input.ChatToken,
		SuppressActivePromptAttach: input.SuppressActivePromptAttach,
		ResolveOnly:                input.ResolveOnly,
	}, output)
}

func (s *TurnService) RespondUserInput(ctx context.Context, input turn.UserInputResponse, output chan<- json.RawMessage) error {
	return s.router.RespondUserInput(ctx, application.UserInputResponseInput{
		BotID:                      input.BotID,
		ThreadID:                   input.ThreadID,
		ActorChannelIdentityID:     input.ActorChannelIdentityID,
		ActorUserID:                input.ActorUserID,
		UserInputID:                input.UserInputID,
		ExplicitID:                 input.ExplicitID,
		ReplyExternalMessageID:     input.ReplyExternalMessageID,
		Answers:                    questionAnswersToInput(input.Answers),
		TextAnswer:                 input.TextAnswer,
		Canceled:                   input.Canceled,
		Reason:                     input.Reason,
		ChatToken:                  input.ChatToken,
		SuppressActivePromptAttach: input.SuppressActivePromptAttach,
		ResolveOnly:                input.ResolveOnly,
	}, output)
}

func questionAnswersToInput(in []turn.QuestionAnswer) []userinput.QuestionAnswer {
	if in == nil {
		return nil
	}
	out := make([]userinput.QuestionAnswer, len(in))
	for i := range in {
		out[i] = userinput.QuestionAnswer{
			QuestionID: in[i].QuestionID,
			OptionIDs:  in[i].OptionIDs,
			CustomText: in[i].CustomText,
			Text:       in[i].Text,
			Skipped:    in[i].Skipped,
		}
	}
	return out
}

func (s *TurnService) AdvancePlainTextUserInput(ctx context.Context, input userinput.AdvanceTextInput) (userinput.AdvanceTextResult, error) {
	return s.base.AdvancePlainTextUserInput(ctx, input)
}
