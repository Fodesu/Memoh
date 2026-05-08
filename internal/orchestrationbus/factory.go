package orchestrationbus

import (
	"context"
	"log/slog"
	"strings"

	"github.com/memohai/memoh/internal/config"
)

// New constructs a Bus appropriate for the supplied configuration. When the
// NATS URL is empty the in-memory bus is returned, which is fine for
// single-process deployments and integration tests. When a URL is configured
// we attempt to dial JetStream; on failure the caller decides whether to fall
// back or surface the error (we surface it here so misconfiguration is loud).
func New(ctx context.Context, logger *slog.Logger, cfg config.NATSConfig) (Bus, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if !cfg.Enabled() {
		logger.Debug("orchestration bus running in-memory; nats.url not configured")
		return NewInMemoryBus(0), nil
	}

	jsCfg := JetStreamConfig{
		URL:             strings.TrimSpace(cfg.URL),
		Token:           cfg.Token,
		User:            cfg.User,
		Password:        cfg.Password,
		CredentialsFile: cfg.CredentialsFile,
		Replicas:        cfg.EffectiveStreamReplicas(),
	}
	return NewJetStreamBus(ctx, logger, jsCfg)
}
