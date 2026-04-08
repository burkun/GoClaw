// Package errors provides a unified error handling framework for GoClaw.
package errors

import (
	"errors"
	"fmt"
)

// Code represents an error code in the system.
type Code int

// Error codes for the application.
const (
	CodeInvalidConfig Code = iota + 1
	CodeInternalError
	CodeNotFound
	CodePermission
	CodeTimeout
	CodeValidation
	CodeCanceled
	CodeUnavailable
	CodeAlreadyExists
	CodeConflict
	CodeTooManyRequests
)

// String returns the string representation of the error code.
func (c Code) String() string {
	switch c {
	case CodeInvalidConfig:
		return "INVALID_CONFIG"
	case CodeInternalError:
		return "INTERNAL_ERROR"
	case CodeNotFound:
		return "NOT_FOUND"
	case CodePermission:
		return "PERMISSION_DENIED"
	case CodeTimeout:
		return "TIMEOUT"
	case CodeValidation:
		return "VALIDATION_ERROR"
	case CodeCanceled:
		return "CANCELED"
	case CodeUnavailable:
		return "UNAVAILABLE"
	case CodeAlreadyExists:
		return "ALREADY_EXISTS"
	case CodeConflict:
		return "CONFLICT"
	case CodeTooManyRequests:
		return "TOO_MANY_REQUESTS"
	default:
		return "UNKNOWN"
	}
}

// Category returns the category of the error code.
func (c Code) Category() string {
	switch c {
	case CodeInvalidConfig, CodeValidation:
		return "client"
	case CodeNotFound, CodeAlreadyExists, CodeConflict:
		return "resource"
	case CodePermission:
		return "auth"
	case CodeTimeout, CodeCanceled, CodeUnavailable, CodeTooManyRequests:
		return "system"
	case CodeInternalError:
		return "server"
	default:
		return "unknown"
	}
}

// Error represents a structured error with code, message, and cause.
type Error struct {
	Code    Code   // Error code
	Message string // Human-readable message
	Cause   error  // Underlying error that caused this error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code.String(), e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code.String(), e.Message)
}

// Unwrap returns the underlying cause of the error.
func (e *Error) Unwrap() error {
	return e.Cause
}

// Is implements errors.Is interface for error comparison.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// As implements errors.As interface for error type assertion.
func (e *Error) As(target interface{}) bool {
	switch t := target.(type) {
	case **Error:
		*t = e
		return true
	case *Code:
		*t = e.Code
		return true
	default:
		return false
	}
}

// New creates a new error with the given code and message.
func New(code Code, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// Newf creates a new error with formatted message.
func Newf(code Code, format string, args ...interface{}) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Wrap creates a new error wrapping an existing error.
func Wrap(code Code, message string, cause error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// Wrapf creates a new error with formatted message wrapping an existing error.
func Wrapf(code Code, cause error, format string, args ...interface{}) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Cause:   cause,
	}
}

// GetCode extracts the error code from an error.
func GetCode(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternalError
}

// IsCode checks if the error has the specified code.
func IsCode(err error, code Code) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}

// Convenience functions for common error types

// ConfigError creates an invalid configuration error.
func ConfigError(message string) *Error {
	return New(CodeInvalidConfig, message)
}

// ConfigErrorf creates an invalid configuration error with formatted message.
func ConfigErrorf(format string, args ...interface{}) *Error {
	return Newf(CodeInvalidConfig, format, args...)
}

// WrapConfigError wraps an error as a configuration error.
func WrapConfigError(err error, message string) *Error {
	return Wrap(CodeInvalidConfig, message, err)
}

// InternalError creates an internal server error.
func InternalError(message string) *Error {
	return New(CodeInternalError, message)
}

// InternalErrorf creates an internal server error with formatted message.
func InternalErrorf(format string, args ...interface{}) *Error {
	return Newf(CodeInternalError, format, args...)
}

// WrapInternalError wraps an error as an internal error.
func WrapInternalError(err error, message string) *Error {
	return Wrap(CodeInternalError, message, err)
}

// NotFoundError creates a not found error.
func NotFoundError(resource string) *Error {
	return Newf(CodeNotFound, "%s not found", resource)
}

// WrapNotFoundError wraps an error as a not found error.
func WrapNotFoundError(err error, resource string) *Error {
	return Wrapf(CodeNotFound, err, "%s not found", resource)
}

// PermissionError creates a permission denied error.
func PermissionError(message string) *Error {
	return New(CodePermission, message)
}

// WrapPermissionError wraps an error as a permission error.
func WrapPermissionError(err error, message string) *Error {
	return Wrap(CodePermission, message, err)
}

// TimeoutError creates a timeout error.
func TimeoutError(message string) *Error {
	return New(CodeTimeout, message)
}

// WrapTimeoutError wraps an error as a timeout error.
func WrapTimeoutError(err error, message string) *Error {
	return Wrap(CodeTimeout, message, err)
}

// ValidationError creates a validation error.
func ValidationError(message string) *Error {
	return New(CodeValidation, message)
}

// ValidationErrorf creates a validation error with formatted message.
func ValidationErrorf(format string, args ...interface{}) *Error {
	return Newf(CodeValidation, format, args...)
}

// WrapValidationError wraps an error as a validation error.
func WrapValidationError(err error, message string) *Error {
	return Wrap(CodeValidation, message, err)
}

// CanceledError creates a canceled error.
func CanceledError(message string) *Error {
	return New(CodeCanceled, message)
}

// WrapCanceledError wraps an error as a canceled error.
func WrapCanceledError(err error, message string) *Error {
	return Wrap(CodeCanceled, message, err)
}

// UnavailableError creates an unavailable error.
func UnavailableError(message string) *Error {
	return New(CodeUnavailable, message)
}

// WrapUnavailableError wraps an error as an unavailable error.
func WrapUnavailableError(err error, message string) *Error {
	return Wrap(CodeUnavailable, message, err)
}

// AlreadyExistsError creates an already exists error.
func AlreadyExistsError(resource string) *Error {
	return Newf(CodeAlreadyExists, "%s already exists", resource)
}

// ConflictError creates a conflict error.
func ConflictError(message string) *Error {
	return New(CodeConflict, message)
}

// WrapConflictError wraps an error as a conflict error.
func WrapConflictError(err error, message string) *Error {
	return Wrap(CodeConflict, message, err)
}

// Tool-specific error wrappers

// WrapToolError wraps an error with tool context.
func WrapToolError(err error, toolName string) *Error {
	return Wrapf(CodeInternalError, err, "tool %s execution failed", toolName)
}

// WrapToolTimeout wraps a timeout error for a specific tool.
func WrapToolTimeout(err error, toolName string) *Error {
	return Wrapf(CodeTimeout, err, "tool %s execution timed out", toolName)
}

// WrapToolNotFoundError wraps a not found error for a specific tool.
func WrapToolNotFoundError(toolName string) *Error {
	return Newf(CodeNotFound, "tool %s not found", toolName)
}

// Agent-specific error wrappers

// WrapAgentError wraps an error with agent context.
func WrapAgentError(err error, agentName string) *Error {
	return Wrapf(CodeInternalError, err, "agent %s execution failed", agentName)
}

// WrapAgentTimeout wraps a timeout error for a specific agent.
func WrapAgentTimeout(err error, agentName string) *Error {
	return Wrapf(CodeTimeout, err, "agent %s execution timed out", agentName)
}

// LLM-specific error wrappers

// WrapLLMError wraps an error with LLM context.
func WrapLLMError(err error, provider string) *Error {
	return Wrapf(CodeInternalError, err, "LLM provider %s error", provider)
}

// WrapLLMTimeout wraps a timeout error for LLM operations.
func WrapLLMTimeout(err error, provider string) *Error {
	return Wrapf(CodeTimeout, err, "LLM provider %s timeout", provider)
}

// WrapLLMRateLimit wraps a rate limit error for LLM operations.
func WrapLLMRateLimit(err error, provider string) *Error {
	return Wrapf(CodeTooManyRequests, err, "LLM provider %s rate limit exceeded", provider)
}
