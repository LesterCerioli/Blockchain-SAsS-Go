package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/blockchain-saas/payment-gateway/internal/app"
	"github.com/blockchain-saas/payment-gateway/internal/config"
)

func TestHTTPPaymentFlow(t *testing.T) {
	t.Setenv("BLOCKCHAIN_MOCK", "true")
	t.Setenv("DATABASE_DRIVER", "memory")
	t.Setenv("CACHE_DRIVER", "memory")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("LOG_FORMAT", "text")
	t.Setenv("APP_ENV", "test")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	application, err := app.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	defer application.Close()

	server := httptest.NewServer(application.HTTPServer.Handler)
	defer server.Close()

	if status, _ := request(t, server.URL, http.MethodGet, "/v1/payments", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request status = %d, want 401", status)
	}

	registerBody := map[string]interface{}{
		"name": "HTTP Merchant", "email": "http-merchant@example.com",
	}
	status, body := request(t, server.URL, http.MethodPost, "/v1/merchants/register", "", registerBody)
	if status != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", status, body)
	}
	register := decodeEnvelope(t, body)
	data := register["data"].(map[string]interface{})
	apiKey := data["api_key"].(string)
	if apiKey == "" {
		t.Fatal("expected api key")
	}

	paymentBody := map[string]interface{}{
		"amount": "0.01", "currency": "ETH",
		"blockchain": "ethereum", "network": "sepolia", "reference": "http-order",
	}
	status, body = request(t, server.URL, http.MethodPost, "/v1/payments", apiKey, paymentBody)
	if status != http.StatusCreated {
		t.Fatalf("create payment status = %d, body = %s", status, body)
	}
	payment := decodeEnvelope(t, body)["data"].(map[string]interface{})
	paymentID := payment["id"].(string)
	depositAddress := payment["deposit_address"].(string)
	cryptoAmount := payment["crypto_amount"].(string)

	inbound := map[string]interface{}{
		"blockchain": "ethereum", "network": "sepolia",
		"tx_hash": "0xhttp123", "from": "0xsender", "to": depositAddress,
		"amount": cryptoAmount, "confirmations": 12, "status": "confirmed",
	}
	status, body = request(t, server.URL, http.MethodPost, "/v1/webhooks/blockchain", "", inbound)
	if status != http.StatusOK {
		t.Fatalf("inbound webhook status = %d, body = %s", status, body)
	}

	status, body = request(t, server.URL, http.MethodGet, "/v1/payments/"+paymentID, apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("get payment status = %d, body = %s", status, body)
	}
	updated := decodeEnvelope(t, body)["data"].(map[string]interface{})
	if updated["status"] != "succeeded" {
		t.Fatalf("payment status = %v, want succeeded", updated["status"])
	}
	if updated["tx_hash"] != "0xhttp123" {
		t.Fatalf("tx hash = %v", updated["tx_hash"])
	}
}

func request(t *testing.T, baseURL, method, path, apiKey string, payload interface{}) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, body
}

func decodeEnvelope(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var envelope map[string]interface{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode envelope: %v (body %s)", err, body)
	}
	return envelope
}
