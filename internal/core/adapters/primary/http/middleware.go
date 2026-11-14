package http

import (
	"context"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
	"github.com/blockchain-saas/payment-gateway/internal/shared/monitoring"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
	"github.com/blockchain-saas/payment-gateway/pkg/api"
	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
)

type contextKey string

const merchantContextKey contextKey = "merchant"

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rps     float64
	burst   float64
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(rps, burst int) *RateLimiter {
	if rps <= 0 {
		rps = 20
	}
	if burst <= 0 {
		burst = rps * 2
	}
	return &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		rps:     float64(rps),
		burst:   float64(burst),
	}
}

func (l *RateLimiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) > 10000 {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
	}
	bucket, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &tokenBucket{tokens: l.burst - 1, last: now}
		return true
	}
	elapsed := now.Sub(bucket.last).Seconds()
	bucket.tokens += elapsed * l.rps
	if bucket.tokens > l.burst {
		bucket.tokens = l.burst
	}
	bucket.last = now
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

type MiddlewareOptions struct {
	Auth           *services.MerchantService
	AdminKey       string
	RateLimitRPS   int
	RateLimitBurst int
	CORSOrigins    []string
	MaxBodyBytes   int64
	Metrics        *monitoring.Registry
	Logger         logger.Logger
	TraceEnabled   bool
}

type Middleware struct {
	auth         *services.MerchantService
	adminKey     string
	rateLimiter  *RateLimiter
	corsOrigins  []string
	maxBodyBytes int64
	metrics      *monitoring.Registry
	logger       logger.Logger
	traceEnabled bool
}

func NewMiddleware(options MiddlewareOptions) *Middleware {
	if options.Logger == nil {
		options.Logger = logger.NopLogger{}
	}
	return &Middleware{
		auth:         options.Auth,
		adminKey:     options.AdminKey,
		rateLimiter:  NewRateLimiter(options.RateLimitRPS, options.RateLimitBurst),
		corsOrigins:  options.CORSOrigins,
		maxBodyBytes: options.MaxBodyBytes,
		metrics:      options.Metrics,
		logger:       options.Logger,
		traceEnabled: options.TraceEnabled,
	}
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
	handler := next
	handler = m.recovery(handler)
	handler = m.requestContext(handler)
	handler = m.logging(handler)
	handler = m.metricsMiddleware(handler)
	handler = m.securityHeaders(handler)
	handler = m.cors(handler)
	handler = m.bodyLimit(handler)
	handler = m.rateLimit(handler)
	return handler
}

func (m *Middleware) requestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = api.NewRequestID()
		}
		ctx := api.WithRequestID(r.Context(), requestID)
		if m.traceEnabled {
			trace := monitoring.ParseTraceparent(r.Header.Get("traceparent"))
			ctx = monitoring.WithTrace(ctx, trace)
			w.Header().Set("traceparent", monitoring.Traceparent(ctx))
			w.Header().Set("X-Trace-ID", trace.TraceID)
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Middleware) recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				m.logger.Error(r.Context(), "panic recovered", "panic", recovered, "stack", string(debug.Stack()))
				api.WriteError(w, r, apperrors.Internal("internal server error", nil))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

func (m *Middleware) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		m.logger.Info(r.Context(), "http request",
			"method", r.Method, "path", r.URL.Path,
			"route", r.Pattern, "status", status, "bytes", recorder.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", api.RequestID(r.Context()), "remote_ip", ClientIP(r),
		)
	})
}

func (m *Middleware) metricsMiddleware(next http.Handler) http.Handler {
	if m.metrics == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		labels := map[string]string{"method": r.Method, "route": route, "status": httpStatusClass(status)}
		m.metrics.IncCounter("http_requests_total", "Total HTTP requests", labels)
		m.metrics.ObserveHistogram("http_request_duration_seconds", "HTTP request duration in seconds",
			map[string]string{"method": r.Method, "route": route}, time.Since(start).Seconds(), nil)
	})
}

func httpStatusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

func (m *Middleware) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && m.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key,X-API-Key,X-Request-ID,X-Signature")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID,X-Trace-ID")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) originAllowed(origin string) bool {
	for _, allowed := range m.corsOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

func (m *Middleware) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.maxBodyBytes > 0 && r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, m.maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		if !m.rateLimiter.Allow(ClientIP(r)) {
			api.WriteError(w, r, apperrors.TooManyRequests("rate limit exceeded"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		token := ""
		if authorization != "" {
			parts := strings.Fields(authorization)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				token = parts[1]
			} else if len(parts) == 2 && strings.EqualFold(parts[0], "ApiKey") {
				apiKey = parts[1]
			}
		}
		if apiKey == "" && token == "" {
			api.WriteError(w, r, apperrors.Unauthorized("authentication required"))
			return
		}

		var merchant *entities.Merchant
		var err error
		if apiKey != "" {
			merchant, err = m.auth.AuthenticateAPIKey(r.Context(), apiKey)
		} else if strings.HasPrefix(token, "pk_") {
			merchant, err = m.auth.AuthenticateAPIKey(r.Context(), token)
		} else {
			merchant, err = m.auth.AuthenticateJWT(r.Context(), token)
		}
		if err != nil {
			api.WriteError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), merchantContextKey, merchant)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Middleware) Admin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("X-Admin-Key"))
		if m.adminKey == "" || !utils.SecureCompare(key, m.adminKey) {
			api.WriteError(w, r, apperrors.Unauthorized("admin authentication required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func MerchantFromContext(ctx context.Context) (*entities.Merchant, bool) {
	merchant, ok := ctx.Value(merchantContextKey).(*entities.Merchant)
	return merchant, ok
}

func ClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			return strings.TrimSpace(parts[0])
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
