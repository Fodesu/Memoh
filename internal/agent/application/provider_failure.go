package application

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/felinics/memoh/internal/apperror"
)

// Provider stream failures reach the application layer as the provider's own
// text: the runtime forwards `StreamEvent.Error`, plus ErrorStatusCode when
// the error chain carries an sdk.APIError. Where the status code is present
// it decides (401/403 auth, 402 quota, 429 rate limit, 5xx overload); the
// patterns below remain as the fallback for failures reported only as text.
// Together they name the four conditions a user can act on. Everything else
// keeps the generic interrupted response.
var (
	providerAuthPattern = regexp.MustCompile(
		`(?i)api error 40[13]\b|invalid[ _-]api[ _-]key|invalid_request_error: incorrect api key|authentication_error|unauthorized|permission_denied`,
	)
	providerQuotaPattern = regexp.MustCompile(
		`(?i)api error 402\b|insufficient[ _-](balance|quota|credit|funds)|exceeded your current quota|billing_(hard_limit|not_active)|payment required`,
	)
	providerRateLimitPattern = regexp.MustCompile(
		`(?i)(^|[^0-9])429($|[^0-9])|rate[ _-]?limit|too many requests|usage limit reached`,
	)
	providerOverloadPattern = regexp.MustCompile(
		`(?i)server_is_overloaded|overloaded_error|\boverloaded\b|api error 50[0234]\b|service unavailable|temporarily unavailable`,
	)
)

// providerFailureCode names the provider condition a failure describes, or
// returns an empty code when neither the status nor the text identifies one.
// statusCode is the upstream HTTP status carried on the stream event, or zero
// when the failure arrived without one; where the status is known it decides,
// since it names the condition more reliably than provider wording.
func providerFailureCode(detail string, statusCode int) apperror.Code {
	detail = strings.TrimSpace(detail)
	if detail == "" && statusCode <= 0 {
		return ""
	}
	if statusCode > 0 {
		return providerFailureCodeForStatus(detail, statusCode)
	}
	switch {
	case providerAuthPattern.MatchString(detail):
		return apperror.CodeAgentProviderAuthFailed
	case providerQuotaPattern.MatchString(detail):
		return apperror.CodeAgentProviderQuotaExhausted
	case providerRateLimitPattern.MatchString(detail):
		return apperror.CodeAgentProviderRateLimited
	case providerOverloadPattern.MatchString(detail):
		return apperror.CodeAgentProviderOverloaded
	default:
		return ""
	}
}

// providerFailureCodeForStatus classifies a failure whose upstream HTTP
// status is known. The status decides, except on 429: quota wording arrives
// with a 429 often enough ("exceeded your current quota") that the text is
// consulted there to send the user to billing instead of back into waiting.
// Other 4xx requests fail again unchanged, so they keep the generic
// interrupted response.
func providerFailureCodeForStatus(detail string, statusCode int) apperror.Code {
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return apperror.CodeAgentProviderAuthFailed
	case statusCode == http.StatusPaymentRequired:
		return apperror.CodeAgentProviderQuotaExhausted
	case statusCode == http.StatusTooManyRequests:
		if providerQuotaPattern.MatchString(detail) {
			return apperror.CodeAgentProviderQuotaExhausted
		}
		return apperror.CodeAgentProviderRateLimited
	case statusCode >= 500:
		return apperror.CodeAgentProviderOverloaded
	default:
		return ""
	}
}
