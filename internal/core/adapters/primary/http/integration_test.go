package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpadapter "github.com/blockchain-saas/payment-gateway/internal/core/adapters/primary/http"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/blockchain"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/cache"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/repositories"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/monitoring"
	"github.com/google/uuid"
)

const adminKey = "admin-secret"

type stubExchange struct{}

func (stubExchange) Name() string { return "stub" }

func (stubExchange) GetRate(_ context.Context, base, quote string) (*ports.ExchangeRate, error) {
	return &ports.ExchangeRate{Base: base, Quote: quote, Rate: 2, Source: "stub", Timestamp: time.Now().UTC()}, nil
}

func (stubExchange) Health(context.Context) error { return nil }

type fakeNotifier struct {
	channel ports.NotificationChannel
	err     error
	sent    int
}

func (n *fakeNotifier) Channel() ports.NotificationChannel { return n.channel }

func (n *fakeNotifier) Send(context.Context, ports.Notification) error {
	n.sent++
	return n.err
}

type harness struct {
	server *httptest.Server
	repos  ports.Repositories
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	repos := repositories.NewMemoryRepository().Repositories()
	registry := blockchain.NewRegistry(blockchain.Options{
		Mock: true,
		Chains: map[types.BlockchainType]blockchain.AdapterOptions{
			types.BlockchainEthereum: {
				Enabled: true, Network: types.NetworkEthereumSepolia, RequiredConfirmations: 12,
			},
		},
	})

	exchange := services.NewExchangeService(stubExchange{}, cache.NewMemoryCache(), time.Minute, nil, nil)
	validation := services.NewValidationService()
	fraud := services.NewFraudService(repos.Payments, services.FraudOptions{MaxAmountUSD: 1_000_000}, nil)
	merchants := services.NewMerchantService(
		repos.Merchants, repos.Audit, validation, nil, nil, "test",
		services.TokenOptions{
			Secret: strings.Repeat("s", 32), Issuer: "test", Audience: []string{"test"}, TTL: time.Hour,
		},
	)
	payments := services.NewPaymentService(
		repos.Merchants, repos.Payments, repos.Transactions, repos.Audit, registry,
		exchange, validation, fraud, nil, nil, nil,
		services.PaymentServiceOptions{DefaultExpiry: 15 * time.Minute, MaxExpiry: time.Hour},
	)
	webhooks := services.NewWebhookService(
		repos.Merchants, repos.Webhooks, &fakeNotifier{channel: ports.ChannelWebhook}, nil, nil,
		services.WebhookServiceOptions{MaxRetries: 3},
	)
	escrows := services.NewEscrowService(
		repos.Escrows, repos.Payments, repos.Audit, registry, nil, nil, nil,
		services.EscrowServiceOptions{MaxDuration: 720 * time.Hour, AllowForceRelease: true},
	)

	metrics := monitoring.NewRegistry()
	health := monitoring.NewHealthChecker("test")
	health.AddCheck(monitoring.Check{Name: "dummy", Fn: func(context.Context) error { return nil }})

	handler := httpadapter.NewHandler(httpadapter.HandlerDeps{
		Merchants: merchants, Payments: payments, Escrows: escrows, Webhooks: webhooks,
		Exchange: exchange, Registry: registry, Audit: repos.Audit, Health: health, Metrics: metrics,
	})
	middleware := httpadapter.NewMiddleware(httpadapter.MiddlewareOptions{
		Auth: merchants, AdminKey: adminKey, RateLimitRPS: 1000, RateLimitBurst: 1000,
		CORSOrigins: []string{"*"}, MaxBodyBytes: 1 << 20, Metrics: metrics,
	})
	server := httpadapter.NewServer(handler, middleware, "")
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	return &harness{server: httpServer, repos: repos}
}

func (h *harness) request(t *testing.T, method, path string, headers map[string]string, payload interface{}) (int, []byte) {
	t.Helper()
	var body []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = encoded
	}
	return h.requestRaw(t, method, path, headers, body)
}

func (h *harness) requestRaw(t *testing.T, method, path string, headers map[string]string, body []byte) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, responseBody
}

func (h *harness) authed(t *testing.T, method, path, apiKey string, payload interface{}) (int, []byte) {
	t.Helper()
	return h.request(t, method, path, map[string]string{"X-API-Key": apiKey}, payload)
}

func (h *harness) registerMerchant(t *testing.T, email, webhookURL string) (string, map[string]interface{}) {
	t.Helper()
	payload := map[string]interface{}{"name": "Integration Merchant", "email": email}
	if webhookURL != "" {
		payload["webhook_url"] = webhookURL
	}
	status, body := h.request(t, http.MethodPost, "/v1/merchants/register", nil, payload)
	if status != http.StatusCreated {
		t.Fatalf("register merchant status = %d, body = %s", status, body)
	}
	data := decodeData(t, body)
	apiKey, _ := data["api_key"].(string)
	if apiKey == "" {
		t.Fatalf("register merchant response missing api key: %s", body)
	}
	if secret, _ := data["webhook_secret"].(string); secret == "" {
		t.Fatalf("register merchant response missing webhook secret: %s", body)
	}
	if webhookURL != "" {
		status, body = h.authed(t, http.MethodPatch, "/v1/merchant/webhook", apiKey, map[string]interface{}{
			"webhook_url": webhookURL,
		})
		if status != http.StatusOK {
			t.Fatalf("configure webhook status = %d, body = %s", status, body)
		}
	}
	return apiKey, data
}

func (h *harness) createPayment(t *testing.T, apiKey, amount, reference string) map[string]interface{} {
	t.Helper()
	status, body := h.authed(t, http.MethodPost, "/v1/payments", apiKey, map[string]interface{}{
		"amount": amount, "currency": "ETH", "blockchain": "ethereum", "network": "sepolia", "reference": reference,
	})
	if status != http.StatusCreated {
		t.Fatalf("create payment status = %d, body = %s", status, body)
	}
	return decodeData(t, body)
}

func (h *harness) fundPayment(t *testing.T, payment map[string]interface{}) map[string]interface{} {
	t.Helper()
	status, body := h.request(t, http.MethodPost, "/v1/webhooks/blockchain", nil, map[string]interface{}{
		"blockchain": "ethereum", "network": "sepolia", "tx_hash": newTxHash(),
		"from": "0xsender", "to": payment["deposit_address"], "amount": payment["crypto_amount"],
		"confirmations": 12, "status": "confirmed",
	})
	if status != http.StatusOK {
		t.Fatalf("fund payment status = %d, body = %s", status, body)
	}
	return decodeData(t, body)
}

func newTxHash() string {
	return "0x" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func decodeEnvelope(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var envelope map[string]interface{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode envelope: %v (body %s)", err, body)
	}
	return envelope
}

func decodeData(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, body = %s", body)
	}
	return data
}

func decodeList(t *testing.T, body []byte) []interface{} {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	data, ok := envelope["data"].([]interface{})
	if !ok {
		t.Fatalf("expected array data, body = %s", body)
	}
	return data
}

func decodePagination(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	pagination, ok := envelope["pagination"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected pagination, body = %s", body)
	}
	return pagination
}

func decodeErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	errorBody, ok := envelope["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error body, body = %s", body)
	}
	code, _ := errorBody["code"].(string)
	return code
}

func TestIntegrationHealthAndMetadata(t *testing.T) {
	h := newHarness(t)

	status, body := h.request(t, http.MethodGet, "/healthz", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("healthz status = %d", status)
	}
	var liveness map[string]interface{}
	if err := json.Unmarshal(body, &liveness); err != nil {
		t.Fatalf("decode liveness: %v", err)
	}
	if liveness["status"] != "ok" {
		t.Fatalf("liveness = %v", liveness)
	}

	status, body = h.request(t, http.MethodGet, "/readyz", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("readyz status = %d, body = %s", status, body)
	}
	var readiness map[string]interface{}
	if err := json.Unmarshal(body, &readiness); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if readiness["status"] != "ok" {
		t.Fatalf("readiness = %v", readiness)
	}
	checks, ok := readiness["checks"].(map[string]interface{})
	if !ok || checks["dummy"] == nil {
		t.Fatalf("readiness checks = %v", readiness["checks"])
	}

	status, body = h.request(t, http.MethodGet, "/metrics", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("metrics status = %d", status)
	}
	if !strings.Contains(string(body), "http_requests_total") {
		t.Fatalf("metrics body = %s", body)
	}

	status, body = h.request(t, http.MethodGet, "/v1/chains", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("chains status = %d, body = %s", status, body)
	}
	chains := decodeList(t, body)
	if len(chains) != 1 {
		t.Fatalf("chains = %v", chains)
	}
	chain, _ := chains[0].(map[string]interface{})
	if chain["type"] != string(types.BlockchainEthereum) {
		t.Fatalf("chain = %v", chain)
	}
}

func TestIntegrationRates(t *testing.T) {
	h := newHarness(t)

	status, body := h.request(t, http.MethodGet, "/v1/rates", nil, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("missing query status = %d, body = %s", status, body)
	}
	if code := decodeErrorCode(t, body); code != "validation_error" {
		t.Fatalf("error code = %s", code)
	}

	status, body = h.request(t, http.MethodGet, "/v1/rates?base=ETH&quote=ETH", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("identity rate status = %d, body = %s", status, body)
	}
	rate := decodeData(t, body)
	if rate["rate"] != float64(1) || rate["source"] != "identity" {
		t.Fatalf("rate = %v", rate)
	}

	status, body = h.request(t, http.MethodGet, "/v1/rates?base=ETH&quote=USD", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("rate status = %d, body = %s", status, body)
	}
	rate = decodeData(t, body)
	if rate["rate"] != float64(2) || rate["source"] != "stub" {
		t.Fatalf("rate = %v", rate)
	}
}

func TestIntegrationMerchantLifecycle(t *testing.T) {
	h := newHarness(t)

	status, body := h.request(t, http.MethodGet, "/v1/merchant", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated me status = %d", status)
	}

	apiKey, registered := h.registerMerchant(t, "merchant@example.com", "https://example.com/hook")
	merchant := registered["merchant"].(map[string]interface{})
	merchantID, _ := merchant["id"].(string)
	if merchantID == "" {
		t.Fatal("missing merchant id")
	}

	status, body = h.request(t, http.MethodPost, "/v1/merchants/register", nil, map[string]interface{}{
		"name": "Duplicate", "email": "merchant@example.com",
	})
	if status != http.StatusConflict {
		t.Fatalf("duplicate register status = %d, body = %s", status, body)
	}

	status, body = h.request(t, http.MethodPost, "/v1/merchants/register", nil, map[string]interface{}{
		"name": "Invalid", "email": "not-an-email",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid register status = %d, body = %s", status, body)
	}

	status, body = h.request(t, http.MethodPost, "/v1/auth/login", nil, map[string]interface{}{"api_key": apiKey})
	if status != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", status, body)
	}
	login := decodeData(t, body)
	token, _ := login["token"].(string)
	if token == "" {
		t.Fatalf("login response = %v", login)
	}

	status, body = h.request(t, http.MethodGet, "/v1/merchant", map[string]string{"Authorization": "Bearer " + token}, nil)
	if status != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", status, body)
	}
	me := decodeData(t, body)
	if me["id"] != merchantID {
		t.Fatalf("me = %v", me)
	}

	status, body = h.authed(t, http.MethodGet, "/v1/merchant", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("api key me status = %d, body = %s", status, body)
	}

	status, body = h.authed(t, http.MethodPatch, "/v1/merchant/webhook", apiKey, map[string]interface{}{
		"webhook_url": "ftp://example.com/hook",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid webhook status = %d, body = %s", status, body)
	}

	status, body = h.authed(t, http.MethodPatch, "/v1/merchant/webhook", apiKey, map[string]interface{}{
		"webhook_url": "https://example.com/new-hook",
	})
	if status != http.StatusOK {
		t.Fatalf("update webhook status = %d, body = %s", status, body)
	}
	if secret, _ := decodeData(t, body)["webhook_secret"].(string); secret == "" {
		t.Fatal("expected webhook secret")
	}

	status, body = h.authed(t, http.MethodPost, "/v1/merchant/rotate-key", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("rotate status = %d, body = %s", status, body)
	}
	rotated := decodeData(t, body)
	newKey, _ := rotated["api_key"].(string)
	if newKey == "" || newKey == apiKey {
		t.Fatalf("rotated api key = %q", newKey)
	}

	status, _ = h.authed(t, http.MethodGet, "/v1/merchant", apiKey, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("old api key status = %d, want 401", status)
	}
	status, _ = h.authed(t, http.MethodGet, "/v1/merchant", newKey, nil)
	if status != http.StatusOK {
		t.Fatalf("new api key status = %d, want 200", status)
	}
}

func TestIntegrationPaymentLifecycle(t *testing.T) {
	h := newHarness(t)
	apiKey, _ := h.registerMerchant(t, "payments@example.com", "")

	status, body := h.request(t, http.MethodPost, "/v1/payments", nil, map[string]interface{}{
		"amount": "0.01", "currency": "ETH", "blockchain": "ethereum", "network": "sepolia",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d", status)
	}

	status, body = h.authed(t, http.MethodPost, "/v1/payments", apiKey, map[string]interface{}{
		"amount": "abc", "currency": "ETH", "blockchain": "ethereum", "network": "sepolia",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid amount status = %d, body = %s", status, body)
	}

	status, body = h.authed(t, http.MethodPost, "/v1/payments", apiKey, map[string]interface{}{
		"amount": "0.01", "currency": "ETH", "blockchain": "ethereum", "network": "sepolia",
		"merchantId": uuid.NewString(),
	})
	if status != http.StatusForbidden {
		t.Fatalf("merchant mismatch status = %d, body = %s", status, body)
	}

	payment := h.createPayment(t, apiKey, "0.01", "order-1")
	paymentID, _ := payment["id"].(string)
	if paymentID == "" || payment["deposit_address"] == "" || payment["status"] != "requires_payment" {
		t.Fatalf("payment = %v", payment)
	}

	status, body = h.authed(t, http.MethodGet, "/v1/payments/"+paymentID, apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("get payment status = %d, body = %s", status, body)
	}
	if got := decodeData(t, body)["id"]; got != paymentID {
		t.Fatalf("payment id = %v", got)
	}

	status, body = h.authed(t, http.MethodGet, "/v1/payments/not-a-uuid", apiKey, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid id status = %d, body = %s", status, body)
	}
	status, body = h.authed(t, http.MethodGet, "/v1/payments/"+uuid.NewString(), apiKey, nil)
	if status != http.StatusNotFound {
		t.Fatalf("missing payment status = %d, body = %s", status, body)
	}

	status, body = h.authed(t, http.MethodGet, "/v1/payments?status=requires_payment&page=1&page_size=10", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("list payments status = %d, body = %s", status, body)
	}
	if total := decodePagination(t, body)["total"].(float64); total < 1 {
		t.Fatalf("list total = %v", total)
	}

	status, body = h.request(t, http.MethodPost, "/v1/payments", map[string]string{
		"X-API-Key": apiKey, "Idempotency-Key": "idem-integration",
	}, map[string]interface{}{
		"amount": "0.01", "currency": "ETH", "blockchain": "ethereum", "network": "sepolia", "reference": "idem",
	})
	if status != http.StatusCreated {
		t.Fatalf("idempotent create status = %d, body = %s", status, body)
	}
	first := decodeData(t, body)
	status, body = h.request(t, http.MethodPost, "/v1/payments", map[string]string{
		"X-API-Key": apiKey, "Idempotency-Key": "idem-integration",
	}, map[string]interface{}{
		"amount": "0.02", "currency": "ETH", "blockchain": "ethereum", "network": "sepolia", "reference": "idem-2",
	})
	if status != http.StatusCreated {
		t.Fatalf("idempotent create status = %d, body = %s", status, body)
	}
	second := decodeData(t, body)
	if first["id"] != second["id"] {
		t.Fatalf("idempotent ids = %v / %v", first["id"], second["id"])
	}

	status, body = h.authed(t, http.MethodPost, "/v1/payments/"+paymentID+"/confirm", apiKey, map[string]interface{}{
		"tx_hash": "0xconfirm",
	})
	if status != http.StatusOK {
		t.Fatalf("confirm status = %d, body = %s", status, body)
	}
	if got := decodeData(t, body)["status"]; got != "processing" {
		t.Fatalf("confirmed status = %v", got)
	}

	status, body = h.authed(t, http.MethodPost, "/v1/payments/"+paymentID+"/cancel", apiKey, nil)
	if status != http.StatusConflict {
		t.Fatalf("cancel processing status = %d, body = %s", status, body)
	}

	cancelable := h.createPayment(t, apiKey, "0.02", "order-cancel")
	cancelID := cancelable["id"].(string)
	status, body = h.authed(t, http.MethodPost, "/v1/payments/"+cancelID+"/cancel", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("cancel status = %d, body = %s", status, body)
	}
	if got := decodeData(t, body)["status"]; got != "canceled" {
		t.Fatalf("canceled status = %v", got)
	}
	status, body = h.authed(t, http.MethodPost, "/v1/payments/"+cancelID+"/cancel", apiKey, nil)
	if status != http.StatusConflict {
		t.Fatalf("double cancel status = %d, body = %s", status, body)
	}

	refundable := h.createPayment(t, apiKey, "0.03", "order-refund")
	h.fundPayment(t, refundable)
	refundID := refundable["id"].(string)
	status, body = h.authed(t, http.MethodPost, "/v1/payments/"+refundID+"/refund", apiKey, map[string]interface{}{
		"tx_hash": "0xrefund",
	})
	if status != http.StatusOK {
		t.Fatalf("refund status = %d, body = %s", status, body)
	}
	if got := decodeData(t, body)["status"]; got != "refunded" {
		t.Fatalf("refunded status = %v", got)
	}
	status, body = h.authed(t, http.MethodPost, "/v1/payments/"+refundID+"/refund", apiKey, map[string]interface{}{
		"tx_hash": "0xagain",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("double refund status = %d, body = %s", status, body)
	}

	duplicate := h.createPayment(t, apiKey, "0.04", "order-duplicate")
	duplicateInbound := map[string]interface{}{
		"blockchain": "ethereum", "network": "sepolia", "tx_hash": "0xduplicate",
		"from": "0xsender", "to": duplicate["deposit_address"], "amount": duplicate["crypto_amount"],
		"confirmations": 12, "status": "confirmed",
	}
	status, body = h.request(t, http.MethodPost, "/v1/webhooks/blockchain", nil, duplicateInbound)
	if status != http.StatusOK {
		t.Fatalf("duplicate inbound status = %d, body = %s", status, body)
	}
	status, body = h.request(t, http.MethodPost, "/v1/webhooks/blockchain", nil, duplicateInbound)
	if status != http.StatusAccepted || !strings.Contains(string(body), `"duplicate":true`) {
		t.Fatalf("duplicate inbound status = %d, body = %s", status, body)
	}

	status, body = h.request(t, http.MethodPost, "/v1/webhooks/blockchain", nil, map[string]interface{}{
		"blockchain": "ethereum", "network": "sepolia", "tx_hash": "0xunmatched",
		"to": "0xunmatched", "amount": "1", "confirmations": 12, "status": "confirmed",
	})
	if status != http.StatusAccepted || !strings.Contains(string(body), `"matched":false`) {
		t.Fatalf("unmatched inbound status = %d, body = %s", status, body)
	}

	status, body = h.requestRaw(t, http.MethodPost, "/v1/webhooks/blockchain", nil, []byte("{"))
	if status != http.StatusBadRequest {
		t.Fatalf("invalid json status = %d, body = %s", status, body)
	}
	status, body = h.request(t, http.MethodPost, "/v1/webhooks/blockchain", nil, map[string]interface{}{
		"blockchain": "dogecoin", "network": "mainnet", "tx_hash": "0xchain", "to": "0xabc", "amount": "1",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid chain status = %d, body = %s", status, body)
	}
	status, body = h.request(t, http.MethodPost, "/v1/webhooks/blockchain", nil, map[string]interface{}{
		"blockchain": "ethereum", "network": "sepolia", "tx_hash": "0xbadamount", "to": "0xabc", "amount": "abc",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid amount status = %d, body = %s", status, body)
	}
}

func TestIntegrationEscrowLifecycle(t *testing.T) {
	h := newHarness(t)
	apiKey, _ := h.registerMerchant(t, "escrows@example.com", "")

	payment := h.createPayment(t, apiKey, "0.05", "escrow")
	h.fundPayment(t, payment)
	paymentID := payment["id"].(string)

	status, body := h.authed(t, http.MethodPost, "/v1/escrows", apiKey, map[string]interface{}{
		"payment_intent_id": paymentID, "beneficiary": "0xbeneficiary", "arbiter": "0xarbiter",
	})
	if status != http.StatusCreated {
		t.Fatalf("create escrow status = %d, body = %s", status, body)
	}
	escrow := decodeData(t, body)
	escrowID := escrow["id"].(string)
	if escrow["status"] != "funded" {
		t.Fatalf("escrow = %v", escrow)
	}

	status, body = h.authed(t, http.MethodGet, "/v1/escrows", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("list escrows status = %d, body = %s", status, body)
	}
	if total := decodePagination(t, body)["total"].(float64); total < 1 {
		t.Fatalf("escrow total = %v", total)
	}

	status, body = h.authed(t, http.MethodGet, "/v1/escrows/"+escrowID, apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("get escrow status = %d, body = %s", status, body)
	}
	status, body = h.authed(t, http.MethodGet, "/v1/escrows/"+uuid.NewString(), apiKey, nil)
	if status != http.StatusNotFound {
		t.Fatalf("missing escrow status = %d, body = %s", status, body)
	}

	status, body = h.authed(t, http.MethodPost, "/v1/escrows/"+escrowID+"/release", apiKey, map[string]interface{}{
		"tx_hash": "0xrelease",
	})
	if status != http.StatusOK {
		t.Fatalf("release escrow status = %d, body = %s", status, body)
	}
	if got := decodeData(t, body)["status"]; got != "released" {
		t.Fatalf("released status = %v", got)
	}
	status, body = h.authed(t, http.MethodPost, "/v1/escrows/"+escrowID+"/release", apiKey, map[string]interface{}{
		"tx_hash": "0xrelease-2",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("double release status = %d, body = %s", status, body)
	}

	status, body = h.authed(t, http.MethodPost, "/v1/escrows", apiKey, map[string]interface{}{
		"payment_intent_id": paymentID, "beneficiary": "0xbeneficiary",
	})
	if status != http.StatusCreated {
		t.Fatalf("create refundable escrow status = %d, body = %s", status, body)
	}
	refundID := decodeData(t, body)["id"].(string)
	status, body = h.authed(t, http.MethodPost, "/v1/escrows/"+refundID+"/refund", apiKey, map[string]interface{}{
		"tx_hash": "0xescrow-refund",
	})
	if status != http.StatusOK {
		t.Fatalf("refund escrow status = %d, body = %s", status, body)
	}
	if got := decodeData(t, body)["status"]; got != "refunded" {
		t.Fatalf("refunded status = %v", got)
	}

	status, body = h.authed(t, http.MethodPost, "/v1/escrows", apiKey, map[string]interface{}{
		"payment_intent_id": paymentID, "beneficiary": "0xbeneficiary",
	})
	if status != http.StatusCreated {
		t.Fatalf("create cancelable escrow status = %d, body = %s", status, body)
	}
	cancelID := decodeData(t, body)["id"].(string)
	status, body = h.authed(t, http.MethodPost, "/v1/escrows/"+cancelID+"/cancel", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("cancel escrow status = %d, body = %s", status, body)
	}
	if got := decodeData(t, body)["status"]; got != "canceled" {
		t.Fatalf("canceled status = %v", got)
	}

	pending := h.createPayment(t, apiKey, "0.01", "escrow-pending")
	status, body = h.authed(t, http.MethodPost, "/v1/escrows", apiKey, map[string]interface{}{
		"payment_intent_id": pending["id"], "beneficiary": "0xbeneficiary",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("pending escrow status = %d, body = %s", status, body)
	}

	status, body = h.authed(t, http.MethodPost, "/v1/escrows", apiKey, map[string]interface{}{
		"payment_intent_id": "not-a-uuid", "beneficiary": "0xbeneficiary",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid payment id status = %d, body = %s", status, body)
	}

	status, body = h.authed(t, http.MethodPost, "/v1/escrows", apiKey, map[string]interface{}{
		"payment_intent_id": paymentID, "beneficiary": "",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid beneficiary status = %d, body = %s", status, body)
	}
}

func TestIntegrationWebhooksAndAdmin(t *testing.T) {
	h := newHarness(t)
	apiKey, _ := h.registerMerchant(t, "webhooks@example.com", "https://example.com/hook")

	status, body := h.authed(t, http.MethodGet, "/v1/webhooks/deliveries", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("list deliveries status = %d, body = %s", status, body)
	}
	if total := decodePagination(t, body)["total"].(float64); total != 0 {
		t.Fatalf("deliveries total = %v, want 0", total)
	}

	status, body = h.authed(t, http.MethodPost, "/v1/webhooks/test", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("test webhook status = %d, body = %s", status, body)
	}
	delivery := decodeData(t, body)
	if delivery["status"] != "delivered" || delivery["event_type"] != "webhook.test" {
		t.Fatalf("delivery = %v", delivery)
	}

	status, body = h.authed(t, http.MethodGet, "/v1/webhooks/deliveries", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("list deliveries status = %d, body = %s", status, body)
	}
	if total := decodePagination(t, body)["total"].(float64); total != 1 {
		t.Fatalf("deliveries total = %v, want 1", total)
	}

	status, body = h.authed(t, http.MethodGet, "/v1/audit-logs", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("audit logs status = %d, body = %s", status, body)
	}
	if total := decodePagination(t, body)["total"].(float64); total < 1 {
		t.Fatalf("audit total = %v", total)
	}

	status, body = h.request(t, http.MethodGet, "/v1/admin/merchants", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("admin without key status = %d, body = %s", status, body)
	}
	status, body = h.request(t, http.MethodGet, "/v1/admin/merchants", map[string]string{"X-Admin-Key": "wrong"}, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("admin wrong key status = %d, body = %s", status, body)
	}
	status, body = h.request(t, http.MethodGet, "/v1/admin/merchants", map[string]string{"X-Admin-Key": adminKey}, nil)
	if status != http.StatusOK {
		t.Fatalf("admin status = %d, body = %s", status, body)
	}
	if total := decodePagination(t, body)["total"].(float64); total < 1 {
		t.Fatalf("admin total = %v", total)
	}
}
