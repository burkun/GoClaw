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

// TestLLMErrorHandlingMiddleware_ClassifyError tests error classification.
func TestLLMErrorHandlingMiddleware_ClassifyError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantRetriable bool
		wantReason    string
	}{
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

// TestLLMErrorHandlingMiddleware_RetryDelay tests retry delay calculation.
func TestLLMErrorHandlingMiddleware_RetryDelay(t *testing.T) {
	m := NewLLMErrorHandlingMiddleware(3)

	tests := []struct {
		name     string
		attempt  int
		wantMin  time.Duration
		wantMax  time.Duration
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
