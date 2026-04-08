package errors

import (
	"errors"
	"fmt"
	"testing"
)

// TestCode_String tests the String method of Code.
func TestCode_String(t *testing.T) {
	tests := []struct {
		code     Code
		expected string
	}{
		{CodeInvalidConfig, "INVALID_CONFIG"},
		{CodeInternalError, "INTERNAL_ERROR"},
		{CodeNotFound, "NOT_FOUND"},
		{CodePermission, "PERMISSION_DENIED"},
		{CodeTimeout, "TIMEOUT"},
		{CodeValidation, "VALIDATION_ERROR"},
		{CodeCanceled, "CANCELED"},
		{CodeUnavailable, "UNAVAILABLE"},
		{CodeAlreadyExists, "ALREADY_EXISTS"},
		{CodeConflict, "CONFLICT"},
		{CodeTooManyRequests, "TOO_MANY_REQUESTS"},
		{Code(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.code.String(); got != tt.expected {
				t.Errorf("Code.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestCode_Category tests the Category method of Code.
func TestCode_Category(t *testing.T) {
	tests := []struct {
		code     Code
		expected string
	}{
		{CodeInvalidConfig, "client"},
		{CodeValidation, "client"},
		{CodeNotFound, "resource"},
		{CodeAlreadyExists, "resource"},
		{CodeConflict, "resource"},
		{CodePermission, "auth"},
		{CodeTimeout, "system"},
		{CodeCanceled, "system"},
		{CodeUnavailable, "system"},
		{CodeTooManyRequests, "system"},
		{CodeInternalError, "server"},
		{Code(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.code.String(), tt.expected), func(t *testing.T) {
			if got := tt.code.Category(); got != tt.expected {
				t.Errorf("Code.Category() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestError_Error tests the Error method.
func TestError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		contains string
	}{
		{
			name:     "without_cause",
			err:      New(CodeNotFound, "resource not found"),
			contains: "[NOT_FOUND] resource not found",
		},
		{
			name:     "with_cause",
			err:      Wrap(CodeInternalError, "internal error", fmt.Errorf("underlying error")),
			contains: "underlying error",
		},
		{
			name:     "formatted_message",
			err:      Newf(CodeValidation, "field %s is required", "name"),
			contains: "field name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if !contains(got, tt.contains) {
				t.Errorf("Error() = %v, want to contain %v", got, tt.contains)
			}
		})
	}
}

// TestError_Unwrap tests the Unwrap method.
func TestError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := Wrap(CodeInternalError, "wrapped", cause)

	if got := err.Unwrap(); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}

	// Test nil cause
	err2 := New(CodeNotFound, "not found")
	if got := err2.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
}

// TestError_Is tests the Is method for error comparison.
func TestError_Is(t *testing.T) {
	err1 := New(CodeNotFound, "not found 1")
	err2 := New(CodeNotFound, "not found 2")
	err3 := New(CodeInternalError, "internal error")

	// Same code should match
	if !errors.Is(err1, err2) {
		t.Error("errors with same code should match")
	}

	// Different code should not match
	if errors.Is(err1, err3) {
		t.Error("errors with different code should not match")
	}

	// Should not match different type
	if errors.Is(err1, fmt.Errorf("standard error")) {
		t.Error("should not match standard error")
	}
}

// TestError_As tests the As method for type assertion.
func TestError_As(t *testing.T) {
	err := New(CodeNotFound, "resource not found")

	// Test As for *Error
	var e *Error
	if !errors.As(err, &e) {
		t.Fatal("errors.As failed for *Error")
	}
	if e.Code != CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", e.Code)
	}

	// Test As method directly for *Code
	var c Code
	if !err.As(&c) {
		t.Fatal("As failed for *Code")
	}
	if c != CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", c)
	}
}

// TestError_Chain tests error chain functionality.
func TestError_Chain(t *testing.T) {
	// Create a chain of errors
	level1 := fmt.Errorf("level 1 error")
	level2 := Wrap(CodeInternalError, "level 2", level1)
	level3 := Wrap(CodeTimeout, "level 3", level2)

	// Verify chain with errors.Is
	var err *Error
	if !errors.As(level3, &err) {
		t.Fatal("expected to extract error from chain")
	}

	if err.Code != CodeTimeout {
		t.Errorf("expected CodeTimeout, got %v", err.Code)
	}

	// Unwrap to get level 2
	level2Err := err.Unwrap()
	if level2Err == nil {
		t.Fatal("expected level 2 error")
	}

	var e2 *Error
	if !errors.As(level2Err, &e2) {
		t.Fatal("expected to extract level 2 error")
	}
	if e2.Code != CodeInternalError {
		t.Errorf("expected CodeInternalError, got %v", e2.Code)
	}
}

// TestNew tests the New constructor.
func TestNew(t *testing.T) {
	err := New(CodeNotFound, "resource not found")

	if err.Code != CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err.Code)
	}
	if err.Message != "resource not found" {
		t.Errorf("expected 'resource not found', got %v", err.Message)
	}
	if err.Cause != nil {
		t.Error("expected nil cause")
	}
}

// TestNewf tests the Newf constructor with formatting.
func TestNewf(t *testing.T) {
	err := Newf(CodeValidation, "field %s is invalid: %v", "name", "empty")

	if err.Code != CodeValidation {
		t.Errorf("expected CodeValidation, got %v", err.Code)
	}
	if err.Message != "field name is invalid: empty" {
		t.Errorf("unexpected message: %v", err.Message)
	}
}

// TestWrap tests the Wrap constructor.
func TestWrap(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := Wrap(CodeInternalError, "wrapped error", cause)

	if err.Code != CodeInternalError {
		t.Errorf("expected CodeInternalError, got %v", err.Code)
	}
	if err.Message != "wrapped error" {
		t.Errorf("expected 'wrapped error', got %v", err.Message)
	}
	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

// TestWrapf tests the Wrapf constructor with formatting.
func TestWrapf(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := Wrapf(CodeTimeout, cause, "operation %s timed out", "query")

	if err.Code != CodeTimeout {
		t.Errorf("expected CodeTimeout, got %v", err.Code)
	}
	if err.Message != "operation query timed out" {
		t.Errorf("unexpected message: %v", err.Message)
	}
	if err.Cause != cause {
		t.Error("expected cause to be set")
	}
}

// TestGetCode tests the GetCode function.
func TestGetCode(t *testing.T) {
	err := New(CodeNotFound, "not found")
	if code := GetCode(err); code != CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", code)
	}

	// Standard error should return CodeInternalError
	stdErr := fmt.Errorf("standard error")
	if code := GetCode(stdErr); code != CodeInternalError {
		t.Errorf("expected CodeInternalError for standard error, got %v", code)
	}
}

// TestIsCode tests the IsCode function.
func TestIsCode(t *testing.T) {
	err := New(CodeNotFound, "not found")

	if !IsCode(err, CodeNotFound) {
		t.Error("expected IsCode to return true")
	}

	if IsCode(err, CodeInternalError) {
		t.Error("expected IsCode to return false for different code")
	}

	// Standard error should return false
	stdErr := fmt.Errorf("standard error")
	if IsCode(stdErr, CodeNotFound) {
		t.Error("expected IsCode to return false for standard error")
	}
}

// TestConvenienceFunctions tests all convenience functions.
func TestConvenienceFunctions(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		code     Code
		contains string
	}{
		{"ConfigError", ConfigError("bad config"), CodeInvalidConfig, "bad config"},
		{"ConfigErrorf", ConfigErrorf("config %s invalid", "db"), CodeInvalidConfig, "config db invalid"},
		{"InternalError", InternalError("internal failure"), CodeInternalError, "internal failure"},
		{"InternalErrorf", InternalErrorf("failed: %s", "reason"), CodeInternalError, "failed: reason"},
		{"NotFoundError", NotFoundError("user"), CodeNotFound, "user not found"},
		{"PermissionError", PermissionError("access denied"), CodePermission, "access denied"},
		{"TimeoutError", TimeoutError("operation timeout"), CodeTimeout, "operation timeout"},
		{"ValidationError", ValidationError("invalid input"), CodeValidation, "invalid input"},
		{"ValidationErrorf", ValidationErrorf("field %s required", "name"), CodeValidation, "field name required"},
		{"CanceledError", CanceledError("operation canceled"), CodeCanceled, "operation canceled"},
		{"UnavailableError", UnavailableError("service down"), CodeUnavailable, "service down"},
		{"AlreadyExistsError", AlreadyExistsError("user"), CodeAlreadyExists, "user already exists"},
		{"ConflictError", ConflictError("version mismatch"), CodeConflict, "version mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("expected code %v, got %v", tt.code, tt.err.Code)
			}
			if !contains(tt.err.Message, tt.contains) {
				t.Errorf("message should contain %v, got %v", tt.contains, tt.err.Message)
			}
		})
	}
}

// TestWrapConvenienceFunctions tests all wrap convenience functions.
func TestWrapConvenienceFunctions(t *testing.T) {
	cause := fmt.Errorf("underlying error")

	tests := []struct {
		name     string
		err      *Error
		code     Code
		contains string
	}{
		{"WrapConfigError", WrapConfigError(cause, "config error"), CodeInvalidConfig, "config error"},
		{"WrapInternalError", WrapInternalError(cause, "internal error"), CodeInternalError, "internal error"},
		{"WrapNotFoundError", WrapNotFoundError(cause, "user"), CodeNotFound, "user not found"},
		{"WrapPermissionError", WrapPermissionError(cause, "denied"), CodePermission, "denied"},
		{"WrapTimeoutError", WrapTimeoutError(cause, "timeout"), CodeTimeout, "timeout"},
		{"WrapValidationError", WrapValidationError(cause, "validation failed"), CodeValidation, "validation failed"},
		{"WrapCanceledError", WrapCanceledError(cause, "canceled"), CodeCanceled, "canceled"},
		{"WrapUnavailableError", WrapUnavailableError(cause, "unavailable"), CodeUnavailable, "unavailable"},
		{"WrapConflictError", WrapConflictError(cause, "conflict"), CodeConflict, "conflict"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("expected code %v, got %v", tt.code, tt.err.Code)
			}
			if !contains(tt.err.Message, tt.contains) {
				t.Errorf("message should contain %v, got %v", tt.contains, tt.err.Message)
			}
			if tt.err.Cause != cause {
				t.Error("expected cause to be set")
			}
		})
	}
}

// TestToolWrappers tests tool-specific error wrappers.
func TestToolWrappers(t *testing.T) {
	cause := fmt.Errorf("tool failed")

	tests := []struct {
		name     string
		err      *Error
		code     Code
		contains string
	}{
		{"WrapToolError", WrapToolError(cause, "search"), CodeInternalError, "tool search execution failed"},
		{"WrapToolTimeout", WrapToolTimeout(cause, "query"), CodeTimeout, "tool query execution timed out"},
		{"WrapToolNotFoundError", WrapToolNotFoundError("missing"), CodeNotFound, "tool missing not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("expected code %v, got %v", tt.code, tt.err.Code)
			}
			if !contains(tt.err.Message, tt.contains) {
				t.Errorf("message should contain %v, got %v", tt.contains, tt.err.Message)
			}
		})
	}
}

// TestAgentWrappers tests agent-specific error wrappers.
func TestAgentWrappers(t *testing.T) {
	cause := fmt.Errorf("agent failed")

	tests := []struct {
		name     string
		err      *Error
		code     Code
		contains string
	}{
		{"WrapAgentError", WrapAgentError(cause, "planner"), CodeInternalError, "agent planner execution failed"},
		{"WrapAgentTimeout", WrapAgentTimeout(cause, "executor"), CodeTimeout, "agent executor execution timed out"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("expected code %v, got %v", tt.code, tt.err.Code)
			}
			if !contains(tt.err.Message, tt.contains) {
				t.Errorf("message should contain %v, got %v", tt.contains, tt.err.Message)
			}
		})
	}
}

// TestLLMWrappers tests LLM-specific error wrappers.
func TestLLMWrappers(t *testing.T) {
	cause := fmt.Errorf("llm failed")

	tests := []struct {
		name     string
		err      *Error
		code     Code
		contains string
	}{
		{"WrapLLMError", WrapLLMError(cause, "openai"), CodeInternalError, "LLM provider openai error"},
		{"WrapLLMTimeout", WrapLLMTimeout(cause, "anthropic"), CodeTimeout, "LLM provider anthropic timeout"},
		{"WrapLLMRateLimit", WrapLLMRateLimit(cause, "google"), CodeTooManyRequests, "LLM provider google rate limit exceeded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Errorf("expected code %v, got %v", tt.code, tt.err.Code)
			}
			if !contains(tt.err.Message, tt.contains) {
				t.Errorf("message should contain %v, got %v", tt.contains, tt.err.Message)
			}
		})
	}
}

// TestNilErrorHandling tests handling of nil errors.
func TestNilErrorHandling(t *testing.T) {
	// GetCode with nil
	if code := GetCode(nil); code != CodeInternalError {
		t.Errorf("expected CodeInternalError for nil, got %v", code)
	}

	// IsCode with nil
	if IsCode(nil, CodeNotFound) {
		t.Error("expected IsCode to return false for nil")
	}
}

// TestDeepNesting tests deeply nested errors.
func TestDeepNesting(t *testing.T) {
	// Create a chain of 5 errors
	var err error = fmt.Errorf("base error")
	for i := 0; i < 5; i++ {
		err = Wrap(CodeInternalError, fmt.Sprintf("level %d", i+1), err)
	}

	// Should be able to extract the deepest error
	var e *Error
	if !errors.As(err, &e) {
		t.Fatal("expected to extract error from chain")
	}

	// Unwrap through the chain
	depth := 0
	current := err
	for current != nil {
		depth++
		if unwrapper, ok := current.(interface{ Unwrap() error }); ok {
			current = unwrapper.Unwrap()
		} else {
			break
		}
	}

	if depth < 5 {
		t.Errorf("expected at least 5 levels of nesting, got %d", depth)
	}
}

// TestAllErrorCodesCovered ensures all error codes are tested.
func TestAllErrorCodesCovered(t *testing.T) {
	codes := []Code{
		CodeInvalidConfig,
		CodeInternalError,
		CodeNotFound,
		CodePermission,
		CodeTimeout,
		CodeValidation,
		CodeCanceled,
		CodeUnavailable,
		CodeAlreadyExists,
		CodeConflict,
		CodeTooManyRequests,
	}

	for _, code := range codes {
		t.Run(code.String(), func(t *testing.T) {
			// Ensure String() doesn't return "UNKNOWN"
			if code.String() == "UNKNOWN" {
				t.Errorf("code %d should have a proper string representation", code)
			}

			// Ensure Category() doesn't return "unknown"
			if code.Category() == "unknown" {
				t.Errorf("code %d should have a proper category", code)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
