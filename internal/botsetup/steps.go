package botsetup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/settings"
)

// SettingsApplier is the slice of the settings service the settings step uses.
type SettingsApplier interface {
	GetBot(ctx context.Context, botID string) (settings.Settings, error)
	UpsertBot(ctx context.Context, botID string, req settings.UpsertRequest) (settings.Settings, error)
}

// GrantsApplier is the slice of the bots service the grants step uses.
type GrantsApplier interface {
	ListUserGrants(ctx context.Context, botID string) ([]bots.UserGrant, error)
	CreateUserGrant(ctx context.Context, botID, createdByUserID string, req bots.CreateUserGrantRequest) (bots.UserGrant, error)
}

// Executor applies one step. It first checks whether the step is already
// satisfied, so a retry or a replay never applies anything twice. The note is
// recorded as the step's last_error even on success (partial results).
type Executor func(ctx context.Context, s Setup) (note string, err error)

func (s *Service) executors() map[string]Executor {
	return map[string]Executor{
		StepSettings: s.applySettings,
		StepGrants:   s.applyGrants,
	}
}

// applySettings writes the requested settings unless the bot already has them.
func (s *Service) applySettings(ctx context.Context, st Setup) (string, error) {
	want := st.Spec.Settings
	if want == nil {
		return "", nil
	}
	if s.deps.Settings == nil {
		return "", &StepError{Retryable: false, Err: errors.New("settings service not configured")}
	}
	current, err := s.deps.Settings.GetBot(ctx, st.BotID)
	if err != nil {
		return "", fmt.Errorf("read settings: %w", err)
	}
	if settingsSatisfied(current, *want) {
		return "", nil
	}
	if _, err := s.deps.Settings.UpsertBot(ctx, st.BotID, *want); err != nil {
		return "", fmt.Errorf("apply settings: %w", err)
	}
	return "", nil
}

// settingsSatisfied reports whether every field the request sets already has
// that value. Fields the comparison does not model count as unsatisfied, so
// they are (idempotently) written.
func settingsSatisfied(cur settings.Settings, want settings.UpsertRequest) bool {
	str := func(p *string, have string) bool { return p == nil || strings.TrimSpace(*p) == strings.TrimSpace(have) }
	boolean := func(p *bool, have bool) bool { return p == nil || *p == have }
	if !str(want.ChatModelID, cur.ChatModelID) || !str(want.DefaultBotAgentID, cur.DefaultBotAgentID) ||
		!str(want.ChatRuntime, cur.ChatRuntime) || !str(want.ChatACPAgentID, cur.ChatACPAgentID) ||
		!str(want.ChatACPProjectPath, cur.ChatACPProjectPath) || !str(want.ChatACPProjectMode, cur.ChatACPProjectMode) ||
		!str(want.ImageModelID, cur.ImageModelID) || !str(want.SearchProviderID, cur.SearchProviderID) ||
		!str(want.FetchProviderID, cur.FetchProviderID) || !str(want.MemoryProviderID, cur.MemoryProviderID) ||
		!str(want.TtsModelID, cur.TtsModelID) || !str(want.TranscriptionModelID, cur.TranscriptionModelID) ||
		!str(want.VideoModelID, cur.VideoModelID) || !str(want.Timezone, cur.Timezone) ||
		!str(want.ReasoningEffort, cur.ReasoningEffort) || !str(want.CompactionModelID, cur.CompactionModelID) {
		return false
	}
	if !boolean(want.CompactionEnabled, cur.CompactionEnabled) || !boolean(want.PersistFullToolResults, cur.PersistFullToolResults) ||
		!boolean(want.ShowToolCallsInIM, cur.ShowToolCallsInIM) {
		return false
	}
	if want.CompactionThreshold != nil && *want.CompactionThreshold != cur.CompactionThreshold {
		return false
	}
	if want.CompactionTargetPercent != nil && (cur.CompactionTargetPercent == nil || *want.CompactionTargetPercent != *cur.CompactionTargetPercent) {
		return false
	}
	if (want.CommandUILanguage != "" && want.CommandUILanguage != cur.CommandUILanguage) ||
		(want.AclDefaultEffect != "" && want.AclDefaultEffect != cur.AclDefaultEffect) ||
		(want.DiscussProbeModelID != "" && want.DiscussProbeModelID != cur.DiscussProbeModelID) {
		return false
	}
	// Structured fields are not compared; writing them again is idempotent.
	return want.ToolApprovalConfig == nil
}

// applyGrants creates the requested grants that do not exist yet. A single
// grant that cannot be created is recorded in the step note and does not stop
// the others: the bot is usable without it and the user can add it by hand.
func (s *Service) applyGrants(ctx context.Context, st Setup) (string, error) {
	if len(st.Spec.Grants) == 0 {
		return "", nil
	}
	if s.deps.Grants == nil {
		return "", &StepError{Retryable: false, Err: errors.New("grants service not configured")}
	}
	existing, err := s.deps.Grants.ListUserGrants(ctx, st.BotID)
	if err != nil {
		return "", fmt.Errorf("list grants: %w", err)
	}
	has := func(g bots.CreateUserGrantRequest) bool {
		subject := strings.ToLower(strings.TrimSpace(g.SubjectType))
		for _, e := range existing {
			if e.SubjectType == subject && (subject != bots.GrantSubjectUser || e.UserID == strings.TrimSpace(g.UserID)) {
				return true
			}
		}
		return false
	}
	var notes []string
	for _, g := range st.Spec.Grants {
		if has(g) {
			continue
		}
		if _, err := s.deps.Grants.CreateUserGrant(ctx, st.BotID, st.RequestedBy, g); err != nil {
			if errors.Is(err, bots.ErrGrantExists) {
				continue
			}
			target := strings.TrimSpace(g.SubjectType)
			if g.UserID != "" {
				target += ":" + strings.TrimSpace(g.UserID)
			}
			notes = append(notes, fmt.Sprintf("grant %s: %s", target, sanitize(err)))
		}
	}
	return strings.Join(notes, "; "), nil
}
