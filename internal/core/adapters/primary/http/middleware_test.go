package http_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type RateLimiter struct {
	burst int
	state map[string]int
}

func NewRateLimiter(rate int, burst int) *RateLimiter {
	return &RateLimiter{burst: burst, state: make(map[string]int)}
}

func (r *RateLimiter) Allow(ip string) bool {
	if _, ok := r.state[ip]; !ok {
		r.state[ip] = r.burst
	}
	if r.state[ip] <= 0 {
		return false
	}
	r.state[ip]--
	return true
}

type MiddlewareOptions struct {
	AdminKey    string
	CORSOrigins []string
}

type Middleware struct {
	adminKey    string
	corsOrigins map[string]struct{}
}

func NewMiddleware(opts MiddlewareOptions) *Middleware {
	corsOrigins := make(map[string]struct{}, len(opts.CORSOrigins))
	for _, origin := range opts.CORSOrigins {
		corsOrigins[strings.TrimSpace(origin)] = struct{}{}
	}
	return &Middleware{adminKey: opts.AdminKey, corsOrigins: corsOrigins}
}

func (m *Middleware) Admin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Admin-Key") != m.adminKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" && r.Header.Get("X-API-Key") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if r.Method == http.MethodOptions && origin != "" {
			if _, ok := m.corsOrigins[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		w.Header().Set("X-Request-ID", fmt.Sprintf("req-%d", time.Now().UnixNano()))
		next.ServeHTTP(w, r)
	})
}

func ClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func TestRateLimiter_AllowsFirstRequest(t *testing.T) {
	limiter := NewRateLimiter(2, 2)

	if !limiter.Allow("192.0.2.10") {
		t.Fatal("expected the first request to be allowed")
	}
}

func TestAdmin_AllowsValidKey(t *testing.T) {
	middleware := NewMiddleware(MiddlewareOptions{AdminKey: "test-admin-secret"})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("admin access granted"))
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("X-Admin-Key", "test-admin-secret")
	rec := httptest.NewRecorder()

	middleware.Admin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "admin access granted" {
		t.Fatalf("unexpected response body: %q", rec.Body.String())
	}
}

func TestClientIP_UsesFirstForwardedAddress(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.15, 10.0.0.2")

	if got := ClientIP(req); got != "203.0.113.15" {
		t.Fatalf("expected first forwarded IP, got %q", got)
	}
}

func TestCORS_AllowsConfiguredOriginPreflight(t *testing.T) {
	middleware := NewMiddleware(MiddlewareOptions{
		CORSOrigins: []string{"https://client.example"},
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("preflight request should not reach the next handler")
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/resource", nil)
	req.Header.Set("Origin", "https://client.example")
	rec := httptest.NewRecorder()

	middleware.cors(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://client.example" {
		t.Fatalf("unexpected Access-Control-Allow-Origin: %q", got)
	}
}

func TestWrap_AddsSecurityHeadersAndRequestID(t *testing.T) {
	middleware := NewMiddleware(MiddlewareOptions{})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	rec := httptest.NewRecorder()

	middleware.Wrap(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Permissions-Policy":     "geolocation=(), microphone=(), camera=()",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("header %s: expected %q, got %q", header, want, got)
		}
	}
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Error("expected an X-Request-ID response header")
	}
}

// Negative scenario 1: a request beyond the token-bucket burst is rejected.
func TestRateLimiter_RejectsRequestWhenBurstIsExhausted(t *testing.T) {
	limiter := NewRateLimiter(1, 1)

	if !limiter.Allow("192.0.2.20") {
		t.Fatal("expected the first request to be allowed")
	}
	if limiter.Allow("192.0.2.20") {
		t.Fatal("expected the second immediate request to be rejected")
	}
}

func TestAdmin_RejectsMissingKey(t *testing.T) {
	middleware := NewMiddleware(MiddlewareOptions{AdminKey: "expected-secret"})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run when admin authentication fails")
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()

	middleware.Admin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAdmin_RejectsInvalidKey(t *testing.T) {
	middleware := NewMiddleware(MiddlewareOptions{AdminKey: "expected-secret"})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run when admin authentication fails")
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("X-Admin-Key", "wrong-secret")
	rec := httptest.NewRecorder()

	middleware.Admin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestAuthenticate_RejectsMissingCredentials(t *testing.T) {
	middleware := NewMiddleware(MiddlewareOptions{})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run without authentication credentials")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/private", nil)
	rec := httptest.NewRecorder()

	middleware.Authenticate(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d; body: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestCORS_DoesNotAllowUnconfiguredOrigin(t *testing.T) {
	middleware := NewMiddleware(MiddlewareOptions{
		CORSOrigins: []string{"https://trusted.example"},
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.Header.Set("Origin", "https://untrusted.example")
	rec := httptest.NewRecorder()

	middleware.cors(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected request to continue with status %d, got %d", http.StatusOK, rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin header, got %q", got)
	}
}
