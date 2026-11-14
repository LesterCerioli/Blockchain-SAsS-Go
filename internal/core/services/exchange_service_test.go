package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
)

type recordingExchange struct {
	rate   float64
	err    error
	calls  int
	base   string
	quote  string
	health error
}

func (e *recordingExchange) Name() string { return "recording" }

func (e *recordingExchange) GetRate(_ context.Context, base, quote string) (*ports.ExchangeRate, error) {
	e.calls++
	e.base = base
	e.quote = quote
	if e.err != nil {
		return nil, e.err
	}
	return &ports.ExchangeRate{Base: base, Quote: quote, Rate: e.rate, Source: "recording", Timestamp: time.Now().UTC()}, nil
}

func (e *recordingExchange) Health(context.Context) error { return e.health }

type fakeCache struct {
	data    map[string][]byte
	getErr  error
	setErr  error
	gets    int
	sets    int
	lastTTL time.Duration
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: map[string][]byte{}}
}

func (c *fakeCache) Get(_ context.Context, key string) ([]byte, error) {
	c.gets++
	if c.getErr != nil {
		return nil, c.getErr
	}
	value, ok := c.data[key]
	if !ok {
		return nil, errors.New("cache miss")
	}
	return value, nil
}

func (c *fakeCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	c.sets++
	c.lastTTL = ttl
	if c.setErr != nil {
		return c.setErr
	}
	c.data[key] = value
	return nil
}

func (c *fakeCache) Delete(_ context.Context, key string) error {
	delete(c.data, key)
	return nil
}

func (c *fakeCache) Increment(context.Context, string, time.Duration) (int64, error) {
	return 0, nil
}

func (c *fakeCache) Ping(context.Context) error { return nil }

func (c *fakeCache) Close() error { return nil }

func TestExchangeServiceIdentityRate(t *testing.T) {
	provider := &recordingExchange{rate: 2}
	service := services.NewExchangeService(provider, nil, time.Minute, fixedClock{now: time.Now().UTC()}, nil)

	rate, err := service.GetRate(context.Background(), "usd", "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate.Rate != 1 || rate.Source != "identity" {
		t.Fatalf("rate = %+v", rate)
	}
	if provider.calls != 0 {
		t.Fatalf("identity rate must not call the provider, got %d calls", provider.calls)
	}
}

func TestExchangeServiceRequiresCurrencies(t *testing.T) {
	service := services.NewExchangeService(&recordingExchange{rate: 2}, nil, time.Minute, nil, nil)

	if _, err := service.GetRate(context.Background(), "", "USD"); err == nil {
		t.Fatal("expected error for missing base currency")
	}
	if _, err := service.GetRate(context.Background(), "ETH", ""); err == nil {
		t.Fatal("expected error for missing quote currency")
	}
}

func TestExchangeServiceNormalizesAndCaches(t *testing.T) {
	provider := &recordingExchange{rate: 2}
	cache := newFakeCache()
	service := services.NewExchangeService(provider, cache, 30*time.Second, nil, nil)

	first, err := service.GetRate(context.Background(), " eth ", "usd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.Rate != 2 {
		t.Fatalf("rate = %v, want 2", first.Rate)
	}
	if provider.base != "ETH" || provider.quote != "USD" {
		t.Fatalf("provider called with %s/%s", provider.base, provider.quote)
	}

	second, err := service.GetRate(context.Background(), "ETH", "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second.Rate != 2 {
		t.Fatalf("cached rate = %v, want 2", second.Rate)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	if cache.sets != 1 {
		t.Fatalf("cache sets = %d, want 1", cache.sets)
	}
	if cache.lastTTL != 30*time.Second {
		t.Fatalf("cache ttl = %s", cache.lastTTL)
	}
}

func TestExchangeServiceCacheHit(t *testing.T) {
	provider := &recordingExchange{rate: 9}
	cache := newFakeCache()
	payload, err := json.Marshal(&ports.ExchangeRate{Base: "ETH", Quote: "USD", Rate: 3, Source: "cache"})
	if err != nil {
		t.Fatal(err)
	}
	cache.data["exchange:rate:ETH:USD"] = payload

	service := services.NewExchangeService(provider, cache, time.Minute, nil, nil)
	rate, err := service.GetRate(context.Background(), "ETH", "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate.Rate != 3 || rate.Source != "cache" {
		t.Fatalf("rate = %+v", rate)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
}

func TestExchangeServiceProviderErrors(t *testing.T) {
	providerErr := errors.New("upstream unavailable")
	provider := &recordingExchange{err: providerErr}
	service := services.NewExchangeService(provider, nil, time.Minute, nil, nil)

	_, err := service.GetRate(context.Background(), "ETH", "USD")
	if err == nil {
		t.Fatal("expected provider error")
	}
	if !errors.Is(err, providerErr) {
		t.Fatalf("error = %v, want wrapped provider error", err)
	}

	invalid := services.NewExchangeService(&recordingExchange{rate: 0}, nil, time.Minute, nil, nil)
	if _, err := invalid.GetRate(context.Background(), "ETH", "USD"); err == nil {
		t.Fatal("expected invalid rate error")
	}
}

func TestExchangeServiceConvertFiatToCrypto(t *testing.T) {
	service := services.NewExchangeService(&recordingExchange{rate: 2}, nil, time.Minute, nil, nil)

	fiat, err := types.NewAmountFromDecimal("100", 2)
	if err != nil {
		t.Fatal(err)
	}
	crypto, rate, err := service.ConvertFiatToCrypto(context.Background(), fiat, "USD", "ETH", 18)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rate.Rate != 2 {
		t.Fatalf("rate = %v", rate.Rate)
	}
	if got := crypto.ToDecimalString(); got != "50" {
		t.Fatalf("crypto amount = %s, want 50", got)
	}
}

func TestExchangeServiceConvertRoundsUp(t *testing.T) {
	service := services.NewExchangeService(&recordingExchange{rate: 3}, nil, time.Minute, nil, nil)

	fiat, err := types.NewAmountFromDecimal("1", 2)
	if err != nil {
		t.Fatal(err)
	}
	crypto, _, err := service.ConvertFiatToCrypto(context.Background(), fiat, "USD", "ETH", 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := crypto.ToDecimalString(); got != "0.333334" {
		t.Fatalf("crypto amount = %s, want 0.333334", got)
	}
}

func TestExchangeServiceHealth(t *testing.T) {
	missing := services.NewExchangeService(nil, nil, time.Minute, nil, nil)
	if err := missing.Health(context.Background()); err == nil {
		t.Fatal("expected error when provider is missing")
	}

	unhealthy := services.NewExchangeService(&recordingExchange{rate: 1, health: errors.New("down")}, nil, time.Minute, nil, nil)
	if err := unhealthy.Health(context.Background()); err == nil {
		t.Fatal("expected provider health error")
	}

	healthy := services.NewExchangeService(&recordingExchange{rate: 1}, nil, time.Minute, nil, nil)
	if err := healthy.Health(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
