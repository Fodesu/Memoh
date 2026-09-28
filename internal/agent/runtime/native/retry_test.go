package native

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/felinics/twilight/sdk"
)

func TestRetryableStreamErrorSeparatesProviderStatusFromApplicationTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "provider 504", err: errors.New("api error 504: gateway timeout"), want: true},
		{name: "provider 524", err: errors.New("api error 524: origin timeout"), want: true},
		{name: "timeout wording alone", err: errors.New("request timeout label only"), want: false},
		{name: "application deadline", err: context.DeadlineExceeded, want: false},
		{name: "explicit cancellation", err: context.Canceled, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isRetryableStreamError(tt.err); got != tt.want {
				t.Fatalf("isRetryableStreamError(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestRetryableStreamErrorReadsAPIErrorStatus pins the twilight #63 contract:
// a provider failure crosses as *sdk.APIError wrapped with %w, and the retry
// decision reads StatusCode from the chain. Non-2xx statuses other than 429
// and 5xx fail again on the next attempt, whatever the message wording says.
func TestRetryableStreamErrorReadsAPIErrorStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "wrapped 429",
			err:  fmt.Errorf("anthropic: stream failed: %w", &sdk.APIError{StatusCode: http.StatusTooManyRequests, Message: "rate limited"}),
			want: true,
		},
		{
			name: "wrapped 503",
			err:  fmt.Errorf("openai: stream failed: %w", &sdk.APIError{StatusCode: http.StatusServiceUnavailable}),
			want: true,
		},
		{
			name: "wrapped 401",
			err:  fmt.Errorf("openai: stream failed: %w", &sdk.APIError{StatusCode: http.StatusUnauthorized, Message: "incorrect api key"}),
			want: false,
		},
		{
			name: "wrapped 400",
			err:  fmt.Errorf("anthropic: stream failed: %w", &sdk.APIError{StatusCode: http.StatusBadRequest, Message: "bad request"}),
			want: false,
		},
		{
			// The status code wins over rate-limit wording: a 400 cannot be
			// ridden out by re-sending the same request.
			name: "wrapped 400 with rate limit wording",
			err:  fmt.Errorf("anthropic: stream failed: %w", &sdk.APIError{StatusCode: http.StatusBadRequest, Message: "usage limit reached"}),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isRetryableStreamError(tt.err); got != tt.want {
				t.Fatalf("isRetryableStreamError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestRetryableStreamErrorReadsAPIErrorStatusDirect pins the unwrapped form:
// sdk.ErrorPart.Error reaches the engine as the bare *sdk.APIError.
func TestRetryableStreamErrorReadsAPIErrorStatusDirect(t *testing.T) {
	t.Parallel()

	if got := isRetryableStreamError(&sdk.APIError{StatusCode: http.StatusTooManyRequests}); !got {
		t.Fatal("bare *sdk.APIError 429 = false, want true")
	}
	if got := isRetryableStreamError(&sdk.APIError{StatusCode: http.StatusForbidden}); got {
		t.Fatal("bare *sdk.APIError 403 = true, want false")
	}
}

// TestRetryDelayToleratesUnsetDelayFields pins the misconfiguration guard:
// callers may set only MaxAttempts (leaving the delay fields at their zero
// values), and retryDelay must fire immediately instead of panicking inside
// rand.Int64N on a non-positive argument.
func TestRetryDelayToleratesUnsetDelayFields(t *testing.T) {
	t.Parallel()

	if got := retryDelay(2, RetryConfig{MaxAttempts: 3}); got != 0 {
		t.Fatalf("retryDelay(2, delays unset) = %v, want 0", got)
	}
	nano := RetryConfig{MaxAttempts: 3, BaseDelay: time.Nanosecond, MaxDelay: time.Nanosecond}
	if got := retryDelay(2, nano); got != 0 {
		t.Fatalf("retryDelay(2, 1ns delays) = %v, want 0 (delay/2 == 0 must not reach Int64N)", got)
	}
}
