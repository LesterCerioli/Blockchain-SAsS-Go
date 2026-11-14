package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
	"github.com/google/uuid"
)

func TestRequestID(t *testing.T) {
	if RequestID(nil) != "" {
		t.Fatal("nil context must return empty request id")
	}
	ctx := context.Background()
	if RequestID(ctx) != "" {
		t.Fatal("missing request id must be empty")
	}
	ctx = WithRequestID(ctx, "req-1")
	if RequestID(ctx) != "req-1" {
		t.Fatalf("request id = %q", RequestID(ctx))
	}
	if _, err := uuid.Parse(NewRequestID()); err != nil {
		t.Fatalf("invalid generated id: %v", err)
	}
}

func TestWriteJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteJSON(recorder, http.StatusCreated, map[string]string{"ok": "yes"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d", recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", recorder.Header().Get("Content-Type"))
	}
	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != "yes" {
		t.Fatalf("payload = %v", payload)
	}

	empty := httptest.NewRecorder()
	WriteJSON(empty, http.StatusNoContent, nil)
	if empty.Body.Len() != 0 {
		t.Fatalf("body = %q", empty.Body.String())
	}
}

func TestWriteSuccessAndList(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	request = request.WithContext(WithRequestID(request.Context(), "req-2"))

	recorder := httptest.NewRecorder()
	WriteSuccess(recorder, request, http.StatusCreated, map[string]string{"id": "1"})
	var success SuccessResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &success); err != nil {
		t.Fatal(err)
	}
	if success.RequestID != "req-2" || success.Timestamp.IsZero() {
		t.Fatalf("success = %+v", success)
	}

	listRecorder := httptest.NewRecorder()
	WriteList(listRecorder, request, []string{"a"}, &Pagination{Page: 1, PageSize: 10, Total: 1})
	var list ListResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Pagination == nil || list.Pagination.Total != 1 {
		t.Fatalf("list = %+v", list)
	}
}

func TestWriteError(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/missing", nil)
	request = request.WithContext(WithRequestID(request.Context(), "req-3"))

	recorder := httptest.NewRecorder()
	WriteError(recorder, request, apperrors.NotFound("gone"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d", recorder.Code)
	}
	var appErr ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &appErr); err != nil {
		t.Fatal(err)
	}
	if appErr.Error.Code != "not_found" || appErr.Error.RequestID != "req-3" {
		t.Fatalf("error = %+v", appErr.Error)
	}

	recorder = httptest.NewRecorder()
	WriteError(recorder, request, errors.New("plain failure"))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &appErr); err != nil {
		t.Fatal(err)
	}
	if appErr.Error.Code != "internal_error" {
		t.Fatalf("code = %q", appErr.Error.Code)
	}
}
