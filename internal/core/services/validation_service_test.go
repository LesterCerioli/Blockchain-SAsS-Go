package services_test

import (
	"strings"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
)

func TestValidationServiceValidateMerchant(t *testing.T) {
	service := services.NewValidationService()

	tests := []struct {
		name         string
		merchantName string
		email        string
		wantErr      bool
	}{
		{name: "valid", merchantName: "Acme", email: "billing@acme.test"},
		{name: "empty name", merchantName: "  ", email: "billing@acme.test", wantErr: true},
		{name: "long name", merchantName: strings.Repeat("a", 201), email: "billing@acme.test", wantErr: true},
		{name: "invalid email", merchantName: "Acme", email: "not-an-email", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.ValidateMerchant(tt.merchantName, tt.email)
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidationServiceParseAmount(t *testing.T) {
	service := services.NewValidationService()

	tests := []struct {
		name     string
		amount   string
		currency string
		want     string
		wantErr  bool
	}{
		{name: "usd", amount: "12.50", currency: "usd", want: "12.5"},
		{name: "eth", amount: "0.000000000000000001", currency: "ETH", want: "0.000000000000000001"},
		{name: "missing currency", amount: "1", currency: "", wantErr: true},
		{name: "unsupported currency", amount: "1", currency: "XYZ", wantErr: true},
		{name: "zero amount", amount: "0", currency: "USD", wantErr: true},
		{name: "negative amount", amount: "-1", currency: "USD", wantErr: true},
		{name: "too many decimals", amount: "1.001", currency: "USD", wantErr: true},
		{name: "invalid number", amount: "abc", currency: "USD", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, err := service.ParseAmount(tt.amount, tt.currency)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got amount %v", amount)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := amount.ToDecimalString(); got != tt.want {
				t.Fatalf("amount = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestValidationServiceResolveChainNetwork(t *testing.T) {
	service := services.NewValidationService()

	chain, network, err := service.ResolveChainNetwork("eth", "sepolia")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain != types.BlockchainEthereum || network != types.NetworkEthereumSepolia {
		t.Fatalf("resolved %s/%s", chain, network)
	}

	if _, _, err := service.ResolveChainNetwork("dogecoin", ""); err == nil {
		t.Fatal("expected unsupported blockchain error")
	}
	if _, _, err := service.ResolveChainNetwork("ethereum", "moon"); err != nil {
		t.Fatalf("unknown networks are passed through: %v", err)
	}
}

func TestValidationServiceExpiry(t *testing.T) {
	service := services.NewValidationService()
	fallback := 15 * time.Minute

	expiry, err := service.Expiry(0, fallback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expiry != fallback {
		t.Fatalf("expiry = %s, want %s", expiry, fallback)
	}

	if _, err := service.Expiry(30, fallback); err == nil {
		t.Fatal("expected expiry below minimum to fail")
	}
	if _, err := service.Expiry(24*60*60+1, fallback); err == nil {
		t.Fatal("expected expiry above maximum to fail")
	}
}

func TestValidationServiceValidateWebhookURL(t *testing.T) {
	service := services.NewValidationService()

	tests := []struct {
		url     string
		wantErr bool
	}{
		{url: ""},
		{url: "https://example.com/hooks/payments"},
		{url: "http://localhost:8080/hook"},
		{url: "ftp://example.com/hook", wantErr: true},
		{url: "http://", wantErr: true},
		{url: "://bad", wantErr: true},
	}

	for _, tt := range tests {
		err := service.ValidateWebhookURL(tt.url)
		if tt.wantErr && err == nil {
			t.Fatalf("expected error for %q", tt.url)
		}
		if !tt.wantErr && err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.url, err)
		}
	}
}

func TestValidationServiceIsCryptoCurrency(t *testing.T) {
	service := services.NewValidationService()

	for _, currency := range []string{"ETH", "btc", " usdt ", "SOL", "ADA", "USDC", "DAI", "BNB", "MATIC"} {
		if !service.IsCryptoCurrency(currency) {
			t.Fatalf("expected %q to be crypto", currency)
		}
	}
	for _, currency := range []string{"USD", "EUR", "", "DOGE"} {
		if service.IsCryptoCurrency(currency) {
			t.Fatalf("expected %q not to be crypto", currency)
		}
	}
}
