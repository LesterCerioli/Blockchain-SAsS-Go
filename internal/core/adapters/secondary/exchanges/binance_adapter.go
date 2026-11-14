package exchanges

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

type BinanceAdapter struct {
	baseURL string
	client  *http.Client
}

func NewBinanceAdapter(baseURL string, timeout time.Duration) *BinanceAdapter {
	if baseURL == "" {
		baseURL = "https://api.binance.com"
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &BinanceAdapter{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: timeout}}
}

func (a *BinanceAdapter) Name() string { return "binance" }

func (a *BinanceAdapter) GetRate(ctx context.Context, base, quote string) (*ports.ExchangeRate, error) {
	base = strings.ToUpper(base)
	quote = strings.ToUpper(quote)
	if base == quote {
		return &ports.ExchangeRate{Base: base, Quote: quote, Rate: 1, Source: a.Name(), Timestamp: time.Now().UTC()}, nil
	}

	if quote == "USD" {
		quote = "USDT"
	}
	symbol := base + quote
	endpoint := fmt.Sprintf("%s/api/v3/ticker/price?symbol=%s", a.baseURL, url.QueryEscape(symbol))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("binance request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("binance status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	rate, err := strconv.ParseFloat(payload.Price, 64)
	if err != nil {
		return nil, err
	}
	return &ports.ExchangeRate{
		Base:      base,
		Quote:     quote,
		Rate:      rate,
		Source:    a.Name() + ":" + payload.Symbol,
		Timestamp: time.Now().UTC(),
	}, nil
}

func (a *BinanceAdapter) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/api/v3/ping", nil)
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("binance health status %d", resp.StatusCode)
	}
	return nil
}
