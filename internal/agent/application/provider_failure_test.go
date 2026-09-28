package application

import (
	"testing"

	"github.com/felinics/memoh/internal/agent/runtime/native"
	"github.com/felinics/memoh/internal/apperror"
)

func TestProviderFailureCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		detail     string
		statusCode int
		want       apperror.Code
	}{
		{
			name:   "codex overload",
			detail: "openai-codex: server_is_overloaded: Our servers are currently overloaded. Please try again later.",
			want:   apperror.CodeAgentProviderOverloaded,
		},
		{
			name:   "anthropic overload",
			detail: "anthropic: api error 529: {\"type\":\"overloaded_error\"}",
			want:   apperror.CodeAgentProviderOverloaded,
		},
		{
			name:   "upstream 503",
			detail: "openai: stream failed: api error 503: Service Unavailable",
			want:   apperror.CodeAgentProviderOverloaded,
		},
		{
			name:   "insufficient balance",
			detail: "openai: stream failed: api error 402: Insufficient Balance [body: {\"error\":{}}]",
			want:   apperror.CodeAgentProviderQuotaExhausted,
		},
		{
			name:   "exhausted quota reported as a rate limit",
			detail: "openai: api error 429: You exceeded your current quota, please check your plan and billing details.",
			want:   apperror.CodeAgentProviderQuotaExhausted,
		},
		{
			name:   "rate limit",
			detail: "anthropic: api error 429: rate_limit_error: number of request tokens has exceeded your per-minute rate limit",
			want:   apperror.CodeAgentProviderRateLimited,
		},
		{
			name:   "rejected key",
			detail: "openai: stream start: api error 401: Incorrect API key provided",
			want:   apperror.CodeAgentProviderAuthFailed,
		},
		{
			name:   "a failure the provider does not name",
			detail: "stream start: unexpected EOF",
		},
		{
			name:   "no detail",
			detail: "   ",
		},
		{
			name:   "a request id that merely contains the digits",
			detail: "openai: stream failed: request req_4291 ended early",
		},
		{
			name:       "status 401 with empty detail",
			statusCode: 401,
			want:       apperror.CodeAgentProviderAuthFailed,
		},
		{
			name:       "status 403",
			detail:     "openai: stream failed",
			statusCode: 403,
			want:       apperror.CodeAgentProviderAuthFailed,
		},
		{
			name:       "status 402",
			detail:     "openai: stream failed",
			statusCode: 402,
			want:       apperror.CodeAgentProviderQuotaExhausted,
		},
		{
			// Quota wording wins over the 429 status: an exhausted balance
			// reported with a rate-limit status still sends the user to
			// billing, not to waiting.
			name:       "status 429 carrying quota wording",
			detail:     "openai: stream failed: You exceeded your current quota, please check your plan and billing details.",
			statusCode: 429,
			want:       apperror.CodeAgentProviderQuotaExhausted,
		},
		{
			name:       "status 429 plain",
			detail:     "openai: stream failed: rate_limit_error",
			statusCode: 429,
			want:       apperror.CodeAgentProviderRateLimited,
		},
		{
			name:       "status 529",
			detail:     "anthropic: stream failed",
			statusCode: 529,
			want:       apperror.CodeAgentProviderOverloaded,
		},
		{
			// The status code wins over rate-limit wording: a 400 cannot be
			// resolved by waiting.
			name:       "status 400 with rate limit wording",
			detail:     "openai: stream failed: usage limit reached",
			statusCode: 400,
			want:       "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := providerFailureCode(tc.detail, tc.statusCode); got != tc.want {
				t.Fatalf("providerFailureCode(%q, %d) = %q, want %q", tc.detail, tc.statusCode, got, tc.want)
			}
		})
	}
}

func TestAgentStreamLifecycleErrorReportsTheProviderCondition(t *testing.T) {
	t.Parallel()

	overloaded := agentStreamLifecycleError(native.StreamEvent{
		Type:  native.EventError,
		Error: "openai-codex: server_is_overloaded: Our servers are currently overloaded.",
	})
	if got := apperror.CodeOf(overloaded); got != apperror.CodeAgentProviderOverloaded {
		t.Fatalf("code = %q, want %q", got, apperror.CodeAgentProviderOverloaded)
	}

	generic := agentStreamLifecycleError(native.StreamEvent{
		Type:  native.EventError,
		Error: "agent stream failed",
	})
	if got := apperror.CodeOf(generic); got != apperror.CodeAgentResponseInterrupted {
		t.Fatalf("code = %q, want %q", got, apperror.CodeAgentResponseInterrupted)
	}

	public := agentFailureStreamEvent(overloaded)
	if public.Code != string(apperror.CodeAgentProviderOverloaded) || public.Error == "" {
		t.Fatalf("public event = %+v, want the overloaded code with a detail", public)
	}
}
