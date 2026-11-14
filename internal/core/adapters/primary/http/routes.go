package http

import (
	"net/http"
	"os"
	"path/filepath"
)

type Server struct {
	handler    *Handler
	middleware *Middleware
	webRoot    string
	mux        *http.ServeMux
}

func NewServer(handler *Handler, middleware *Middleware, webRoot string) *Server {
	server := &Server{handler: handler, middleware: middleware, webRoot: webRoot, mux: http.NewServeMux()}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.middleware.Wrap(s.mux)
}

func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

func (s *Server) routes() {
	h := s.handler
	auth := s.middleware.Authenticate
	admin := s.middleware.Admin

	s.mux.HandleFunc("GET /healthz", h.Liveness)
	s.mux.HandleFunc("GET /readyz", h.Readiness)
	s.mux.HandleFunc("GET /metrics", h.Metrics)

	s.mux.HandleFunc("GET /v1/chains", h.Chains)
	s.mux.HandleFunc("GET /v1/rates", h.Rate)

	s.mux.HandleFunc("POST /v1/merchants/register", h.RegisterMerchant)
	s.mux.HandleFunc("POST /v1/auth/login", h.Login)

	s.mux.Handle("GET /v1/merchant", auth(http.HandlerFunc(h.Me)))
	s.mux.Handle("PATCH /v1/merchant/webhook", auth(http.HandlerFunc(h.UpdateWebhook)))
	s.mux.Handle("POST /v1/merchant/rotate-key", auth(http.HandlerFunc(h.RotateKey)))

	s.mux.Handle("POST /v1/payments", auth(http.HandlerFunc(h.CreatePaymentIntent)))
	s.mux.Handle("GET /v1/payments", auth(http.HandlerFunc(h.ListPaymentIntents)))
	s.mux.Handle("GET /v1/payments/{id}", auth(http.HandlerFunc(h.GetPaymentIntent)))
	s.mux.Handle("POST /v1/payments/{id}/confirm", auth(http.HandlerFunc(h.ConfirmPaymentIntent)))
	s.mux.Handle("POST /v1/payments/{id}/cancel", auth(http.HandlerFunc(h.CancelPaymentIntent)))
	s.mux.Handle("POST /v1/payments/{id}/refund", auth(http.HandlerFunc(h.RefundPaymentIntent)))

	s.mux.Handle("POST /v1/escrows", auth(http.HandlerFunc(h.CreateEscrow)))
	s.mux.Handle("GET /v1/escrows", auth(http.HandlerFunc(h.ListEscrows)))
	s.mux.Handle("GET /v1/escrows/{id}", auth(http.HandlerFunc(h.GetEscrow)))
	s.mux.Handle("POST /v1/escrows/{id}/release", auth(http.HandlerFunc(h.ReleaseEscrow)))
	s.mux.Handle("POST /v1/escrows/{id}/refund", auth(http.HandlerFunc(h.RefundEscrow)))
	s.mux.Handle("POST /v1/escrows/{id}/cancel", auth(http.HandlerFunc(h.CancelEscrow)))

	s.mux.Handle("GET /v1/webhooks/deliveries", auth(http.HandlerFunc(h.ListWebhookDeliveries)))
	s.mux.Handle("POST /v1/webhooks/test", auth(http.HandlerFunc(h.TestWebhook)))
	s.mux.HandleFunc("POST /v1/webhooks/blockchain", h.InboundBlockchainWebhook)

	s.mux.Handle("GET /v1/audit-logs", auth(http.HandlerFunc(h.ListAuditLogs)))
	s.mux.Handle("GET /v1/admin/merchants", admin(http.HandlerFunc(h.AdminListMerchants)))

	if s.webRoot != "" {
		if info, err := os.Stat(s.webRoot); err == nil && info.IsDir() {
			fileServer := http.FileServer(http.Dir(s.webRoot))
			s.mux.Handle("GET /", fileServer)
			if _, err := os.Stat(filepath.Join(s.webRoot, "index.html")); err == nil {
				s.mux.HandleFunc("GET /dashboard", func(w http.ResponseWriter, r *http.Request) {
					http.ServeFile(w, r, filepath.Join(s.webRoot, "index.html"))
				})
			}
		}
	}
}
