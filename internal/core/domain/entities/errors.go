package entities

import "errors"

var (
	ErrNotFound       = errors.New("resource not found")
	ErrConflict       = errors.New("resource already exists")
	ErrInvalidState   = errors.New("invalid state transition")
	ErrExpired        = errors.New("resource expired")
	ErrUnsupported    = errors.New("unsupported operation")
	ErrUnauthorized   = errors.New("unauthorized")
	ErrForbidden      = errors.New("forbidden")
	ErrInvalidAmount  = errors.New("invalid amount")
	ErrInvalidAddress = errors.New("invalid address")
	ErrBlocked        = errors.New("operation blocked by risk engine")
	ErrRateLimited    = errors.New("rate limit exceeded")
)
