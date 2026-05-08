package orchestrationbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// JetStreamConfig captures the runtime knobs needed to provision the
// orchestration JetStream streams and the NATS client connection.
type JetStreamConfig struct {
	URL             string
	Token           string `json:"-"`
	User            string
	Password        string `json:"-"`
	CredentialsFile string
	Replicas        int

	// RunEventMaxAge controls how long committed run events are retained
	// in JetStream before being aged out. The kernel still has the durable
	// copy in Postgres, so the bus is best treated as a live-tail buffer.
	RunEventMaxAge time.Duration

	// AttemptFactMaxAge bounds the retention of attempt facts. Facts are
	// advisory and short lived; a tight TTL keeps memory predictable.
	AttemptFactMaxAge time.Duration

	// ConnectionName is set on the NATS client so server-side observability
	// reflects the originating component.
	ConnectionName string
}

const (
	defaultRunEventMaxAge    = 24 * time.Hour
	defaultAttemptFactMaxAge = 1 * time.Hour
)

// JetStreamBus is a NATS JetStream-backed implementation of Bus.
//
// Run events are deduplicated by their EventID via JetStream MsgID semantics so
// the outbox dispatcher can safely retry without producing duplicate deliveries.
// Attempt facts use FactID for the same purpose.
type JetStreamBus struct {
	logger *slog.Logger
	cfg    JetStreamConfig

	conn *nats.Conn
	js   jetstream.JetStream

	mu     sync.Mutex
	closed bool
	subs   map[*jsRunSub]struct{}
	fsubs  map[*jsFactSub]struct{}
}

// NewJetStreamBus connects to the configured NATS server, ensures the
// orchestration streams exist, and returns a ready-to-use bus. Callers must
// invoke Close on shutdown to release the connection and outstanding consumers.
func NewJetStreamBus(ctx context.Context, logger *slog.Logger, cfg JetStreamConfig) (*JetStreamBus, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("orchestrationbus: jetstream url is required")
	}
	if cfg.Replicas <= 0 {
		cfg.Replicas = 1
	}
	if cfg.RunEventMaxAge <= 0 {
		cfg.RunEventMaxAge = defaultRunEventMaxAge
	}
	if cfg.AttemptFactMaxAge <= 0 {
		cfg.AttemptFactMaxAge = defaultAttemptFactMaxAge
	}
	if strings.TrimSpace(cfg.ConnectionName) == "" {
		cfg.ConnectionName = "memoh-orchestration"
	}

	natsOpts := []nats.Option{
		nats.Name(cfg.ConnectionName),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
	}
	if strings.TrimSpace(cfg.Token) != "" {
		natsOpts = append(natsOpts, nats.Token(cfg.Token))
	}
	if strings.TrimSpace(cfg.User) != "" {
		natsOpts = append(natsOpts, nats.UserInfo(cfg.User, cfg.Password))
	}
	if strings.TrimSpace(cfg.CredentialsFile) != "" {
		natsOpts = append(natsOpts, nats.UserCredentials(cfg.CredentialsFile))
	}

	conn, err := nats.Connect(cfg.URL, natsOpts...)
	if err != nil {
		return nil, fmt.Errorf("orchestrationbus: connect nats: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("orchestrationbus: jetstream context: %w", err)
	}

	bus := &JetStreamBus{
		logger: logger.With(slog.String("component", "orchestrationbus.jetstream")),
		cfg:    cfg,
		conn:   conn,
		js:     js,
		subs:   map[*jsRunSub]struct{}{},
		fsubs:  map[*jsFactSub]struct{}{},
	}

	if err := bus.ensureStreams(ctx); err != nil {
		conn.Close()
		return nil, err
	}

	bus.logger.Info("orchestration jetstream bus connected", slog.String("url", cfg.URL))
	return bus, nil
}

func (b *JetStreamBus) ensureStreams(ctx context.Context) error {
	runEventsCfg := jetstream.StreamConfig{
		Name:        StreamRunEvents,
		Description: "Memoh orchestration committed run events outbox",
		Subjects:    []string{SubjectAllRunEvents},
		Retention:   jetstream.LimitsPolicy,
		Storage:     jetstream.FileStorage,
		Replicas:    b.cfg.Replicas,
		MaxAge:      b.cfg.RunEventMaxAge,
		Discard:     jetstream.DiscardOld,
		Duplicates:  5 * time.Minute,
	}
	if _, err := b.js.CreateOrUpdateStream(ctx, runEventsCfg); err != nil {
		return fmt.Errorf("orchestrationbus: ensure run events stream: %w", err)
	}

	attemptFactsCfg := jetstream.StreamConfig{
		Name:        StreamAttemptFacts,
		Description: "Memoh orchestration attempt fact ingress",
		Subjects:    []string{SubjectAllAttemptFacts},
		Retention:   jetstream.LimitsPolicy,
		Storage:     jetstream.FileStorage,
		Replicas:    b.cfg.Replicas,
		MaxAge:      b.cfg.AttemptFactMaxAge,
		Discard:     jetstream.DiscardOld,
		Duplicates:  2 * time.Minute,
	}
	if _, err := b.js.CreateOrUpdateStream(ctx, attemptFactsCfg); err != nil {
		return fmt.Errorf("orchestrationbus: ensure attempt facts stream: %w", err)
	}
	return nil
}

// PublishRunEvent serialises the envelope and publishes it on the run events
// stream using the EventID as MsgID to deduplicate retries.
func (b *JetStreamBus) PublishRunEvent(ctx context.Context, env RunEventEnvelope) error {
	if err := env.Validate(); err != nil {
		return err
	}
	env.SchemaVersion = EnvelopeVersion
	if env.PublishedAt.IsZero() {
		env.PublishedAt = time.Now().UTC()
	}

	payload, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("orchestrationbus: marshal run event: %w", err)
	}

	subject := RunEventSubject(env.RunID, env.Type)
	if _, err := b.js.Publish(ctx, subject, payload, jetstream.WithMsgID(env.EventID)); err != nil {
		return fmt.Errorf("orchestrationbus: publish run event: %w", err)
	}
	return nil
}

// PublishAttemptFact publishes a fact deduplicated by FactID.
func (b *JetStreamBus) PublishAttemptFact(ctx context.Context, env AttemptFactEnvelope) error {
	if err := env.Validate(); err != nil {
		return err
	}
	env.SchemaVersion = EnvelopeVersion
	if env.ObservedAt.IsZero() {
		env.ObservedAt = time.Now().UTC()
	}

	payload, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("orchestrationbus: marshal attempt fact: %w", err)
	}

	subject := AttemptFactSubject(env.RunID, env.AttemptID, env.Type)
	if _, err := b.js.Publish(ctx, subject, payload, jetstream.WithMsgID(env.FactID)); err != nil {
		return fmt.Errorf("orchestrationbus: publish attempt fact: %w", err)
	}
	return nil
}

// SubscribeRunEvents creates an ephemeral ordered consumer scoped to the
// run-event subject for the given run.
func (b *JetStreamBus) SubscribeRunEvents(ctx context.Context, runID string) (RunEventSubscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, ErrSubscriptionClosed
	}
	b.mu.Unlock()

	consumer, err := b.js.OrderedConsumer(ctx, StreamRunEvents, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{RunEventRunSubject(runID)},
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrationbus: create run event consumer: %w", err)
	}

	sub := &jsRunSub{
		bus:    b,
		runID:  runID,
		ch:     make(chan RunEventEnvelope, defaultInMemBuffer),
		closed: make(chan struct{}),
	}

	cc, err := consumer.Consume(func(msg jetstream.Msg) {
		var env RunEventEnvelope
		if err := json.Unmarshal(msg.Data(), &env); err != nil {
			b.logger.Warn("decode run event envelope failed", slog.Any("error", err), slog.String("subject", msg.Subject()))
			_ = msg.Ack()
			return
		}
		select {
		case sub.ch <- env:
		case <-sub.closed:
		}
		_ = msg.Ack()
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrationbus: consume run events: %w", err)
	}
	sub.cc = cc

	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()

	return sub, nil
}

// SubscribeAttemptFacts creates an ephemeral ordered consumer for attempt
// facts under one run.
func (b *JetStreamBus) SubscribeAttemptFacts(ctx context.Context, runID string) (AttemptFactSubscription, error) {
	return b.subscribeAttemptFacts(ctx, AttemptFactRunSubject(runID), runID, jetstream.DeliverNewPolicy)
}

// SubscribeAllAttemptFacts opens an ordered consumer that sees every attempt
// fact across runs. Used by the kernel fact loop.
func (b *JetStreamBus) SubscribeAllAttemptFacts(ctx context.Context) (AttemptFactSubscription, error) {
	return b.subscribeAttemptFacts(ctx, SubjectAllAttemptFacts, "", jetstream.DeliverNewPolicy)
}

func (b *JetStreamBus) subscribeAttemptFacts(ctx context.Context, filter, runID string, deliverPolicy jetstream.DeliverPolicy) (AttemptFactSubscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, ErrSubscriptionClosed
	}
	b.mu.Unlock()

	consumer, err := b.js.OrderedConsumer(ctx, StreamAttemptFacts, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{filter},
		DeliverPolicy:  deliverPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrationbus: create attempt fact consumer: %w", err)
	}

	sub := &jsFactSub{
		bus:    b,
		runID:  runID,
		ch:     make(chan AttemptFactEnvelope, defaultInMemBuffer),
		closed: make(chan struct{}),
	}

	cc, err := consumer.Consume(func(msg jetstream.Msg) {
		var env AttemptFactEnvelope
		if err := json.Unmarshal(msg.Data(), &env); err != nil {
			b.logger.Warn("decode attempt fact envelope failed", slog.Any("error", err), slog.String("subject", msg.Subject()))
			_ = msg.Ack()
			return
		}
		select {
		case sub.ch <- env:
		case <-sub.closed:
		}
		_ = msg.Ack()
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrationbus: consume attempt facts: %w", err)
	}
	sub.cc = cc

	b.mu.Lock()
	b.fsubs[sub] = struct{}{}
	b.mu.Unlock()

	return sub, nil
}

// Close stops outstanding subscriptions and drains the NATS connection.
func (b *JetStreamBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	subs := b.subs
	fsubs := b.fsubs
	b.subs = map[*jsRunSub]struct{}{}
	b.fsubs = map[*jsFactSub]struct{}{}
	b.mu.Unlock()

	for sub := range subs {
		_ = sub.closeInternal()
	}
	for sub := range fsubs {
		_ = sub.closeInternal()
	}

	if b.conn != nil {
		_ = b.conn.Drain()
	}
	return nil
}

func (b *JetStreamBus) removeRunSub(sub *jsRunSub) {
	b.mu.Lock()
	delete(b.subs, sub)
	b.mu.Unlock()
}

func (b *JetStreamBus) removeFactSub(sub *jsFactSub) {
	b.mu.Lock()
	delete(b.fsubs, sub)
	b.mu.Unlock()
}

type jsRunSub struct {
	bus    *JetStreamBus
	runID  string
	ch     chan RunEventEnvelope
	cc     jetstream.ConsumeContext
	closed chan struct{}
	once   sync.Once
}

func (s *jsRunSub) Events() <-chan RunEventEnvelope { return s.ch }

func (s *jsRunSub) Close() error {
	err := s.closeInternal()
	if s.bus != nil {
		s.bus.removeRunSub(s)
	}
	return err
}

func (s *jsRunSub) closeInternal() error {
	s.once.Do(func() {
		if s.cc != nil {
			s.cc.Stop()
		}
		close(s.closed)
		close(s.ch)
	})
	return nil
}

type jsFactSub struct {
	bus    *JetStreamBus
	runID  string
	ch     chan AttemptFactEnvelope
	cc     jetstream.ConsumeContext
	closed chan struct{}
	once   sync.Once
}

func (s *jsFactSub) Facts() <-chan AttemptFactEnvelope { return s.ch }

func (s *jsFactSub) Close() error {
	err := s.closeInternal()
	if s.bus != nil {
		s.bus.removeFactSub(s)
	}
	return err
}

func (s *jsFactSub) closeInternal() error {
	s.once.Do(func() {
		if s.cc != nil {
			s.cc.Stop()
		}
		close(s.closed)
		close(s.ch)
	})
	return nil
}
