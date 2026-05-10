package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	dbsqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
	"github.com/memohai/memoh/internal/imagebuild"
)

const (
	containerImageSourceRegistry   = "registry"
	containerImageSourceDockerfile = "dockerfile"
	containerImageStatusReady      = "ready"
	containerImageStatusBuilding   = "building"
	containerImageStatusFailed     = "failed"
)

type containerImageQueries interface {
	CreateOrchestrationContainerImage(ctx context.Context, arg dbsqlc.CreateOrchestrationContainerImageParams) (dbsqlc.OrchestrationContainerImage, error)
	GetOrchestrationContainerImageByID(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationContainerImage, error)
	ListOrchestrationContainerImagesByTenant(ctx context.Context, tenantID string) ([]dbsqlc.OrchestrationContainerImage, error)
	UpdateOrchestrationContainerImageBuildResult(ctx context.Context, arg dbsqlc.UpdateOrchestrationContainerImageBuildResultParams) (dbsqlc.OrchestrationContainerImage, error)
}

// ContainerImageHandler exposes the catalog of images env resources can use.
type ContainerImageHandler struct {
	queries containerImageQueries
	builder *imagebuild.Service
	logger  *slog.Logger
}

// NewContainerImageHandler builds the orchestration image catalog handler.
func NewContainerImageHandler(log *slog.Logger, queries *dbsqlc.Queries, builder *imagebuild.Service) *ContainerImageHandler {
	return &ContainerImageHandler{
		queries: queries,
		builder: builder,
		logger:  log.With(slog.String("handler", "orchestration_container_images")),
	}
}

// Register attaches the container image catalog routes.
func (h *ContainerImageHandler) Register(e *echo.Echo) {
	group := e.Group("/orchestration/container-images")
	group.GET("", h.ListContainerImages)
	group.GET("/capabilities", h.GetContainerImageCapabilities)
	group.POST("", h.CreateContainerImage)
	group.GET("/:id", h.GetContainerImage)
}

// ContainerImageView is the JSON projection returned by the admin API.
type ContainerImageView struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	OwnerSubject   string         `json:"owner_subject,omitempty"`
	Name           string         `json:"name"`
	SourceType     string         `json:"source_type"`
	ImageRef       string         `json:"image_ref"`
	Dockerfile     string         `json:"dockerfile,omitempty"`
	BuildOptions   map[string]any `json:"build_options"`
	Status         string         `json:"status"`
	Digest         string         `json:"digest,omitempty"`
	LastBuildError string         `json:"last_build_error,omitempty"`
	Metadata       map[string]any `json:"metadata"`
	Builtin        bool           `json:"builtin"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// ContainerImageListPage keeps the list response extensible.
type ContainerImageListPage struct {
	Items []ContainerImageView `json:"items"`
}

// ContainerImageCapabilities describes image build support for this runtime.
type ContainerImageCapabilities struct {
	DockerfileBuild bool   `json:"dockerfile_build"`
	Reason          string `json:"reason,omitempty"`
}

// ContainerImageCreateRequest is the admin payload for POST.
type ContainerImageCreateRequest struct {
	Name         string         `json:"name" validate:"required"`
	SourceType   string         `json:"source_type" validate:"required"`
	ImageRef     string         `json:"image_ref" validate:"required"`
	Dockerfile   string         `json:"dockerfile"`
	BuildOptions map[string]any `json:"build_options"`
	Metadata     map[string]any `json:"metadata"`
}

// ListContainerImages godoc
// @Summary List orchestration container images
// @Description Return the built-in images and tenant images available for env resources
// @Tags orchestration
// @Security BearerAuth
// @Success 200 {object} ContainerImageListPage
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /orchestration/container-images [get].
func (h *ContainerImageHandler) ListContainerImages(c echo.Context) error {
	caller, err := controlIdentity(c)
	if err != nil {
		return err
	}
	page := ContainerImageListPage{Items: defaultContainerImageViews(caller.TenantID)}
	if h.queries == nil {
		return c.JSON(http.StatusOK, page)
	}
	images, err := h.queries.ListOrchestrationContainerImagesByTenant(c.Request().Context(), caller.TenantID)
	if err != nil {
		return h.imageHTTPError(err)
	}
	for i := range images {
		page.Items = append(page.Items, projectContainerImageView(images[i], false))
	}
	return c.JSON(http.StatusOK, page)
}

// GetContainerImageCapabilities godoc
// @Summary Get orchestration image capabilities
// @Description Return image build capabilities for the current runtime backend
// @Tags orchestration
// @Security BearerAuth
// @Success 200 {object} ContainerImageCapabilities
// @Failure 401 {object} ErrorResponse
// @Router /orchestration/container-images/capabilities [get].
func (h *ContainerImageHandler) GetContainerImageCapabilities(c echo.Context) error {
	if _, err := controlIdentity(c); err != nil {
		return err
	}
	caps := imagebuild.Capabilities{
		DockerfileBuild: false,
		Reason:          "image build service is not configured",
	}
	if h.builder != nil {
		caps = h.builder.Capabilities()
	}
	return c.JSON(http.StatusOK, ContainerImageCapabilities{
		DockerfileBuild: caps.DockerfileBuild,
		Reason:          caps.Reason,
	})
}

// CreateContainerImage godoc
// @Summary Add an orchestration container image
// @Description Register an existing image ref or Dockerfile build source for env resources
// @Tags orchestration
// @Security BearerAuth
// @Param payload body ContainerImageCreateRequest true "Container image payload"
// @Success 201 {object} ContainerImageView
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /orchestration/container-images [post].
func (h *ContainerImageHandler) CreateContainerImage(c echo.Context) error {
	caller, err := controlIdentity(c)
	if err != nil {
		return err
	}
	if h.queries == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "orchestration image catalog is not configured")
	}
	var req ContainerImageCreateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	name := strings.TrimSpace(req.Name)
	sourceType := strings.TrimSpace(req.SourceType)
	imageRef := strings.TrimSpace(req.ImageRef)
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if imageRef == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "image_ref is required")
	}
	status := containerImageStatusReady
	dockerfile := strings.TrimSpace(req.Dockerfile)
	switch sourceType {
	case "", containerImageSourceRegistry:
		sourceType = containerImageSourceRegistry
	case containerImageSourceDockerfile:
		if dockerfile == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "dockerfile is required")
		}
		if h.builder == nil || !h.builder.Capabilities().DockerfileBuild {
			reason := "image build is not supported by the current container backend"
			if h.builder != nil && strings.TrimSpace(h.builder.Capabilities().Reason) != "" {
				reason = h.builder.Capabilities().Reason
			}
			return echo.NewHTTPError(http.StatusBadRequest, reason)
		}
		status = containerImageStatusBuilding
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "source_type is invalid")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return h.imageHTTPError(err)
	}
	row, err := h.queries.CreateOrchestrationContainerImage(c.Request().Context(), dbsqlc.CreateOrchestrationContainerImageParams{
		ID:             pgtype.UUID{Bytes: id, Valid: true},
		TenantID:       caller.TenantID,
		OwnerSubject:   caller.Subject,
		Name:           name,
		SourceType:     sourceType,
		ImageRef:       imageRef,
		Dockerfile:     dockerfile,
		BuildOptions:   encodeContainerImageObject(req.BuildOptions),
		Status:         status,
		Digest:         "",
		LastBuildError: "",
		Metadata:       encodeContainerImageObject(req.Metadata),
	})
	if err != nil {
		return h.imageHTTPError(err)
	}
	if sourceType == containerImageSourceDockerfile {
		info, buildErr := h.builder.Build(c.Request().Context(), imagebuild.BuildRequest{
			Tag:        imageRef,
			Dockerfile: dockerfile,
		})
		if buildErr != nil {
			row, err = h.queries.UpdateOrchestrationContainerImageBuildResult(c.Request().Context(), dbsqlc.UpdateOrchestrationContainerImageBuildResultParams{
				ID:             row.ID,
				Status:         containerImageStatusFailed,
				Digest:         "",
				LastBuildError: buildErr.Error(),
			})
			if err != nil {
				return h.imageHTTPError(err)
			}
			return c.JSON(http.StatusCreated, projectContainerImageView(row, false))
		}
		row, err = h.queries.UpdateOrchestrationContainerImageBuildResult(c.Request().Context(), dbsqlc.UpdateOrchestrationContainerImageBuildResultParams{
			ID:             row.ID,
			Status:         containerImageStatusReady,
			Digest:         info.ImageID,
			LastBuildError: "",
		})
		if err != nil {
			return h.imageHTTPError(err)
		}
	}
	return c.JSON(http.StatusCreated, projectContainerImageView(row, false))
}

// GetContainerImage godoc
// @Summary Get an orchestration container image
// @Description Return a single tenant image. Built-in images are returned by the list endpoint.
// @Tags orchestration
// @Security BearerAuth
// @Param id path string true "Image ID"
// @Success 200 {object} ContainerImageView
// @Failure 401 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /orchestration/container-images/{id} [get].
func (h *ContainerImageHandler) GetContainerImage(c echo.Context) error {
	caller, err := controlIdentity(c)
	if err != nil {
		return err
	}
	if h.queries == nil {
		return echo.NewHTTPError(http.StatusNotFound, "container image not found")
	}
	id, err := parseContainerImageUUID(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "container image not found")
	}
	image, err := h.queries.GetOrchestrationContainerImageByID(c.Request().Context(), id)
	if err != nil {
		return h.imageHTTPError(err)
	}
	if image.TenantID != caller.TenantID {
		return echo.NewHTTPError(http.StatusNotFound, "container image not found")
	}
	return c.JSON(http.StatusOK, projectContainerImageView(image, false))
}

func (h *ContainerImageHandler) imageHTTPError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return echo.NewHTTPError(http.StatusNotFound, "container image not found")
	default:
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return echo.NewHTTPError(http.StatusConflict, "container image already exists")
		}
		h.logger.Error("orchestration container image handler error", slog.String("error", err.Error()))
		return echo.NewHTTPError(http.StatusInternalServerError, "internal orchestration image error")
	}
}

func projectContainerImageView(image dbsqlc.OrchestrationContainerImage, builtin bool) ContainerImageView {
	return ContainerImageView{
		ID:             uuidStringFromPg(image.ID),
		TenantID:       image.TenantID,
		OwnerSubject:   image.OwnerSubject,
		Name:           image.Name,
		SourceType:     image.SourceType,
		ImageRef:       image.ImageRef,
		Dockerfile:     image.Dockerfile,
		BuildOptions:   decodeContainerImageObject(image.BuildOptions),
		Status:         image.Status,
		Digest:         image.Digest,
		LastBuildError: image.LastBuildError,
		Metadata:       decodeContainerImageObject(image.Metadata),
		Builtin:        builtin,
		CreatedAt:      timeFromPg(image.CreatedAt),
		UpdatedAt:      timeFromPg(image.UpdatedAt),
	}
}

func defaultContainerImageViews(tenantID string) []ContainerImageView {
	now := time.Unix(0, 0).UTC()
	return []ContainerImageView{
		{
			ID:           "builtin:debian-bookworm-slim",
			TenantID:     tenantID,
			Name:         "Debian Bookworm",
			SourceType:   containerImageSourceRegistry,
			ImageRef:     "debian:bookworm-slim",
			BuildOptions: map[string]any{},
			Status:       containerImageStatusReady,
			Metadata:     map[string]any{"registry_source": "dockerhub"},
			Builtin:      true,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
}

func parseContainerImageUUID(value string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

func uuidStringFromPg(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return value.String()
}

func timeFromPg(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func encodeContainerImageObject(value map[string]any) []byte {
	if value == nil {
		return []byte("{}")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func decodeContainerImageObject(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}
