package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
)

func TestMapDomainError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{name: "not found", err: entities.ErrNotFound, code: "not_found"},
		{name: "conflict", err: entities.ErrConflict, code: "conflict"},
		{name: "invalid state", err: entities.ErrInvalidState, code: "unprocessable_entity"},
		{name: "expired", err: entities.ErrExpired, code: "unprocessable_entity"},
		{name: "unsupported", err: entities.ErrUnsupported, code: "unsupported"},
		{name: "unauthorized", err: entities.ErrUnauthorized, code: "unauthorized"},
		{name: "forbidden", err: entities.ErrForbidden, code: "forbidden"},
		{name: "invalid amount", err: entities.ErrInvalidAmount, code: "unprocessable_entity"},
		{name: "invalid address", err: entities.ErrInvalidAddress, code: "unprocessable_entity"},
		{name: "blocked", err: entities.ErrBlocked, code: "forbidden"},
		{name: "rate limited", err: entities.ErrRateLimited, code: "rate_limited"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapped := mapDomainError(tc.err, "resource not found")
			appErr, ok := apperrors.As(mapped)
			if !ok {
				t.Fatalf("expected application error, got %T: %v", mapped, mapped)
			}
			if appErr.Code != tc.code {
				t.Fatalf("code = %q, want %q", appErr.Code, tc.code)
			}
		})
	}

	if mapped := mapDomainError(nil, "ignored"); mapped != nil {
		t.Fatalf("nil error mapped to %v", mapped)
	}

	unknown := errors.New("boom")
	if mapped := mapDomainError(unknown, "ignored"); mapped != unknown {
		t.Fatalf("unknown error = %v, want passthrough", mapped)
	}
}

func TestMerchantNotFound(t *testing.T) {
	base := errors.New("store failure")
	err := merchantNotFound(base)
	if !errors.Is(err, entities.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), base.Error()) {
		t.Fatalf("error = %v, want message containing %q", err, base)
	}
}

func TestInferChain(t *testing.T) {
	cases := map[string]string{
		"BTC":   "bitcoin",
		"btc":   "bitcoin",
		"SOL":   "solana",
		"ADA":   "cardano",
		"ETH":   "ethereum",
		"USDT":  "ethereum",
		"USDC":  "ethereum",
		"DAI":   "ethereum",
		"BNB":   "bsc",
		"MATIC": "polygon",
		"DOGE":  "ethereum",
	}
	for currency, want := range cases {
		if got := inferChain(currency); got != want {
			t.Errorf("inferChain(%q) = %q, want %q", currency, got, want)
		}
	}
}
