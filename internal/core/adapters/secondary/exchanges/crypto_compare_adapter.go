package exchanges

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

type CryptoCompareAdapter struct {
	baseURL string
	client  *http.Client
}

func NewCryptoCompareAdapter(baseURL string, timeout time.Duration) *CryptoCompareAdapter {
	if baseURL == "" {
		baseURL = "https://min-api.cryptocompare.com"
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &CryptoCompareAdapter{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: timeout}}
}

func (a *CryptoCompareAdapter) Name() string { return "cryptocompare" }

func (a *CryptoCompareAdapter) GetRate(ctx context.Context, base, quote string) (*ports.ExchangeRate, error) {
	base = strings.ToUpper(base)
	quote = strings.ToUpper(quote)
	if base == quote {
		return &ports.ExchangeRate{Base: base, Quote: quote, Rate: 1, Source: a.Name(), Timestamp: time.Now().UTC()}, nil
	}
	endpoint := fmt.Sprintf("%s/data/price?fsym=%s&tsyms=%s", a.baseURL, url.QueryEscape(base), url.QueryEscape(quote))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cryptocompare request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cryptocompare status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload map[string]float64
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	rate, ok := payload[quote]
	if !ok {
		return nil, fmt.Errorf("cryptocompare returned no rate for %s/%s", base, quote)
	}
	return &ports.ExchangeRate{
		Base: base, Quote: quote, Rate: rate,
		Source: a.Name(), Timestamp: time.Now().UTC(),
	}, nil
}

func (a *CryptoCompareAdapter) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/data/price?fsym=ETH&tsyms=USD", nil)
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("cryptocompare health status %d", resp.StatusCode)
	}
	return nil
}

type FallbackExchange struct {
	providers []ports.ExchangePort
}

func NewFallbackExchange(providers ...ports.ExchangePort) *FallbackExchange {
	return &FallbackExchange{providers: providers}
}

func (f *FallbackExchange) Name() string {
	names := make([]string, 0, len(f.providers))
	for _, provider := range f.providers {
		names = append(names, provider.Name())
	}
	return strings.Join(names, "+")
}

func (f *FallbackExchange) GetRate(ctx context.Context, base, quote string) (*ports.ExchangeRate, error) {
	if len(f.providers) == 0 {
		return nil, fmt.Errorf("no exchange provider configured")
	}
	var lastErr error
	for _, provider := range f.providers {
		rate, err := provider.GetRate(ctx, base, quote)
		if err == nil && rate != nil && rate.Rate > 0 {
			return rate, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all exchange providers failed: %w", lastErr)
}

func (f *FallbackExchange) Health(ctx context.Context) error {
	for _, provider := range f.providers {
		if err := provider.Health(ctx); err == nil {
			return nil
		}
	}
	return fmt.Errorf("all exchange providers unhealthy")
}
