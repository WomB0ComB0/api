package apperr

import (
	stderrors "errors"
	"fmt"
	"net/http"
)

type ErrorType string

const (
	TypeValidation   ErrorType = "VALIDATION"
	TypeNotFound     ErrorType = "NOT_FOUND"
	TypeUnauthorized ErrorType = "UNAUTHORIZED"
	TypeForbidden    ErrorType = "FORBIDDEN"
	TypeConflict     ErrorType = "CONFLICT"
	TypeTooLarge     ErrorType = "TOO_LARGE"
	TypeUnavailable  ErrorType = "UNAVAILABLE"
	TypeExternal     ErrorType = "EXTERNAL"
	TypeSystem       ErrorType = "SYSTEM"
)

type AppError struct {
	Type    ErrorType
	Message string
	Cause   error
	Details map[string]any
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Cause
}

func (e *AppError) WithDetails(details map[string]any) *AppError {
	e.Details = details
	return e
}

func (e *AppError) HTTPStatus() int {
	switch e.Type {
	case TypeValidation:
		return http.StatusBadRequest
	case TypeUnauthorized:
		return http.StatusUnauthorized
	case TypeForbidden:
		return http.StatusForbidden
	case TypeNotFound:
		return http.StatusNotFound
	case TypeConflict:
		return http.StatusConflict
	case TypeTooLarge:
		return http.StatusRequestEntityTooLarge
	case TypeUnavailable:
		return http.StatusServiceUnavailable
	case TypeExternal:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func New(errType ErrorType, message string) *AppError {
	return &AppError{Type: errType, Message: message}
}

func Wrap(err error, errType ErrorType, message string) *AppError {
	return &AppError{Type: errType, Message: message, Cause: err}
}

func Validation(message string) *AppError   { return New(TypeValidation, message) }
func NotFound(message string) *AppError     { return New(TypeNotFound, message) }
func Unauthorized(message string) *AppError { return New(TypeUnauthorized, message) }
func Forbidden(message string) *AppError    { return New(TypeForbidden, message) }
func TooLarge(message string) *AppError     { return New(TypeTooLarge, message) }
func Unavailable(message string) *AppError  { return New(TypeUnavailable, message) }
func External(message string, cause error) *AppError {
	return Wrap(cause, TypeExternal, message)
}
func System(message string, cause error) *AppError {
	return Wrap(cause, TypeSystem, message)
}

func As(err error) (*AppError, bool) {
	var appErr *AppError
	if stderrors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

func IsType(err error, errType ErrorType) bool {
	if appErr, ok := As(err); ok {
		return appErr.Type == errType
	}
	return false
}
