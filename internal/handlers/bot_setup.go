package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/bots"
	"github.com/felinics/memoh/internal/botsetup"
)

// BotSetupStepView is one post-create setup step as reported to clients.
type BotSetupStepView struct {
	Step      string    `json:"step"`
	Status    string    `json:"status"`
	Attempts  int32     `json:"attempts"`
	LastError string    `json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BotSetupSpecSummary says which parts of the setup the server manages,
// without echoing the values (a grant list or a settings payload may be
// large; credentials are never part of the spec).
type BotSetupSpecSummary struct {
	Settings bool   `json:"settings"`
	Grants   int    `json:"grants"`
	Agent    string `json:"agent,omitempty"`
}

// BotSetupView is the progress resource for a bot's post-create setup.
type BotSetupView struct {
	BotID              string              `json:"bot_id"`
	State              string              `json:"state"`
	DesiredGeneration  int64               `json:"desired_generation"`
	ObservedGeneration int64               `json:"observed_generation"`
	RetryPending       bool                `json:"retry_pending"`
	Spec               BotSetupSpecSummary `json:"spec"`
	Steps              []BotSetupStepView  `json:"steps"`
}

// BotSetupHandler exposes the post-create setup progress of a bot.
type BotSetupHandler struct {
	botService     *bots.Service
	accountService *accounts.Service
	setup          *botsetup.Service
}

// NewBotSetupHandler constructs a BotSetupHandler.
func NewBotSetupHandler(botService *bots.Service, accountService *accounts.Service, setup *botsetup.Service) *BotSetupHandler {
	return &BotSetupHandler{botService: botService, accountService: accountService, setup: setup}
}

// Register mounts the routes.
func (h *BotSetupHandler) Register(e *echo.Echo) {
	e.GET("/bots/:bot_id/setup", h.Get)
	e.POST("/bots/:bot_id/setup/retry", h.Retry)
}

// Get godoc
// @Summary Get bot setup progress
// @Description Where the server-side post-create setup of a bot stands, step by step
// @Tags bots
// @Param bot_id path string true "Bot ID"
// @Success 200 {object} BotSetupView
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /bots/{bot_id}/setup [get].
func (h *BotSetupHandler) Get(c echo.Context) error {
	botID, err := h.requireAccess(c)
	if err != nil {
		return err
	}
	st, err := h.setup.Get(c.Request().Context(), botID)
	if err != nil {
		return h.mapError(err)
	}
	return c.JSON(http.StatusOK, botSetupView(st, h.setup.MaxAttempts()))
}

// Retry godoc
// @Summary Retry bot setup
// @Description Re-run the setup steps that have not completed; done steps are kept
// @Tags bots
// @Param bot_id path string true "Bot ID"
// @Success 200 {object} BotSetupView
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /bots/{bot_id}/setup/retry [post].
func (h *BotSetupHandler) Retry(c echo.Context) error {
	botID, err := h.requireAccess(c)
	if err != nil {
		return err
	}
	st, err := h.setup.Retry(c.Request().Context(), botID)
	if err != nil {
		return h.mapError(err)
	}
	return c.JSON(http.StatusOK, botSetupView(st, h.setup.MaxAttempts()))
}

func (h *BotSetupHandler) requireAccess(c echo.Context) (string, error) {
	actorID, err := RequireChannelIdentityID(c)
	if err != nil {
		return "", err
	}
	botID := strings.TrimSpace(c.Param("bot_id"))
	if botID == "" {
		return "", echo.NewHTTPError(http.StatusBadRequest, "bot_id is required")
	}
	if h.setup == nil {
		return "", echo.NewHTTPError(http.StatusInternalServerError, "bot setup not configured")
	}
	if _, err := AuthorizeBotAccess(c.Request().Context(), h.botService, h.accountService, actorID, botID); err != nil {
		return "", err
	}
	return botID, nil
}

func (*BotSetupHandler) mapError(err error) error {
	if errors.Is(err, botsetup.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "bot setup not found")
	}
	return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
}

func botSetupView(st botsetup.Setup, maxAttempts int32) BotSetupView {
	view := BotSetupView{
		BotID:              st.BotID,
		State:              st.State,
		DesiredGeneration:  st.DesiredGeneration,
		ObservedGeneration: st.ObservedGeneration,
		RetryPending:       st.RetryPending(maxAttempts),
		Spec: BotSetupSpecSummary{
			Settings: st.Spec.Settings != nil,
			Grants:   len(st.Spec.Grants),
		},
		Steps: make([]BotSetupStepView, 0, len(st.Steps)),
	}
	if st.Spec.Agent != nil {
		view.Spec.Agent = st.Spec.Agent.Runtime
	}
	for _, step := range st.Steps {
		view.Steps = append(view.Steps, BotSetupStepView{
			Step: step.Step, Status: step.Status, Attempts: step.Attempts, LastError: step.LastError, UpdatedAt: step.UpdatedAt,
		})
	}
	return view
}
