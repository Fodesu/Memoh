package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
)

func (s *Service) CreateSideEffectApprovalToken(ctx context.Context, req CreateSideEffectApprovalTokenRequest) (SideEffectApprovalToken, error) {
	if s == nil || s.queries == nil {
		return SideEffectApprovalToken{}, errors.New("orchestration service is not configured")
	}
	attemptID, err := db.ParseUUID(req.AttemptID)
	if err != nil {
		return SideEffectApprovalToken{}, fmt.Errorf("invalid attempt_id: %w", err)
	}
	attempt, err := s.queries.GetOrchestrationTaskAttemptByID(ctx, attemptID)
	if err != nil {
		return SideEffectApprovalToken{}, err
	}
	switch strings.TrimSpace(attempt.Status) {
	case TaskAttemptStatusClaimed, TaskAttemptStatusBinding, TaskAttemptStatusRunning:
	default:
		return SideEffectApprovalToken{}, fmt.Errorf("attempt status %q cannot receive side-effect approval tokens", strings.TrimSpace(attempt.Status))
	}
	manifest, err := s.queries.GetOrchestrationInputManifestByID(ctx, attempt.InputManifestID)
	if err != nil {
		return SideEffectApprovalToken{}, fmt.Errorf("load input manifest: %w", err)
	}
	envSessionID := pgtype.UUID{}
	envLeaseEpoch := int64(0)
	if capture, ok := decodeCapturedEnvPreconditions(manifest.CapturedEnvPreconditions); ok {
		if parsed, parseErr := db.ParseUUID(capture.SessionID); parseErr == nil {
			envSessionID = parsed
		}
		envLeaseEpoch = capture.LeaseEpoch
	}
	token := uuid.NewString()
	_, tokenID, err := newPGUUID()
	if err != nil {
		return SideEffectApprovalToken{}, err
	}
	row, err := s.queries.CreateOrchestrationSideEffectApprovalToken(ctx, sqlc.CreateOrchestrationSideEffectApprovalTokenParams{
		ID:             tokenID,
		RunID:          attempt.RunID,
		TaskID:         attempt.TaskID,
		AttemptID:      attempt.ID,
		ClaimEpoch:     attempt.ClaimEpoch,
		EnvSessionID:   envSessionID,
		EnvLeaseEpoch:  envLeaseEpoch,
		TokenHash:      sideEffectApprovalHash(token),
		ApprovedBy:     strings.TrimSpace(req.ApprovedBy),
		ApprovalReason: strings.TrimSpace(req.ApprovalReason),
		ExpiresAt:      optionalTimestamptz(req.ExpiresAt),
	})
	if err != nil {
		return SideEffectApprovalToken{}, err
	}
	return sideEffectApprovalTokenFromRow(row, token), nil
}

func sideEffectApprovalTokenFromRow(row sqlc.OrchestrationSideEffectApprovalToken, token string) SideEffectApprovalToken {
	claimEpoch, _ := uint64FromInt64(row.ClaimEpoch, "side_effect_approval.claim_epoch")
	result := SideEffectApprovalToken{
		ID:             uuidToString(row.ID),
		RunID:          uuidToString(row.RunID),
		TaskID:         uuidToString(row.TaskID),
		AttemptID:      uuidToString(row.AttemptID),
		ClaimEpoch:     claimEpoch,
		EnvSessionID:   uuidToString(row.EnvSessionID),
		EnvLeaseEpoch:  row.EnvLeaseEpoch,
		EffectClass:    strings.TrimSpace(row.EffectClass),
		Token:          token,
		Status:         strings.TrimSpace(row.Status),
		ApprovedBy:     strings.TrimSpace(row.ApprovedBy),
		ApprovalReason: strings.TrimSpace(row.ApprovalReason),
		ToolCallID:     strings.TrimSpace(row.ToolCallID),
		CreatedAt:      row.CreatedAt.Time,
	}
	if row.ExpiresAt.Valid {
		expiresAt := row.ExpiresAt.Time
		result.ExpiresAt = &expiresAt
	}
	if row.ConsumedAt.Valid {
		consumedAt := row.ConsumedAt.Time
		result.ConsumedAt = &consumedAt
	}
	return result
}

func sideEffectApprovalHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func optionalTimestamptz(value *time.Time) pgtype.Timestamptz {
	if value == nil || value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
