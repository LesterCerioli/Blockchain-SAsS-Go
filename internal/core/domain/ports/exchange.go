package ports

import (
	"context"
	"time"
)

type ExchangeRate struct {
	Base      string    `json:"base"`
	Quote     string    `json:"quote"`
	Rate      float64   `json:"rate"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
}

type ExchangePort interface {
	Name() string
	GetRate(ctx context.Context, base, quote string) (*ExchangeRate, error)
	Health(ctx context.Context) error
}
