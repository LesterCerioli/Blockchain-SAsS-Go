package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
	"github.com/google/uuid"
)

type ErrorBody struct {
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type SuccessResponse struct {
	Data      interface{} `json:"data"`
	RequestID string      `json:"request_id,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

type ListResponse struct {
	Data       interface{} `json:"data"`
	Pagination *Pagination `json:"pagination,omitempty"`
	RequestID  string      `json:"request_id,omitempty"`
	Timestamp  time.Time   `json:"timestamp"`
}

type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

func WriteJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func WriteSuccess(w http.ResponseWriter, r *http.Request, status int, data interface{}) {
	WriteJSON(w, status, SuccessResponse{
		Data:      data,
		RequestID: RequestID(r.Context()),
		Timestamp: time.Now().UTC(),
	})
}

func WriteList(w http.ResponseWriter, r *http.Request, data interface{}, pagination *Pagination) {
	WriteJSON(w, http.StatusOK, ListResponse{
		Data:       data,
		Pagination: pagination,
		RequestID:  RequestID(r.Context()),
		Timestamp:  time.Now().UTC(),
	})
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	appErr, ok := apperrors.As(err)
	if !ok {
		appErr = apperrors.Internal("internal server error", err)
	}
	if appErr.HTTPStatus >= http.StatusInternalServerError {
		slog.Error("request failed", "error", err, "code", appErr.Code,
			"request_id", RequestID(r.Context()), "path", r.URL.Path)
	}
	WriteJSON(w, appErr.HTTPStatus, ErrorResponse{Error: ErrorBody{
		Code:      appErr.Code,
		Message:   appErr.Message,
		Details:   appErr.Details,
		RequestID: RequestID(r.Context()),
		Timestamp: time.Now().UTC(),
	}})
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

func NewRequestID() string {
	return uuid.NewString()
}
