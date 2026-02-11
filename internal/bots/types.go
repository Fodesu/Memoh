package bots

import (
	"context"
	"time"
)

type Bot struct {
	ID          string         `json:"id" validate:"required"`
	OwnerUserID string         `json:"owner_user_id" validate:"required"`
	Type        string         `json:"type" validate:"required"`
	DisplayName string         `json:"display_name" validate:"required"`
	AvatarURL   string         `json:"avatar_url,omitempty"`
	IsActive    bool           `json:"is_active" validate:"required"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at" validate:"required"`
	UpdatedAt   time.Time      `json:"updated_at" validate:"required"`
}

type BotMember struct {
	BotID     string    `json:"bot_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateBotRequest struct {
	Type        string         `json:"type"`
	DisplayName string         `json:"display_name,omitempty"`
	AvatarURL   string         `json:"avatar_url,omitempty"`
	IsActive    *bool          `json:"is_active,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type UpdateBotRequest struct {
	DisplayName *string        `json:"display_name,omitempty"`
	AvatarURL   *string        `json:"avatar_url,omitempty"`
	IsActive    *bool          `json:"is_active,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type TransferBotRequest struct {
	OwnerUserID string `json:"owner_user_id"`
}

type UpsertMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role,omitempty"`
}

type ListBotsResponse struct {
	Items []Bot `json:"items" validate:"required"`
}

type ListMembersResponse struct {
	Items []BotMember `json:"items"`
}

// ContainerLifecycle handles container lifecycle events bound to bot operations.
type ContainerLifecycle interface {
	SetupBotContainer(ctx context.Context, botID string) error
	CleanupBotContainer(ctx context.Context, botID string) error
}

const (
	BotTypePersonal = "personal"
	BotTypePublic   = "public"
)

const (
	MemberRoleOwner  = "owner"
	MemberRoleAdmin  = "admin"
	MemberRoleMember = "member"
)
