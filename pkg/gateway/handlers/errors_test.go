package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNewValidationError(t *testing.T) {
	err := NewValidationError("test validation error")
	if err.Code != ErrCodeValidation {
		t.Errorf("expected code %s, got %s", ErrCodeValidation, err.Code)
	}
	if err.Message != "test validation error" {
		t.Errorf("expected message 'test validation error', got %s", err.Message)
	}
}

func TestNewNotFoundError(t *testing.T) {
	err := NewNotFoundError("test not found")
	if err.Code != ErrCodeNotFound {
		t.Errorf("expected code %s, got %s", ErrCodeNotFound, err.Code)
	}
}

func TestNewAPIError(t *testing.T) {
	err := NewAPIError("custom_code", "custom message")
	if err.Code != "custom_code" {
		t.Errorf("expected code custom_code, got %s", err.Code)
	}
	if err.Message != "custom message" {
		t.Errorf("expected message 'custom message', got %s", err.Message)
	}
}

func TestNewInternalError(t *testing.T) {
	err := NewInternalError("internal error message")
	if err.Code != ErrCodeInternal {
		t.Errorf("expected code %s, got %s", ErrCodeInternal, err.Code)
	}
}

func TestNewConflictError(t *testing.T) {
	err := NewConflictError("conflict occurred")
	if err.Code != ErrCodeConflict {
		t.Errorf("expected code %s, got %s", ErrCodeConflict, err.Code)
	}
}

func TestNewServiceUnavailableError(t *testing.T) {
	err := NewServiceUnavailableError("service down")
	if err.Code != ErrCodeServiceDown {
		t.Errorf("expected code %s, got %s", ErrCodeServiceDown, err.Code)
	}
}

func TestAPIError_Render(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	err := NewValidationError("test error")
	err.Render(c, http.StatusBadRequest)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	expected := `{"error":{"code":"validation_error","message":"test error"}}`
	if w.Body.String() != expected {
		t.Errorf("expected body %s, got %s", expected, w.Body.String())
	}
}
