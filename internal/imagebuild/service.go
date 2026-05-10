package imagebuild

import (
	"context"
	"errors"
	"log/slog"

	"github.com/memohai/memoh/internal/boot"
	"github.com/memohai/memoh/internal/config"
	ctr "github.com/memohai/memoh/internal/container"
)

type Capabilities struct {
	DockerfileBuild bool
	Reason          string
}

type BuildRequest struct {
	Tag        string
	Dockerfile string
}

type BuildResult struct {
	ImageID string
}

type Builder interface {
	Capabilities() Capabilities
	Build(ctx context.Context, req BuildRequest) (BuildResult, error)
}

type Service struct {
	builder Builder
}

func NewService(log *slog.Logger, cfg config.Config, rc *boot.RuntimeConfig) (*Service, error) {
	if rc != nil && rc.ContainerBackend == ctr.BackendDocker {
		builder, err := NewDockerBuilder(log, cfg)
		if err != nil {
			return nil, err
		}
		return &Service{builder: builder}, nil
	}
	return &Service{builder: UnsupportedBuilder{
		reason: "image build is not supported by the current container backend",
	}}, nil
}

func (s *Service) Capabilities() Capabilities {
	if s == nil || s.builder == nil {
		return Capabilities{
			DockerfileBuild: false,
			Reason:          "image build service is not configured",
		}
	}
	return s.builder.Capabilities()
}

func (s *Service) Build(ctx context.Context, req BuildRequest) (BuildResult, error) {
	if s == nil || s.builder == nil {
		return BuildResult{}, ErrNotSupported
	}
	return s.builder.Build(ctx, req)
}

var ErrNotSupported = errors.New("image build is not supported by the current container backend")

type UnsupportedBuilder struct {
	reason string
}

func (b UnsupportedBuilder) Capabilities() Capabilities {
	reason := b.reason
	if reason == "" {
		reason = ErrNotSupported.Error()
	}
	return Capabilities{
		DockerfileBuild: false,
		Reason:          reason,
	}
}

func (UnsupportedBuilder) Build(context.Context, BuildRequest) (BuildResult, error) {
	return BuildResult{}, ErrNotSupported
}
