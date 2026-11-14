package http_test

import (
	"net/http/httptest"
	"testing"

	httpapi "github.com/blockchain-saas/payment-gateway/internal/core/adapters/primary/http"
)

func newTestServer(t *testing.T, webRoot string) *httpapi.Server {
	t.Helper()

	handler := &httpapi.Handler{}

	middleware := httpapi.NewMiddleware(
		httpapi.MiddlewareOptions{
			RateLimitRPS:   100,
			RateLimitBurst: 100,
		},
	)

	return httpapi.NewServer(handler, middleware, webRoot)
}

func getRoutePattern(
	server *httpapi.Server,
	method string,
	path string,
) string {
	request := httptest.NewRequest(method, path, nil)
	_, pattern := server.Mux().Handler(request)

	return pattern
}
