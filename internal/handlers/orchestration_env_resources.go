package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/memohai/memoh/internal/orchestrationenv"
)

// envResourceAPI is the part of orchestrationenv.Manager used by this
// handler.
type envResourceAPI interface {
	RegisterResource(ctx context.Context, req orchestrationenv.RegisterResourceRequest) (*orchestrationenv.Resource, error)
	UpdateResource(ctx context.Context, req orchestrationenv.UpdateResourceRequest) (*orchestrationenv.Resource, error)
	DeleteResource(ctx context.Context, id string) error
	GetResource(ctx context.Context, id string) (*orchestrationenv.Resource, error)
	ListResources(ctx context.Context, tenantID string) ([]orchestrationenv.Resource, error)
}

// EnvResourceHandler exposes the admin surface for env resource
// templates.
type EnvResourceHandler struct {
	api    envResourceAPI
	logger *slog.Logger
}

// NewEnvResourceHandler builds a handler bound to the env manager.
func NewEnvResourceHandler(log *slog.Logger, manager *orchestrationenv.Manager) *EnvResourceHandler {
	var api envResourceAPI
	if manager != nil {
		api = manager
	}
	return &EnvResourceHandler{
		api:    api,
		logger: log.With(slog.String("handler", "orchestration_env_resources")),
	}
}

// Register attaches the env-resource admin routes.
func (h *EnvResourceHandler) Register(e *echo.Echo) {
	group := e.Group("/orchestration/env-resources")
	group.GET("", h.ListEnvResources)
	group.POST("", h.RegisterEnvResource)
	group.GET("/:id", h.GetEnvResource)
	group.PATCH("/:id", h.UpdateEnvResource)
	group.DELETE("/:id", h.DeleteEnvResource)
}

// EnvResourceView is the JSON projection returned by the admin API.
type EnvResourceView struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	OwnerSubject string         `json:"owner_subject,omitempty"`
	Kind         string         `json:"kind"`
	Name         string         `json:"name"`
	Config       map[string]any `json:"config"`
	Capacity     int            `json:"capacity"`
	Status       string         `json:"status"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// EnvResourceListPage keeps the list response extensible.
type EnvResourceListPage struct {
	Items []EnvResourceView `json:"items"`
}

// EnvResourceCreateRequest is the admin payload for POST.
type EnvResourceCreateRequest struct {
	Kind     string         `json:"kind" validate:"required"`
	Name     string         `json:"name" validate:"required"`
	Capacity int            `json:"capacity"`
	Config   map[string]any `json:"config"`
	Metadata map[string]any `json:"metadata"`
	Status   string         `json:"status"`
}

// EnvResourceUpdateRequest is the admin payload for PATCH.
type EnvResourceUpdateRequest struct {
	Name     string         `json:"name" validate:"required"`
	Capacity int            `json:"capacity"`
	Config   map[string]any `json:"config"`
	Metadata map[string]any `json:"metadata"`
	Status   string         `json:"status"`
}

// ListEnvResources godoc
// @Summary List orchestration env resources
// @Description Return every env resource template registered for the caller's tenant
// @Tags orchestration
// @Security BearerAuth
// @Success 200 {object} EnvResourceListPage
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /orchestration/env-resources [get].
func (h *EnvResourceHandler) ListEnvResources(c echo.Context) error {
	caller, err := controlIdentity(c)
	if err != nil {
		return err
	}
	if h.api == nil {
		return c.JSON(http.StatusOK, EnvResourceListPage{Items: []EnvResourceView{}})
	}
	resources, err := h.api.ListResources(c.Request().Context(), caller.TenantID)
	if err != nil {
		return h.envHTTPError(err)
	}
	page := EnvResourceListPage{Items: make([]EnvResourceView, 0, len(resources))}
	for i := range resources {
		page.Items = append(page.Items, projectEnvResourceView(resources[i]))
	}
	return c.JSON(http.StatusOK, page)
}

// RegisterEnvResource godoc
// @Summary Register an orchestration env resource
// @Description Add a new env resource template under the caller's tenant
// @Tags orchestration
// @Security BearerAuth
// @Param payload body EnvResourceCreateRequest true "Env resource payload"
// @Success 201 {object} EnvResourceView
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /orchestration/env-resources [post].
func (h *EnvResourceHandler) RegisterEnvResource(c echo.Context) error {
	caller, err := controlIdentity(c)
	if err != nil {
		return err
	}
	if h.api == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration env runtime is not configured")
	}
	var req EnvResourceCreateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	resource, err := h.api.RegisterResource(c.Request().Context(), orchestrationenv.RegisterResourceRequest{
		TenantID:     caller.TenantID,
		OwnerSubject: caller.Subject,
		Kind:         strings.TrimSpace(req.Kind),
		Name:         strings.TrimSpace(req.Name),
		Capacity:     req.Capacity,
		Config:       req.Config,
		Status:       strings.TrimSpace(req.Status),
		Metadata:     req.Metadata,
	})
	if err != nil {
		return h.envHTTPError(err)
	}
	return c.JSON(http.StatusCreated, projectEnvResourceView(*resource))
}

// GetEnvResource godoc
// @Summary Get an orchestration env resource
// @Description Return a single env resource, scoped to the caller's tenant
// @Tags orchestration
// @Security BearerAuth
// @Param id path string true "Env resource ID"
// @Success 200 {object} EnvResourceView
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /orchestration/env-resources/{id} [get].
func (h *EnvResourceHandler) GetEnvResource(c echo.Context) error {
	caller, err := controlIdentity(c)
	if err != nil {
		return err
	}
	if h.api == nil {
		return echo.NewHTTPError(http.StatusNotFound, "env resource not found")
	}
	resource, err := h.api.GetResource(c.Request().Context(), strings.TrimSpace(c.Param("id")))
	if err != nil {
		return h.envHTTPError(err)
	}
	if resource.TenantID != caller.TenantID {
		return echo.NewHTTPError(http.StatusNotFound, "env resource not found")
	}
	return c.JSON(http.StatusOK, projectEnvResourceView(*resource))
}

// UpdateEnvResource godoc
// @Summary Update an orchestration env resource
// @Description Rewrite the mutable fields (config, capacity, status, metadata) of an env resource
// @Tags orchestration
// @Security BearerAuth
// @Param id path string true "Env resource ID"
// @Param payload body EnvResourceUpdateRequest true "Env resource update payload"
// @Success 200 {object} EnvResourceView
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /orchestration/env-resources/{id} [patch].
func (h *EnvResourceHandler) UpdateEnvResource(c echo.Context) error {
	caller, err := controlIdentity(c)
	if err != nil {
		return err
	}
	if h.api == nil {
		return echo.NewHTTPError(http.StatusNotFound, "env resource not found")
	}
	id := strings.TrimSpace(c.Param("id"))
	current, err := h.api.GetResource(c.Request().Context(), id)
	if err != nil {
		return h.envHTTPError(err)
	}
	if current.TenantID != caller.TenantID {
		return echo.NewHTTPError(http.StatusNotFound, "env resource not found")
	}
	var req EnvResourceUpdateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	updated, err := h.api.UpdateResource(c.Request().Context(), orchestrationenv.UpdateResourceRequest{
		ID:       id,
		Name:     strings.TrimSpace(req.Name),
		Config:   req.Config,
		Capacity: req.Capacity,
		Status:   strings.TrimSpace(req.Status),
		Metadata: req.Metadata,
	})
	if err != nil {
		return h.envHTTPError(err)
	}
	return c.JSON(http.StatusOK, projectEnvResourceView(*updated))
}

// DeleteEnvResource godoc
// @Summary Delete an orchestration env resource
// @Description Delete an unused env resource template. Resources with session history must be archived instead.
// @Tags orchestration
// @Security BearerAuth
// @Param id path string true "Env resource ID"
// @Success 204
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /orchestration/env-resources/{id} [delete].
func (h *EnvResourceHandler) DeleteEnvResource(c echo.Context) error {
	caller, err := controlIdentity(c)
	if err != nil {
		return err
	}
	if h.api == nil {
		return echo.NewHTTPError(http.StatusNotFound, "env resource not found")
	}
	id := strings.TrimSpace(c.Param("id"))
	current, err := h.api.GetResource(c.Request().Context(), id)
	if err != nil {
		return h.envHTTPError(err)
	}
	if current.TenantID != caller.TenantID {
		return echo.NewHTTPError(http.StatusNotFound, "env resource not found")
	}
	if err := h.api.DeleteResource(c.Request().Context(), id); err != nil {
		return h.envHTTPError(err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *EnvResourceHandler) envHTTPError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, orchestrationenv.ErrInvalidArgument):
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	case errors.Is(err, orchestrationenv.ErrResourceNotFound):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, orchestrationenv.ErrResourceInUse):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	default:
		h.logger.Error("orchestration env resource handler error", slog.String("error", err.Error()))
		return echo.NewHTTPError(http.StatusInternalServerError, "internal orchestration env error")
	}
}

func projectEnvResourceView(res orchestrationenv.Resource) EnvResourceView {
	return EnvResourceView{
		ID:           res.ID,
		TenantID:     res.TenantID,
		OwnerSubject: res.OwnerSubject,
		Kind:         res.Kind,
		Name:         res.Name,
		Config:       res.Config,
		Capacity:     res.Capacity,
		Status:       res.Status,
		Metadata:     res.Metadata,
		CreatedAt:    res.CreatedAt,
		UpdatedAt:    res.UpdatedAt,
	}
}
