package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/felinics/twilight/sdk"
)

// TestStreamFailureCarriesAPIErrorStatus pins the twilight #63 plumbing: a
// provider failure whose chain carries *sdk.APIError crosses the event
// boundary with the upstream HTTP status on ErrorStatusCode, so the
// application layer classifies by status code instead of parsing the text.
// The field is internal and must never reach the serialized wire shape.
func TestStreamFailureCarriesAPIErrorStatus(t *testing.T) {
	t.Parallel()

	eng := &streamEngine{
		streamCtx: context.Background(),
		events:    make(chan StreamEvent, 4),
	}
	providerErr := fmt.Errorf("anthropic: stream failed: %w", &sdk.APIError{
		StatusCode: http.StatusTooManyRequests,
		Message:    "rate limited",
	})

	msg, retriable := eng.streamFailure(providerErr)
	if msg == "" || !retriable {
		t.Fatalf("streamFailure() = (%q, %t), want message and retryable", msg, retriable)
	}

	ev := <-eng.events
	if ev.Type != EventError || ev.Error == "" {
		t.Fatalf("streamFailure() event = %+v, want EventError with text", ev)
	}
	if ev.ErrorStatusCode != http.StatusTooManyRequests {
		t.Fatalf("ErrorStatusCode = %d, want %d", ev.ErrorStatusCode, http.StatusTooManyRequests)
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if strings.Contains(string(data), "ErrorStatusCode") {
		t.Fatalf("serialized event leaked the internal status field: %s", data)
	}
}

// TestStreamFailureLeavesStatusUnsetForNonAPIError pins the fallback: a
// failure without an sdk.APIError in its chain emits the text-only event the
// application layer classifies by pattern, with no status attached.
func TestStreamFailureLeavesStatusUnsetForNonAPIError(t *testing.T) {
	t.Parallel()

	eng := &streamEngine{
		streamCtx: context.Background(),
		events:    make(chan StreamEvent, 4),
	}

	// A non-retryable failure aborts the run, so the engine reports no
	// retriable message; the event itself still carries the failure text.
	msg, retriable := eng.streamFailure(errors.New("stream start: request rejected"))
	if msg != "" || retriable {
		t.Fatalf("streamFailure() = (%q, %t), want empty non-retryable result", msg, retriable)
	}

	ev := <-eng.events
	if ev.ErrorStatusCode != 0 {
		t.Fatalf("ErrorStatusCode = %d, want 0 for a non-APIError failure", ev.ErrorStatusCode)
	}
}
