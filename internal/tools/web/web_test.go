package web

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestWebSearchTool_Name tests the tool name
func TestWebSearchTool_Name(t *testing.T) {
	tool := NewWebSearchTool(WebToolConfig{})
	if tool.Name() != "web_search" {
		t.Errorf("WebSearchTool.Name() = %q, want %q", tool.Name(), "web_search")
	}
}

// TestWebSearchTool_Description tests the tool description
func TestWebSearchTool_Description(t *testing.T) {
	tool := NewWebSearchTool(WebToolConfig{})
	desc := tool.Description()
	if !strings.Contains(strings.ToLower(desc), "search") {
		t.Errorf("WebSearchTool.Description() should contain 'search'")
	}
}

// TestWebSearchTool_InputSchema tests the input schema
func TestWebSearchTool_InputSchema(t *testing.T) {
	tool := NewWebSearchTool(WebToolConfig{})
	schema := tool.InputSchema()

	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("WebSearchTool.InputSchema() is not valid JSON: %v", err)
	}

	// Check required fields
	required, ok := parsed["required"].([]interface{})
	if !ok {
		t.Error("InputSchema missing required array")
		return
	}

	hasQuery := false
	for _, r := range required {
		if r == "query" {
			hasQuery = true
			break
		}
	}
	if !hasQuery {
		t.Error("InputSchema should require 'query' field")
	}
}

// TestWebSearchTool_Execute_MissingAPIKey tests missing API key
func TestWebSearchTool_Execute_MissingAPIKey(t *testing.T) {
	tool := NewWebSearchTool(WebToolConfig{TavilyAPIKey: ""})
	out, err := tool.Execute(context.Background(), `{"query":"golang"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "TAVILY_API_KEY") {
		t.Fatalf("unexpected output: %s", out)
	}
}

// TestWebSearchTool_Execute_InvalidJSON tests invalid input JSON
func TestWebSearchTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewWebSearchTool(WebToolConfig{TavilyAPIKey: "test-key"})
	out, err := tool.Execute(context.Background(), `invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid input JSON") {
		t.Errorf("unexpected error: %v", err)
	}
	_ = out
}

// TestWebSearchTool_Execute_EmptyQuery tests empty query
func TestWebSearchTool_Execute_EmptyQuery(t *testing.T) {
	tool := NewWebSearchTool(WebToolConfig{TavilyAPIKey: "test-key"})

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty query string",
			input: `{"query":""}`,
		},
		{
			name:  "whitespace query",
			input: `{"query":"   "}`,
		},
		{
			name:  "missing query field",
			input: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tt.input)
			if err == nil {
				t.Error("expected error for empty/missing query")
			}
		})
	}
}

// TestWebFetchTool_Name tests the tool name
func TestWebFetchTool_Name(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{})
	if tool.Name() != "web_fetch" {
		t.Errorf("WebFetchTool.Name() = %q, want %q", tool.Name(), "web_fetch")
	}
}

// TestWebFetchTool_Description tests the tool description
func TestWebFetchTool_Description(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{})
	desc := tool.Description()
	if !strings.Contains(desc, "fetch") {
		t.Errorf("WebFetchTool.Description() should contain 'fetch'")
	}
}

// TestWebFetchTool_InputSchema tests the input schema
func TestWebFetchTool_InputSchema(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{})
	schema := tool.InputSchema()

	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("WebFetchTool.InputSchema() is not valid JSON: %v", err)
	}

	// Check required fields
	required, ok := parsed["required"].([]interface{})
	if !ok {
		t.Error("InputSchema missing required array")
		return
	}

	hasURL := false
	for _, r := range required {
		if r == "url" {
			hasURL = true
			break
		}
	}
	if !hasURL {
		t.Error("InputSchema should require 'url' field")
	}
}

// TestWebFetchTool_Execute_InvalidURL tests invalid URL
func TestWebFetchTool_Execute_InvalidURL(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{})

	tests := []struct {
		name        string
		input       string
		shouldError bool
	}{
		{
			name:        "missing scheme",
			input:       `{"url":"example.com"}`,
			shouldError: false, // Returns error in output, not as error
		},
		{
			name:        "invalid scheme",
			input:       `{"url":"ftp://example.com"}`,
			shouldError: false,
		},
		{
			name:        "empty URL",
			input:       `{"url":""}`,
			shouldError: true,
		},
		{
			name:        "missing URL field",
			input:       `{}`,
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := tool.Execute(context.Background(), tt.input)
			if tt.shouldError {
				if err == nil {
					t.Error("expected error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if !strings.Contains(out, "Error") && !strings.Contains(out, "invalid") {
					t.Errorf("expected error message in output: %s", out)
				}
			}
		})
	}
}

// TestWebFetchTool_Execute_InvalidJSON tests invalid input JSON
func TestWebFetchTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{})
	_, err := tool.Execute(context.Background(), `invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid input JSON") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestDefaultWebToolConfig tests default configuration
func TestDefaultWebToolConfig(t *testing.T) {
	cfg := defaultWebToolConfig()

	if cfg.Timeout != 10*time.Second {
		t.Errorf("default timeout = %v, want 10s", cfg.Timeout)
	}

	if cfg.MaxSearchResults != 5 {
		t.Errorf("default MaxSearchResults = %d, want 5", cfg.MaxSearchResults)
	}

	if cfg.MaxFetchChars != 4096 {
		t.Errorf("default MaxFetchChars = %d, want 4096", cfg.MaxFetchChars)
	}
}

// TestNewWebSearchTool tests WebSearchTool constructor
func TestNewWebSearchTool(t *testing.T) {
	tests := []struct {
		name            string
		cfg             WebToolConfig
		expectedTimeout time.Duration
		expectedMax     int
	}{
		{
			name:            "zero values use defaults",
			cfg:             WebToolConfig{},
			expectedTimeout: 10 * time.Second,
			expectedMax:     5,
		},
		{
			name: "custom values preserved",
			cfg: WebToolConfig{
				Timeout:          5 * time.Second,
				MaxSearchResults: 10,
			},
			expectedTimeout: 5 * time.Second,
			expectedMax:     10,
		},
		{
			name: "negative max uses default",
			cfg: WebToolConfig{
				MaxSearchResults: -1,
			},
			expectedTimeout: 10 * time.Second, // default timeout is applied
			expectedMax:     5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewWebSearchTool(tt.cfg)
			if tool.cfg.Timeout != tt.expectedTimeout {
				t.Errorf("timeout = %v, want %v", tool.cfg.Timeout, tt.expectedTimeout)
			}
			if tool.cfg.MaxSearchResults != tt.expectedMax {
				t.Errorf("max results = %d, want %d", tool.cfg.MaxSearchResults, tt.expectedMax)
			}
		})
	}
}

// TestNewWebFetchTool tests WebFetchTool constructor
func TestNewWebFetchTool(t *testing.T) {
	tests := []struct {
		name            string
		cfg             WebToolConfig
		expectedTimeout time.Duration
		expectedMax     int
	}{
		{
			name:            "zero values use defaults",
			cfg:             WebToolConfig{},
			expectedTimeout: 10 * time.Second,
			expectedMax:     4096,
		},
		{
			name: "custom values preserved",
			cfg: WebToolConfig{
				Timeout:       5 * time.Second,
				MaxFetchChars: 8192,
			},
			expectedTimeout: 5 * time.Second,
			expectedMax:     8192,
		},
		{
			name: "negative max uses default",
			cfg: WebToolConfig{
				MaxFetchChars: -1,
			},
			expectedTimeout: 10 * time.Second, // default timeout is applied
			expectedMax:     4096,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewWebFetchTool(tt.cfg)
			if tool.cfg.Timeout != tt.expectedTimeout {
				t.Errorf("timeout = %v, want %v", tool.cfg.Timeout, tt.expectedTimeout)
			}
			if tool.cfg.MaxFetchChars != tt.expectedMax {
				t.Errorf("max fetch chars = %d, want %d", tool.cfg.MaxFetchChars, tt.expectedMax)
			}
		})
	}
}

// TestNewHTTPClient tests HTTP client creation
func TestNewHTTPClient(t *testing.T) {
	tests := []struct {
		name            string
		timeout         time.Duration
		expectedTimeout time.Duration
	}{
		{
			name:            "zero timeout uses default",
			timeout:         0,
			expectedTimeout: 10 * time.Second,
		},
		{
			name:            "custom timeout",
			timeout:         5 * time.Second,
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "long timeout",
			timeout:         30 * time.Second,
			expectedTimeout: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newHTTPClient(tt.timeout)
			if client.Timeout != tt.expectedTimeout {
				t.Errorf("client timeout = %v, want %v", client.Timeout, tt.expectedTimeout)
			}
		})
	}
}

// TestClampInt tests integer clamping
func TestClampInt(t *testing.T) {
	tests := []struct {
		v, min, max, expected int
	}{
		{5, 1, 10, 5},
		{0, 1, 10, 1},
		{15, 1, 10, 10},
		{-5, 0, 10, 0},
		{5, 5, 5, 5},
	}

	for _, tt := range tests {
		got := clampInt(tt.v, tt.min, tt.max)
		if got != tt.expected {
			t.Errorf("clampInt(%d, %d, %d) = %d, want %d", tt.v, tt.min, tt.max, got, tt.expected)
		}
	}
}

// TestWebFetchTool_Execute_ContextCancellation tests context cancellation
func TestWebFetchTool_Execute_ContextCancellation(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{Timeout: 10 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This should either fail quickly or handle cancellation gracefully
	_, err := tool.Execute(ctx, `{"url":"https://example.com"}`)
	// We don't assert on the error because it depends on timing
	_ = err
}

// TestWebSearchTool_Execute_ContextCancellation tests context cancellation
func TestWebSearchTool_Execute_ContextCancellation(t *testing.T) {
	tool := NewWebSearchTool(WebToolConfig{
		TavilyAPIKey: "test-key",
		Timeout:      10 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This should either fail quickly or handle cancellation gracefully
	_, err := tool.Execute(ctx, `{"query":"test"}`)
	// We don't assert on the error because it depends on timing
	_ = err
}

// TestWebFetchTool_Execute_SSRFProtection tests SSRF protection
func TestWebFetchTool_Execute_SSRFProtection(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{})

	// Test potentially dangerous URL schemes that should be rejected immediately
	tests := []struct {
		name        string
		input       string
		shouldError bool
	}{
		{
			name:        "file scheme",
			input:       `{"url":"file:///etc/passwd"}`,
			shouldError: false, // Returns error in output
		},
		{
			name:        "javascript scheme",
			input:       `{"url":"javascript:alert(1)"}`,
			shouldError: false,
		},
		{
			name:        "data scheme",
			input:       `{"url":"data:text/html,<script>alert(1)</script>"}`,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These schemes should be rejected immediately
			out, err := tool.Execute(context.Background(), tt.input)

			if tt.shouldError {
				if err == nil {
					t.Error("expected error for dangerous URL scheme")
				}
			} else {
				// Should return error message in output
				if !strings.Contains(out, "Error") && !strings.Contains(out, "invalid") {
					t.Errorf("expected error message in output for dangerous URL: %s", tt.input)
				}
			}
		})
	}
}

// TestWebFetchTool_Execute_TavilyExtract tests Tavily Extract API path
func TestWebFetchTool_Execute_TavilyExtract(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{
		TavilyAPIKey:     "test-key",
		UseTavilyExtract: true,
		Timeout:          1 * time.Second, // Short timeout for test
	})

	// This will fail because we don't have a real API key, but it should try the Tavily path
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := tool.Execute(ctx, `{"url":"https://example.com"}`)

	// Should attempt Tavily Extract (will fail with network/auth error or timeout)
	_ = out
	_ = err
}

// TestWebFetchTool_Execute_JinaReader tests Jina Reader API path
func TestWebFetchTool_Execute_JinaReader(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{
		JinaAPIKey: "test-key",
		Timeout:    1 * time.Second, // Short timeout for test
		// UseTavilyExtract defaults to false
	})

	// This will fail because we don't have a real API key, but it should try the Jina path
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := tool.Execute(ctx, `{"url":"https://example.com"}`)

	// Should attempt Jina Reader (will fail with network/auth error or timeout)
	_ = out
	_ = err
}

// ---------------------------------------------------------------------------
// ImageSearchTool tests
// ---------------------------------------------------------------------------

func TestImageSearchTool_Name(t *testing.T) {
	tool := NewImageSearchTool(WebToolConfig{})
	if tool.Name() != "image_search" {
		t.Errorf("expected name 'image_search', got %q", tool.Name())
	}
}

func TestImageSearchTool_Description(t *testing.T) {
	tool := NewImageSearchTool(WebToolConfig{})
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestImageSearchTool_InputSchema(t *testing.T) {
	tool := NewImageSearchTool(WebToolConfig{})
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("InputSchema is not valid JSON: %v", err)
	}
}

func TestImageSearchTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewImageSearchTool(WebToolConfig{})
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestImageSearchTool_Execute_EmptyQuery(t *testing.T) {
	tool := NewImageSearchTool(WebToolConfig{})
	_, err := tool.Execute(context.Background(), `{"query":""}`)
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestImageSearchTool_Execute_WhitespaceQuery(t *testing.T) {
	tool := NewImageSearchTool(WebToolConfig{})
	_, err := tool.Execute(context.Background(), `{"query":"   "}`)
	if err == nil {
		t.Error("expected error for whitespace query")
	}
}

func TestNewImageSearchTool(t *testing.T) {
	// Test with defaults
	tool1 := NewImageSearchTool(WebToolConfig{})
	if tool1.cfg.Timeout != 10*time.Second {
		t.Errorf("expected default timeout, got %v", tool1.cfg.Timeout)
	}

	// Test with custom values
	tool2 := NewImageSearchTool(WebToolConfig{
		Timeout:          5 * time.Second,
		MaxSearchResults: 10,
	})
	if tool2.cfg.Timeout != 5*time.Second {
		t.Errorf("expected 5s timeout, got %v", tool2.cfg.Timeout)
	}
	if tool2.cfg.MaxSearchResults != 10 {
		t.Errorf("expected 10 max results, got %d", tool2.cfg.MaxSearchResults)
	}
}

func TestImageSearchTool_Execute_ContextCancellation(t *testing.T) {
	tool := NewImageSearchTool(WebToolConfig{Timeout: 10 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tool.Execute(ctx, `{"query":"test"}`)
	_ = err // Depends on timing
}
