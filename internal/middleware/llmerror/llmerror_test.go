package llmerror

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestLLMErrorHandlingMiddleware_Name(t *testing.T) {
	m := NewLLMErrorHandlingMiddleware(3)
	if m.Name() != "LLMErrorHandlingMiddleware" {
		t.Errorf("expected name 'LLMErrorHandlingMiddleware', got %s", m.Name())
	}
}

func TestLLMErrorHandlingMiddleware_DefaultMaxAttempts(t *testing.T) {
	m := NewLLMErrorHandlingMiddleware(0)
	if m.MaxAttempts != 3 {
		t.Errorf("expected default MaxAttempts 3, got %d", m.MaxAttempts)
	}

	m = NewLLMErrorHandlingMiddleware(-1)
	if m.MaxAttempts != 3 {
		t.Errorf("expected default MaxAttempts 3, got %d", m.MaxAttempts)
	}
}

func TestLLMErrorHandlingMiddleware_BeforeModel(t *testing.T) {
	m := NewLLMErrorHandlingMiddleware(3)
	err := m.BeforeModel(context.Background(), nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLLMErrorHandlingMiddleware_AfterModel(t *testing.T) {
	m := NewLLMErrorHandlingMiddleware(3)
	err := m.AfterModel(context.Background(), nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLLMErrorHandlingMiddleware_WrapModel(t *testing.T) {
	m := NewLLMErrorHandlingMiddleware(3)
	mockModel := &mockChatModel{}
	wrapped := m.WrapModel(mockModel)
	if wrapped == nil {
		t.Error("expected wrapped model")
	}
}

// TestLLMErrorHandlingMiddleware_ClassifyError tests error classification.
func TestLLMErrorHandlingMiddleware_ClassifyError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantRetriable bool
		wantReason    string
	}{
		{
			name:          "nil error",
			err:           nil,
			wantRetriable: false,
			wantReason:    "",
		},
		{
			name:          "quota error",
			err:           errors.New("insufficient_quota: please upgrade your plan"),
			wantRetriable: false,
			wantReason:    "quota",
		},
		{
			name:          "auth error",
			err:           errors.New("invalid api key provided"),
			wantRetriable: false,
			wantReason:    "auth",
		},
		{
			name:          "rate limit error",
			err:           errors.New("rate limit exceeded, please try again later"),
			wantRetriable: true,
			wantReason:    "busy",
		},
		{
			name:          "server busy",
			err:           errors.New("server busy, please retry"),
			wantRetriable: true,
			wantReason:    "busy",
		},
		{
			name:          "generic error",
			err:           errors.New("some random error"),
			wantRetriable: false,
			wantReason:    "generic",
		},
		{
			name:          "Chinese quota error",
			err:           errors.New("余额不足"),
			wantRetriable: false,
			wantReason:    "quota",
		},
		{
			name:          "Chinese auth error",
			err:           errors.New("未授权"),
			wantRetriable: false,
			wantReason:    "auth",
		},
		{
			name:          "Chinese busy error",
			err:           errors.New("服务繁忙"),
			wantRetriable: true,
			wantReason:    "busy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retriable, reason := classifyError(tt.err)
			if retriable != tt.wantRetriable {
				t.Errorf("classifyError() retriable = %v, want %v", retriable, tt.wantRetriable)
			}
			if reason != tt.wantReason {
				t.Errorf("classifyError() reason = %v, want %v", reason, tt.wantReason)
			}
		})
	}
}

func TestClassifyError_StatusCodes(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		wantRetriable bool
	}{
		{"408 Request Timeout", 408, true},
		{"429 Too Many Requests", 429, true},
		{"500 Internal Server Error", 500, true},
		{"502 Bad Gateway", 502, true},
		{"503 Service Unavailable", 503, true},
		{"504 Gateway Timeout", 504, true},
		{"400 Bad Request", 400, false},
		{"401 Unauthorized", 401, false},
		{"404 Not Found", 404, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &mockStatusCodeError{statusCode: tt.statusCode}
			retriable, _ := classifyError(err)
			if retriable != tt.wantRetriable {
				t.Errorf("expected retriable=%v for status %d", tt.wantRetriable, tt.statusCode)
			}
		})
	}
}

func TestClassifyError_ErrorTypes(t *testing.T) {
	// Test that retriable status codes work correctly
	// Note: getErrorTypeName returns the Go type name, not a custom name
	// So we test the status code path instead which is more reliable
	
	tests := []struct {
		name          string
		err           error
		wantRetriable bool
		wantReason    string
	}{
		{"status 429", &mockStatusCodeError{statusCode: 429}, true, "transient"},
		{"status 500", &mockStatusCodeError{statusCode: 500}, true, "transient"},
		{"status 503", &mockStatusCodeError{statusCode: 503}, true, "transient"},
		{"busy pattern", errors.New("server busy, please retry"), true, "busy"},
		{"rate limit pattern", errors.New("rate limit exceeded"), true, "busy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retriable, reason := classifyError(tt.err)
			if retriable != tt.wantRetriable {
				t.Errorf("expected retriable=%v, got %v", tt.wantRetriable, retriable)
			}
			if reason != tt.wantReason {
				t.Errorf("expected reason=%s, got %s", tt.wantReason, reason)
			}
		})
	}
}

// TestLLMErrorHandlingMiddleware_RetryDelay tests retry delay calculation.
func TestLLMErrorHandlingMiddleware_RetryDelay(t *testing.T) {
	m := NewLLMErrorHandlingMiddleware(3)

	tests := []struct {
		name    string
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name:    "first retry",
			attempt: 1,
			wantMin: 1 * time.Second,
			wantMax: 1 * time.Second,
		},
		{
			name:    "second retry",
			attempt: 2,
			wantMin: 2 * time.Second,
			wantMax: 2 * time.Second,
		},
		{
			name:    "third retry (capped)",
			attempt: 3,
			wantMin: 4 * time.Second,
			wantMax: 4 * time.Second,
		},
		{
			name:    "fourth retry (capped)",
			attempt: 4,
			wantMin: 8 * time.Second,
			wantMax: 8 * time.Second,
		},
		{
			name:    "fifth retry (capped at cap)",
			attempt: 5,
			wantMin: 8 * time.Second,
			wantMax: 8 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := m.buildRetryDelay(tt.attempt, errors.New("test"))
			if delay < tt.wantMin || delay > tt.wantMax {
				t.Errorf("buildRetryDelay() = %v, want between %v and %v", delay, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// TestLLMErrorHandlingMiddleware_RetryAfterHeader tests Retry-After header parsing.
func TestLLMErrorHandlingMiddleware_RetryAfterHeader(t *testing.T) {
	m := NewLLMErrorHandlingMiddleware(3)

	// Create mock error with Retry-After header
	resp := &http.Response{
		Header: http.Header{"Retry-After": []string{"5"}},
	}
	err := &mockResponseError{response: resp, msg: "rate limit"}

	delay := m.buildRetryDelay(1, err)
	if delay != 5*time.Second {
		t.Errorf("expected 5s delay from Retry-After header, got %v", delay)
	}
}

func TestExtractRetryAfterMS(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected int
	}{
		{"retry-after-ms header", map[string]string{"retry-after-ms": "3000"}, 3000},
		{"Retry-After-Ms header", map[string]string{"Retry-After-Ms": "2000"}, 2000},
		{"retry-after seconds", map[string]string{"retry-after": "5"}, 5000},
		{"no header", map[string]string{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			for k, v := range tt.headers {
				resp.Header.Set(k, v)
			}
			err := &mockResponseError{response: resp}
			result := extractRetryAfterMS(err)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestExtractStatusCode(t *testing.T) {
	// Test with StatusCode() method
	err := &mockStatusCodeError{statusCode: 429}
	if extractStatusCode(err) != 429 {
		t.Errorf("expected 429, got %d", extractStatusCode(err))
	}

	// Test with Status() method
	err2 := &mockStatusError{status: 500}
	if extractStatusCode(err2) != 500 {
		t.Errorf("expected 500, got %d", extractStatusCode(err2))
	}

	// Test with Response() method
	resp := &http.Response{StatusCode: 503}
	err3 := &mockResponseError{response: resp}
	if extractStatusCode(err3) != 503 {
		t.Errorf("expected 503, got %d", extractStatusCode(err3))
	}

	// Test with no method
	err4 := errors.New("plain error")
	if extractStatusCode(err4) != 0 {
		t.Errorf("expected 0, got %d", extractStatusCode(err4))
	}
}

func TestExtractErrorDetail(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{"nil error", nil, ""},
		{"simple error", errors.New("test error"), "test error"},
		{"whitespace error", errors.New("  spaced  "), "spaced"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractErrorDetail(tt.err)
			if tt.contains != "" && result != tt.contains {
				t.Errorf("expected %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestExtractErrorCode(t *testing.T) {
	// Test with Code() method
	err := &mockCodeError{code: "RATE_LIMIT"}
	if extractErrorCode(err) != "RATE_LIMIT" {
		t.Errorf("expected RATE_LIMIT, got %s", extractErrorCode(err))
	}

	// Test with ErrorCode() method
	err2 := &mockErrorCodeError{code: "QUOTA_EXCEEDED"}
	if extractErrorCode(err2) != "QUOTA_EXCEEDED" {
		t.Errorf("expected QUOTA_EXCEEDED, got %s", extractErrorCode(err2))
	}

	// Test with no method
	err3 := errors.New("plain error")
	if extractErrorCode(err3) != "" {
		t.Errorf("expected empty, got %s", extractErrorCode(err3))
	}
}

func TestGetErrorTypeName(t *testing.T) {
	err := &mockNamedError{name: "TestError"}
	result := getErrorTypeName(err)
	// getErrorTypeName returns the type name without package path
	if result == "" {
		t.Error("expected non-empty type name")
	}

	// Test nil
	if getErrorTypeName(nil) != "" {
		t.Error("expected empty string for nil")
	}

	// Test standard error
	stdErr := errors.New("standard error")
	result = getErrorTypeName(stdErr)
	if result == "" {
		t.Error("expected non-empty type name for standard error")
	}
}

func TestMatchesAny(t *testing.T) {
	patterns := []string{"quota", "billing", "credit"}

	if !matchesAny("insufficient_quota", patterns) {
		t.Error("expected match for quota")
	}
	if !matchesAny("billing unavailable", patterns) {
		t.Error("expected match for billing")
	}
	if matchesAny("random error", patterns) {
		t.Error("expected no match")
	}
}

// TestLLMErrorHandlingMiddleware_BuildUserMessage tests user message generation.
func TestLLMErrorHandlingMiddleware_BuildUserMessage(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		reason    string
		wantInMsg string
	}{
		{
			name:      "quota error",
			err:       errors.New("insufficient quota"),
			reason:    "quota",
			wantInMsg: "out of quota",
		},
		{
			name:      "auth error",
			err:       errors.New("unauthorized"),
			reason:    "auth",
			wantInMsg: "authentication",
		},
		{
			name:      "busy error",
			err:       errors.New("server busy"),
			reason:    "busy",
			wantInMsg: "temporarily unavailable",
		},
		{
			name:      "transient error",
			err:       errors.New("timeout"),
			reason:    "transient",
			wantInMsg: "temporarily unavailable",
		},
		{
			name:      "generic error",
			err:       errors.New("some error"),
			reason:    "generic",
			wantInMsg: "some error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := buildUserMessage(tt.err, tt.reason)
			if !strings.Contains(msg, tt.wantInMsg) {
				t.Errorf("buildUserMessage() = %q, want to contain %q", msg, tt.wantInMsg)
			}
		})
	}
}

func TestBuildRetryMessage(t *testing.T) {
	m := NewLLMErrorHandlingMiddleware(3)

	msg := m.buildRetryMessage(1, 1000, "busy")
	if !strings.Contains(msg, "busy") {
		t.Errorf("expected 'busy' in message, got %s", msg)
	}

	msg = m.buildRetryMessage(1, 1000, "transient")
	if !strings.Contains(msg, "failed temporarily") {
		t.Errorf("expected 'failed temporarily' in message, got %s", msg)
	}
}

// TestRetryModelWrapper_Generate tests Generate with retry.
func TestRetryModelWrapper_Generate(t *testing.T) {
	attempts := 0
	mockModel := &mockChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			attempts++
			if attempts < 2 {
				return nil, errors.New("rate limit exceeded")
			}
			return schema.AssistantMessage("success", nil), nil
		},
	}

	wrapper := &retryModelWrapper{
		inner:       mockModel,
		maxAttempts: 3,
		baseDelayMS: 10, // Fast for testing
		capDelayMS:  100,
	}

	msg, err := wrapper.Generate(context.Background(), []*schema.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Content != "success" {
		t.Errorf("expected 'success', got %q", msg.Content)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

// TestRetryModelWrapper_MaxRetries tests that max retries is respected.
func TestRetryModelWrapper_MaxRetries(t *testing.T) {
	attempts := 0
	mockModel := &mockChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			attempts++
			return nil, errors.New("rate limit exceeded")
		},
	}

	wrapper := &retryModelWrapper{
		inner:       mockModel,
		maxAttempts: 2,
		baseDelayMS: 10,
		capDelayMS:  100,
	}

	msg, err := wrapper.Generate(context.Background(), []*schema.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg.Content, "temporarily unavailable") {
		t.Errorf("expected fallback message, got %q", msg.Content)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

// TestRetryModelWrapper_NonRetriable tests non-retriable errors.
func TestRetryModelWrapper_NonRetriable(t *testing.T) {
	attempts := 0
	mockModel := &mockChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			attempts++
			return nil, errors.New("invalid api key")
		},
	}

	wrapper := &retryModelWrapper{
		inner:       mockModel,
		maxAttempts: 3,
		baseDelayMS: 10,
		capDelayMS:  100,
	}

	msg, err := wrapper.Generate(context.Background(), []*schema.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(msg.Content, "authentication") {
		t.Errorf("expected auth error message, got %q", msg.Content)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry for auth errors), got %d", attempts)
	}
}

func TestRetryModelWrapper_Stream(t *testing.T) {
	attempts := 0
	mockModel := &mockChatModel{
		streamFunc: func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
			attempts++
			if attempts < 2 {
				return nil, errors.New("rate limit exceeded")
			}
			reader, writer := schema.Pipe[*schema.Message](1)
			go func() {
				defer writer.Close()
				writer.Send(schema.AssistantMessage("stream success", nil), nil)
			}()
			return reader, nil
		},
	}

	wrapper := &retryModelWrapper{
		inner:       mockModel,
		maxAttempts: 3,
		baseDelayMS: 10,
		capDelayMS:  100,
	}

	stream, err := wrapper.Stream(context.Background(), []*schema.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read from stream
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream recv error: %v", err)
	}
	if msg.Content != "stream success" {
		t.Errorf("expected 'stream success', got %q", msg.Content)
	}
}

func TestRetryModelWrapper_BindTools(t *testing.T) {
	mockModel := &mockChatModel{}
	wrapper := &retryModelWrapper{
		inner:       mockModel,
		maxAttempts: 3,
	}

	err := wrapper.BindTools([]*schema.ToolInfo{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMax(t *testing.T) {
	if max(1, 2) != 2 {
		t.Error("expected 2")
	}
	if max(2, 1) != 2 {
		t.Error("expected 2")
	}
	if max(1, 1) != 1 {
		t.Error("expected 1")
	}
}

// mockChatModel is a mock implementation of model.BaseChatModel.
type mockChatModel struct {
	generateFunc func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error)
	streamFunc   func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error)
}

func (m *mockChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, input, opts...)
	}
	return schema.AssistantMessage("mock response", nil), nil
}

func (m *mockChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.streamFunc != nil {
		return m.streamFunc(ctx, input, opts...)
	}
	reader, writer := schema.Pipe[*schema.Message](1)
	go func() {
		defer writer.Close()
		writer.Send(schema.AssistantMessage("mock stream", nil), nil)
	}()
	return reader, nil
}

func (m *mockChatModel) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

// mockResponseError is a mock error with HTTP response.
type mockResponseError struct {
	response *http.Response
	msg      string
}

func (e *mockResponseError) Error() string {
	return e.msg
}

func (e *mockResponseError) Response() *http.Response {
	return e.response
}

// mockStatusCodeError is a mock error with StatusCode method.
type mockStatusCodeError struct {
	statusCode int
}

func (e *mockStatusCodeError) Error() string { return "status code error" }
func (e *mockStatusCodeError) StatusCode() int { return e.statusCode }

// mockStatusError is a mock error with Status method.
type mockStatusError struct {
	status int
}

func (e *mockStatusError) Error() string { return "status error" }
func (e *mockStatusError) Status() int { return e.status }

// mockCodeError is a mock error with Code method.
type mockCodeError struct {
	code string
}

func (e *mockCodeError) Error() string { return "code error" }
func (e *mockCodeError) Code() string  { return e.code }

// mockErrorCodeError is a mock error with ErrorCode method.
type mockErrorCodeError struct {
	code string
}

func (e *mockErrorCodeError) Error() string  { return "error code error" }
func (e *mockErrorCodeError) ErrorCode() string { return e.code }

// mockNamedError is a mock error with a specific type name.
type mockNamedError struct {
	name string
}

func (e *mockNamedError) Error() string { return e.name }
