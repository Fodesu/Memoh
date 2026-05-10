package imagebuild

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/client"

	"github.com/memohai/memoh/internal/config"
	ctr "github.com/memohai/memoh/internal/container"
)

type DockerBuilder struct {
	client *client.Client
	logger *slog.Logger
}

func NewDockerBuilder(log *slog.Logger, cfg config.Config) (*DockerBuilder, error) {
	if log == nil {
		log = slog.Default()
	}
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if host := strings.TrimSpace(cfg.Docker.Host); host != "" {
		opts = append(opts, client.WithHost(host))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("create docker image builder: %w", err)
	}
	return &DockerBuilder{
		client: cli,
		logger: log.With(slog.String("service", "imagebuild"), slog.String("builder", "docker")),
	}, nil
}

func (*DockerBuilder) Capabilities() Capabilities {
	return Capabilities{DockerfileBuild: true}
}

func (b *DockerBuilder) Build(ctx context.Context, req BuildRequest) (BuildResult, error) {
	tag := strings.TrimSpace(req.Tag)
	dockerfile := strings.TrimSpace(req.Dockerfile)
	if tag == "" || dockerfile == "" {
		return BuildResult{}, ctr.ErrInvalidArgument
	}
	buildContext, err := dockerfileBuildContext(dockerfile)
	if err != nil {
		return BuildResult{}, err
	}
	resp, err := b.client.ImageBuild(ctx, buildContext, build.ImageBuildOptions{
		Tags:        []string{tag},
		Dockerfile:  "Dockerfile",
		Remove:      true,
		ForceRemove: true,
		PullParent:  true,
	})
	if err != nil {
		return BuildResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := consumeBuildOutput(resp.Body); err != nil {
		return BuildResult{}, err
	}
	info, err := b.client.ImageInspect(ctx, tag)
	if err != nil {
		return BuildResult{}, err
	}
	return BuildResult{ImageID: info.ID}, nil
}

func dockerfileBuildContext(dockerfile string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte(dockerfile)
	if err := tw.WriteHeader(&tar.Header{
		Name: "Dockerfile",
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}

func consumeBuildOutput(r io.Reader) error {
	decoder := json.NewDecoder(r)
	for {
		var msg struct {
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		if err := decoder.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if strings.TrimSpace(msg.ErrorDetail.Message) != "" {
			return fmt.Errorf("%w: %s", ctr.ErrRuntime, msg.ErrorDetail.Message)
		}
		if strings.TrimSpace(msg.Error) != "" {
			return fmt.Errorf("%w: %s", ctr.ErrRuntime, msg.Error)
		}
	}
}
