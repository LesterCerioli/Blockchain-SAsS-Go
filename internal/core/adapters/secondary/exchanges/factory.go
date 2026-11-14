package exchanges

import (
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

type Options struct {
	Provider         string
	BinanceURL       string
	CryptoCompareURL string
	Timeout          time.Duration
}

func NewExchange(options Options) ports.ExchangePort {
	binance := NewBinanceAdapter(options.BinanceURL, options.Timeout)
	cryptoCompare := NewCryptoCompareAdapter(options.CryptoCompareURL, options.Timeout)
	switch options.Provider {
	case "cryptocompare":
		return NewFallbackExchange(cryptoCompare, binance)
	case "auto":
		return NewFallbackExchange(binance, cryptoCompare)
	default:
		return NewFallbackExchange(binance, cryptoCompare)
	}
}
