package errors

import (
	"errors"
	"fmt"
	"net/http"
)

type AppError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Details    map[string]interface{} `json:"details,omitempty"`
	HTTPStatus int                    `json:"-"`
	Err        error                  `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(code, message string, status int) *AppError {
	return &AppError{Code: code, Message: message, HTTPStatus: status}
}

func WithDetails(err *AppError, details map[string]interface{}) *AppError {
	err.Details = details
	return err
}

func WithCause(err *AppError, cause error) *AppError {
	err.Err = cause
	return err
}

func BadRequest(message string) *AppError {
	return New("bad_request", message, http.StatusBadRequest)
}

func Validation(message string, details map[string]interface{}) *AppError {
	e := New("validation_error", message, http.StatusUnprocessableEntity)
	e.Details = details
	return e
}

func Unauthorized(message string) *AppError {
	return New("unauthorized", message, http.StatusUnauthorized)
}

func Forbidden(message string) *AppError {
	return New("forbidden", message, http.StatusForbidden)
}

func NotFound(message string) *AppError {
	return New("not_found", message, http.StatusNotFound)
}

func Conflict(message string) *AppError {
	return New("conflict", message, http.StatusConflict)
}

func Unprocessable(message string) *AppError {
	return New("unprocessable_entity", message, http.StatusUnprocessableEntity)
}

func TooManyRequests(message string) *AppError {
	return New("rate_limited", message, http.StatusTooManyRequests)
}

func Unsupported(message string) *AppError {
	return New("unsupported", message, http.StatusNotImplemented)
}

func Internal(message string, cause error) *AppError {
	e := New("internal_error", message, http.StatusInternalServerError)
	e.Err = cause
	return e
}

func ServiceUnavailable(message string, cause error) *AppError {
	e := New("service_unavailable", message, http.StatusServiceUnavailable)
	e.Err = cause
	return e
}

func Is(err error, target *AppError) bool {
	var appErr *AppError
	if !errors.As(err, &appErr) {
		return false
	}
	return appErr.Code == target.Code
}

func As(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}
