package errors

import (
	"errors"
	"net/http"
	"testing"
)

func TestConstructors(t *testing.T) {
	cases := []struct {
		name       string
		err        *AppError
		code       string
		httpStatus int
	}{
		{name: "bad request", err: BadRequest("bad"), code: "bad_request", httpStatus: http.StatusBadRequest},
		{name: "validation", err: Validation("invalid", map[string]interface{}{"field": "email"}), code: "validation_error", httpStatus: http.StatusUnprocessableEntity},
		{name: "unauthorized", err: Unauthorized("no"), code: "unauthorized", httpStatus: http.StatusUnauthorized},
		{name: "forbidden", err: Forbidden("no"), code: "forbidden", httpStatus: http.StatusForbidden},
		{name: "not found", err: NotFound("missing"), code: "not_found", httpStatus: http.StatusNotFound},
		{name: "conflict", err: Conflict("dup"), code: "conflict", httpStatus: http.StatusConflict},
		{name: "unprocessable", err: Unprocessable("nope"), code: "unprocessable_entity", httpStatus: http.StatusUnprocessableEntity},
		{name: "rate limited", err: TooManyRequests("slow"), code: "rate_limited", httpStatus: http.StatusTooManyRequests},
		{name: "unsupported", err: Unsupported("nope"), code: "unsupported", httpStatus: http.StatusNotImplemented},
		{name: "internal", err: Internal("boom", errors.New("cause")), code: "internal_error", httpStatus: http.StatusInternalServerError},
		{name: "unavailable", err: ServiceUnavailable("down", errors.New("cause")), code: "service_unavailable", httpStatus: http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Fatalf("code = %q, want %q", tc.err.Code, tc.code)
			}
			if tc.err.HTTPStatus != tc.httpStatus {
				t.Fatalf("status = %d, want %d", tc.err.HTTPStatus, tc.httpStatus)
			}
			if tc.err.Message == "" {
				t.Fatal("expected message")
			}
		})
	}
}

func TestDetailsCauseAndError(t *testing.T) {
	cause := errors.New("database down")
	err := Internal("failed", cause)
	if err.Error() != "failed: database down" {
		t.Fatalf("error = %q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatal("expected unwrapped cause")
	}

	WithDetails(err, map[string]interface{}{"retry": true})
	if err.Details["retry"] != true {
		t.Fatalf("details = %v", err.Details)
	}
	WithCause(err, cause)
	if err.Err != cause {
		t.Fatalf("cause = %v", err.Err)
	}

	plain := New("custom", "plain", http.StatusTeapot)
	if plain.Error() != "plain" || plain.Unwrap() != nil {
		t.Fatalf("plain = %+v", plain)
	}
}

func TestIsAndAs(t *testing.T) {
	appErr := NotFound("missing")
	wrapped := errors.New("outer")
	combined := errors.Join(wrapped, appErr)

	if !Is(appErr, NotFound("other")) {
		t.Fatal("expected code match")
	}
	if Is(errors.New("plain"), appErr) {
		t.Fatal("plain error must not match")
	}
	if !Is(combined, appErr) {
		t.Fatal("expected joined app error to match")
	}

	found, ok := As(combined)
	if !ok || found.Code != "not_found" {
		t.Fatalf("As = %+v %v", found, ok)
	}
	if found, ok := As(errors.New("plain")); ok || found != nil {
		t.Fatalf("As plain = %+v %v", found, ok)
	}
}
