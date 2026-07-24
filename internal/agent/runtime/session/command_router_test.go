package sessionruntime

import (
	"context"
	"errors"
	"testing"
)

func TestCommandRouterDispatchesRegisteredRouteBeforeFallback(t *testing.T) {
	router := NewCommandRouter()
	var handled string
	fallbackCalls := 0
	if err := router.Register(" approval.response ", func(_ context.Context, command Command) error {
		handled = command.ID
		return nil
	}, nil); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	router.setFallbackHandler(func(context.Context, Command) error {
		fallbackCalls++
		return errors.New("fallback should not run")
	})

	if err := router.Handle(context.Background(), Command{Type: "approval.response", ID: "cmd-1"}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if handled != "cmd-1" || fallbackCalls != 0 {
		t.Fatalf("dispatch = handled:%q fallback:%d", handled, fallbackCalls)
	}
}

func TestCommandRouterReplacesRouteAndKeepsReconcilerScoped(t *testing.T) {
	router := NewCommandRouter()
	firstCalls := 0
	secondCalls := 0
	if err := router.Register("decision", func(context.Context, Command) error {
		firstCalls++
		return nil
	}, func(context.Context, Command) (bool, error) {
		return true, nil
	}); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if err := router.Register("decision", func(context.Context, Command) error {
		secondCalls++
		return nil
	}, func(context.Context, Command) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("second Register() error = %v", err)
	}

	if err := router.Handle(context.Background(), Command{Type: "decision"}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	reconciled, err := router.Reconcile(context.Background(), Command{Type: "decision"})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if reconciled {
		t.Fatal("Reconcile() = true after replacement, want false")
	}
	if firstCalls != 0 || secondCalls != 1 {
		t.Fatalf("route calls = first:%d second:%d", firstCalls, secondCalls)
	}
}

func TestCommandRouterFallbackSettersDoNotOverwriteEachOther(t *testing.T) {
	router := NewCommandRouter()
	wantErr := errors.New("reconciled")
	router.setFallbackReconciler(func(context.Context, Command) (bool, error) {
		return true, wantErr
	})
	router.setFallbackHandler(func(context.Context, Command) error { return nil })

	if err := router.Handle(context.Background(), Command{Type: "unregistered"}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	reconciled, err := router.Reconcile(context.Background(), Command{Type: "unregistered"})
	if !reconciled || !errors.Is(err, wantErr) {
		t.Fatalf("Reconcile() = (%v, %v), want (true, %v)", reconciled, err, wantErr)
	}
}

func TestCommandRouterTypedRouteWithoutReconcilerDoesNotUseFallback(t *testing.T) {
	router := NewCommandRouter()
	if err := router.Register("decision", func(context.Context, Command) error { return nil }, nil); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	fallbackCalls := 0
	router.setFallbackReconciler(func(context.Context, Command) (bool, error) {
		fallbackCalls++
		return true, nil
	})

	reconciled, err := router.Reconcile(context.Background(), Command{Type: "decision"})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if reconciled || fallbackCalls != 0 {
		t.Fatalf("Reconcile() = %v, fallback calls = %d; want false, 0", reconciled, fallbackCalls)
	}
}

func TestCommandRouterRejectsInvalidRegistration(t *testing.T) {
	router := NewCommandRouter()
	if err := router.Register(" ", func(context.Context, Command) error { return nil }, nil); err == nil {
		t.Fatal("Register() accepted an empty command type")
	}
	if err := router.Register("command", nil, nil); !errors.Is(err, ErrCommandHandlerNotConfigured) {
		t.Fatalf("Register() error = %v, want ErrCommandHandlerNotConfigured", err)
	}
}
