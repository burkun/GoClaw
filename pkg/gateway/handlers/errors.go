// Package handlers provides HTTP route handlers for GoClaw gateway.
package handlers

import (
	"github.com/gin-gonic/gin"
)

// APIError is the unified error response structure.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// Error codes aligned with deer-flow.
const (
	ErrCodeValidation   = "validation_error"
	ErrCodeNotFound     = "not_found"
	ErrCodeInternal     = "internal_error"
	ErrCodeConflict     = "conflict"
	ErrCodeUnauthorized = "unauthorized"
	ErrCodeServiceDown  = "service_unavailable"
)

// NewValidationError creates a validation error.
func NewValidationError(msg string) APIError {
	return APIError{Code: ErrCodeValidation, Message: msg}
}

// NewNotFoundError creates a not-found error.
func NewNotFoundError(msg string) APIError {
	return APIError{Code: ErrCodeNotFound, Message: msg}
}

// NewInternalError creates an internal error (use when underlying error should not be exposed).
func NewInternalError(msg string) APIError {
	return APIError{Code: ErrCodeInternal, Message: msg}
}

// NewConflictError creates a conflict error.
func NewConflictError(msg string) APIError {
	return APIError{Code: ErrCodeConflict, Message: msg}
}

// NewServiceUnavailableError creates a service-unavailable error.
func NewServiceUnavailableError(msg string) APIError {
	return APIError{Code: ErrCodeServiceDown, Message: msg}
}

// Render writes the error to the gin context in a unified JSON structure.
func (e APIError) Render(c *gin.Context, status int) {
	c.JSON(status, gin.H{"error": e})
}
