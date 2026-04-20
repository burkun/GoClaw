// Package llmerror implements LLMErrorHandlingMiddleware for GoClaw.
//
// LLMErrorHandlingMiddleware wraps LLM model calls with retry logic for transient errors,
// providing exponential backoff and graceful user-facing fallback messages.
// This mirrors DeerFlow's LLMErrorHandlingMiddleware implementation.
package llmerror

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"goclaw/internal/logging"
	"goclaw/internal/middleware"
)

// Retriable HTTP status codes.
var retriableStatusCodes = map[int]bool{
	408: true, // Request Timeout
	409: true, // Conflict
	425: true, // Too Early
	429: true, // Too Many Requests
	500: true, // Internal Server Error
	502: true, // Bad Gateway
	503: true, // Service Unavailable
	504: true, // Gateway Timeout
}

// Busy patterns that indicate transient server overload.
var busyPatterns = []string{
	"server busy",
	"temporarily unavailable",
	"try again later",
	"please retry",
	"please try again",
	"overloaded",
	"high demand",
	"rate limit",
	"负载较高",
	"服务繁忙",
	"稍后重试",
	"请稍后重试",
}

// Quota patterns that indicate billing/quota issues.
var quotaPatterns = []string{
	"insufficient_quota",
	"quota",
	"billing",
	"credit",
	"payment",
	"余额不足",
	"超出限额",
	"额度不足",
	"欠费",
}

// Auth patterns that indicate authentication issues.
var authPatterns = []string{
	"authentication",
	"unauthorized",
	"invalid api key",
	"invalid_api_key",
	"permission",
	"forbidden",
	"access denied",
	"无权",
	"未授权",
}

// LLMErrorHandlingMiddleware handles LLM call errors with retry/backoff logic.
type LLMErrorHandlingMiddleware struct {
	middleware.MiddlewareWrapper
	// MaxAttempts is the maximum number of retry attempts (including initial call).
	MaxAttempts int
	// BaseDelayMS is the base delay in milliseconds for exponential backoff.
	BaseDelayMS int
	// CapDelayMS is the maximum delay cap in milliseconds.
	CapDelayMS int
}

// NewLLMErrorHandlingMiddleware constructs a LLMErrorHandlingMiddleware.
func NewLLMErrorHandlingMiddleware(maxAttempts int) *LLMErrorHandlingMiddleware {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return &LLMErrorHandlingMiddleware{
		MaxAttempts: maxAttempts,
		BaseDelayMS: 1000,
		CapDelayMS:  8000,
	}
}

// Name implements middleware.Middleware.
func (m *LLMErrorHandlingMiddleware) Name() string { return "LLMErrorHandlingMiddleware" }

// BeforeModel is a no-op.
func (m *LLMErrorHandlingMiddleware) BeforeModel(_ context.Context, _ *middleware.State) error {
	return nil
}

// AfterModel is a no-op (error handling is done in WrapModel).
func (m *LLMErrorHandlingMiddleware) AfterModel(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

// classifyError determines if an error is retriable and categorizes it.
// Returns (retriable, reason) where reason is one of: quota, auth, transient, busy, generic.
func classifyError(err error) (bool, string) {
	if err == nil {
		return false, ""
	}

	detail := extractErrorDetail(err)
	lowered := strings.ToLower(detail)
	errorCode := extractErrorCode(err)
	statusCode := extractStatusCode(err)

	// Check for quota issues (non-retriable)
	if matchesAny(lowered, quotaPatterns) || matchesAny(strings.ToLower(errorCode), quotaPatterns) {
		return false, "quota"
	}

	// Check for auth issues (non-retriable)
	if matchesAny(lowered, authPatterns) {
		return false, "auth"
	}

	// Check for well-known retriable exception types
	errName := getErrorTypeName(err)
	if errName == "APITimeoutError" || errName == "APIConnectionError" || errName == "InternalServerError" {
		return true, "transient"
	}

	// Check for retriable status codes
	if retriableStatusCodes[statusCode] {
		return true, "transient"
	}

	// Check for busy patterns
	if matchesAny(lowered, busyPatterns) {
		return true, "busy"
	}

	return false, "generic"
}

// buildRetryDelay calculates the delay before the next retry attempt.
func (m *LLMErrorHandlingMiddleware) buildRetryDelay(attempt int, err error) time.Duration {
	// First, check for Retry-After header
	if retryAfter := extractRetryAfterMS(err); retryAfter > 0 {
		return time.Duration(retryAfter) * time.Millisecond
	}

	// Exponential backoff: base * 2^(attempt-1)
	backoff := m.BaseDelayMS * (1 << uint(attempt-1))
	if backoff > m.CapDelayMS {
		backoff = m.CapDelayMS
	}
	return time.Duration(backoff) * time.Millisecond
}

// buildRetryMessage creates a log message for retry attempts.
func (m *LLMErrorHandlingMiddleware) buildRetryMessage(attempt int, waitMS int, reason string) string {
	seconds := max(1, waitMS/1000)
	reasonText := "provider is busy"
	if reason != "busy" {
		reasonText = "provider request failed temporarily"
	}
	return fmt.Sprintf("LLM request retry %d/%d: %s. Retrying in %ds.", attempt, m.MaxAttempts, reasonText, seconds)
}

// buildUserMessage creates a user-friendly error message.
func buildUserMessage(err error, reason string) string {
	detail := extractErrorDetail(err)
	switch reason {
	case "quota":
		return "The configured LLM provider rejected the request because the account is out of quota, billing is unavailable, or usage is restricted. Please fix the provider account and try again."
	case "auth":
		return "The configured LLM provider rejected the request because authentication or access is invalid. Please check the provider credentials and try again."
	case "busy", "transient":
		return "The configured LLM provider is temporarily unavailable after multiple retries. Please wait a moment and continue the conversation."
	default:
		return fmt.Sprintf("LLM request failed: %s", detail)
	}
}

// extractErrorDetail extracts the error message from various error types.
func extractErrorDetail(err error) string {
	if err == nil {
		return ""
	}

	detail := strings.TrimSpace(err.Error())
	if detail != "" {
		return detail
	}

	return getErrorTypeName(err)
}

// extractErrorCode extracts error code from error struct.
func extractErrorCode(err error) string {
	// Try common error code fields via type assertion
	type coder interface{ Code() string }
	if c, ok := err.(coder); ok {
		return c.Code()
	}

	type errorCodeer interface{ ErrorCode() string }
	if c, ok := err.(errorCodeer); ok {
		return c.ErrorCode()
	}

	return ""
}

// extractStatusCode extracts HTTP status code from error.
func extractStatusCode(err error) int {
	// Try common status code fields
	type statusCoder interface{ StatusCode() int }
	if c, ok := err.(statusCoder); ok {
		return c.StatusCode()
	}

	type statusHolder interface{ Status() int }
	if c, ok := err.(statusHolder); ok {
		return c.Status()
	}

	// Try to extract from response field
	type responseHolder interface{ Response() *http.Response }
	if r, ok := err.(responseHolder); ok && r.Response() != nil {
		return r.Response().StatusCode
	}

	return 0
}

// extractRetryAfterMS extracts Retry-After header value in milliseconds.
func extractRetryAfterMS(err error) int {
	type responseHolder interface{ Response() *http.Response }
	r, ok := err.(responseHolder)
	if !ok || r.Response() == nil {
		return 0
	}

	resp := r.Response()
	if resp.Header == nil {
		return 0
	}

	// Try retry-after-ms first
	for _, key := range []string{"retry-after-ms", "Retry-After-Ms"} {
		if v := resp.Header.Get(key); v != "" {
			if ms, err := strconv.Atoi(v); err == nil {
				return ms
			}
		}
	}

	// Try retry-after (seconds)
	for _, key := range []string{"retry-after", "Retry-After"} {
		if v := resp.Header.Get(key); v != "" {
			if sec, err := strconv.Atoi(v); err == nil {
				return sec * 1000
			}
			// Try parsing as date
			if t, err := http.ParseTime(v); err == nil {
				delta := time.Until(t)
				if delta > 0 {
					return int(delta.Milliseconds())
				}
			}
		}
	}

	return 0
}

// getErrorTypeName returns the error type name.
func getErrorTypeName(err error) string {
	if err == nil {
		return ""
	}
	// Get the type name without package path
	s := fmt.Sprintf("%T", err)
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// matchesAny checks if s contains any of the patterns.
func matchesAny(s string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// max returns the larger of a and b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// WrapModel creates a model wrapper with retry logic.
// This is called by the EinoMiddlewareAdapter.
func (m *LLMErrorHandlingMiddleware) WrapModel(inner model.BaseChatModel) model.BaseChatModel {
	return &retryModelWrapper{
		inner:       inner,
		maxAttempts: m.MaxAttempts,
		baseDelayMS: m.BaseDelayMS,
		capDelayMS:  m.CapDelayMS,
	}
}

// retryModelWrapper wraps a BaseChatModel with retry logic.
type retryModelWrapper struct {
	inner       model.BaseChatModel
	maxAttempts int
	baseDelayMS int
	capDelayMS  int
}

// Generate implements model.BaseChatModel with retry logic.
func (w *retryModelWrapper) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	attempt := 1
	for {
		result, err := w.inner.Generate(ctx, input, opts...)
		if err == nil {
			return result, nil
		}

		retriable, reason := classifyError(err)
		if retriable && attempt < w.maxAttempts {
			delay := w.buildRetryDelay(attempt, err)
			logging.Warn("[LLM] Transient error, retrying",
				"attempt", attempt,
				"max_attempts", w.maxAttempts,
				"delay", delay,
				"error", extractErrorDetail(err))
			time.Sleep(delay)
			attempt++
			continue
		}

		logging.Warn("[LLM] Call failed after attempts",
			"attempts", attempt,
			"error", extractErrorDetail(err))

		// Return a friendly error message
		return schema.AssistantMessage(buildUserMessage(err, reason), nil), nil
	}
}

// Stream implements model.BaseChatModel with retry logic.
func (w *retryModelWrapper) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	attempt := 1
	for {
		stream, err := w.inner.Stream(ctx, input, opts...)
		if err == nil {
			return stream, nil
		}

		retriable, reason := classifyError(err)
		if retriable && attempt < w.maxAttempts {
			delay := w.buildRetryDelay(attempt, err)
			logging.Warn("[LLM] Transient error, retrying",
				"attempt", attempt,
				"max_attempts", w.maxAttempts,
				"delay", delay,
				"error", extractErrorDetail(err))
			time.Sleep(delay)
			attempt++
			continue
		}

		logging.Warn("[LLM] Stream failed after attempts",
			"attempts", attempt,
			"error", extractErrorDetail(err))

		// Return a stream with friendly error message
		msg := schema.AssistantMessage(buildUserMessage(err, reason), nil)
		reader, writer := schema.Pipe[*schema.Message](1)
		go func() {
			defer writer.Close()
			writer.Send(msg, nil)
		}()
		return reader, nil
	}
}

// BindTools implements model.BaseChatModel.
func (w *retryModelWrapper) BindTools(tools []*schema.ToolInfo) error {
	if binder, ok := w.inner.(interface {
		BindTools([]*schema.ToolInfo) error
	}); ok {
		return binder.BindTools(tools)
	}
	return nil
}

func (w *retryModelWrapper) buildRetryDelay(attempt int, err error) time.Duration {
	// First, check for Retry-After header
	if retryAfter := extractRetryAfterMS(err); retryAfter > 0 {
		return time.Duration(retryAfter) * time.Millisecond
	}

	// Exponential backoff: base * 2^(attempt-1)
	backoff := w.baseDelayMS * (1 << uint(attempt-1))
	if backoff > w.capDelayMS {
		backoff = w.capDelayMS
	}
	return time.Duration(backoff) * time.Millisecond
}

var _ middleware.Middleware = (*LLMErrorHandlingMiddleware)(nil)
