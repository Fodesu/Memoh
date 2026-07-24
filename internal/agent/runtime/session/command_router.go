package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var ErrCommandHandlerNotConfigured = errors.New("runtime command handler is not configured")

// CommandHandler applies one owner-local runtime command.
type CommandHandler func(context.Context, Command) error

// CommandReconciler checks whether a durable command was already applied.
// It must not require local run control and must not repeat side effects.
type CommandReconciler func(context.Context, Command) (bool, error)

type commandRoute struct {
	handle    CommandHandler
	reconcile CommandReconciler
}

// CommandRouter dispatches owner commands by type. The runtime transport owns
// delivery and deduplication; registered handlers own command behavior.
type CommandRouter struct {
	mu sync.RWMutex

	routes            map[string]commandRoute
	fallbackHandler   CommandHandler
	fallbackReconcile CommandReconciler
}

func NewCommandRouter() *CommandRouter {
	return &CommandRouter{routes: make(map[string]commandRoute)}
}

// Register installs the route for one command type. Re-registering a type is
// an atomic replacement, which supports setter-based composition in tests and
// embedded deployments without exposing a partially updated route.
func (r *CommandRouter) Register(commandType string, handler CommandHandler, reconciler CommandReconciler) error {
	if r == nil {
		return ErrCommandHandlerNotConfigured
	}
	commandType = strings.TrimSpace(commandType)
	if commandType == "" {
		return errors.New("runtime command type is required")
	}
	if handler == nil {
		return fmt.Errorf("%w for %q", ErrCommandHandlerNotConfigured, commandType)
	}
	r.mu.Lock()
	if r.routes == nil {
		r.routes = make(map[string]commandRoute)
	}
	r.routes[commandType] = commandRoute{handle: handler, reconcile: reconciler}
	r.mu.Unlock()
	return nil
}

func (r *CommandRouter) Handle(ctx context.Context, command Command) error {
	if r == nil {
		return ErrCommandHandlerNotConfigured
	}
	r.mu.RLock()
	route, ok := r.routes[strings.TrimSpace(command.Type)]
	fallback := r.fallbackHandler
	r.mu.RUnlock()
	if ok && route.handle != nil {
		return route.handle(ctx, command)
	}
	if fallback != nil {
		return fallback(ctx, command)
	}
	return fmt.Errorf("%w for %q", ErrCommandHandlerNotConfigured, command.Type)
}

func (r *CommandRouter) Reconcile(ctx context.Context, command Command) (bool, error) {
	if r == nil {
		return false, nil
	}
	r.mu.RLock()
	route, ok := r.routes[strings.TrimSpace(command.Type)]
	fallback := r.fallbackReconcile
	r.mu.RUnlock()
	if ok {
		if route.reconcile == nil {
			return false, nil
		}
		return route.reconcile(ctx, command)
	}
	if fallback != nil {
		return fallback(ctx, command)
	}
	return false, nil
}

func (r *CommandRouter) setFallbackHandler(handler CommandHandler) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.fallbackHandler = handler
	r.mu.Unlock()
}

func (r *CommandRouter) setFallbackReconciler(reconciler CommandReconciler) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.fallbackReconcile = reconciler
	r.mu.Unlock()
}
