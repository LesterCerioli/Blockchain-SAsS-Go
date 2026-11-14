package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
)

type ExchangeService struct {
	provider ports.ExchangePort
	cache    ports.Cache
	ttl      time.Duration
	clock    ports.Clock
	logger   logger.Logger
}

func NewExchangeService(provider ports.ExchangePort, cache ports.Cache, ttl time.Duration, clock ports.Clock, log logger.Logger) *ExchangeService {
	if ttl <= 0 {
		ttl = time.Minute
	}
	if clock == nil {
		clock = ports.SystemClock{}
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	return &ExchangeService{provider: provider, cache: cache, ttl: ttl, clock: clock, logger: log}
}

func (s *ExchangeService) GetRate(ctx context.Context, base, quote string) (*ports.ExchangeRate, error) {
	base = utils.NormalizeCurrency(base)
	quote = utils.NormalizeCurrency(quote)
	if base == "" || quote == "" {
		return nil, fmt.Errorf("base and quote currencies are required")
	}
	if base == quote {
		return &ports.ExchangeRate{Base: base, Quote: quote, Rate: 1, Source: "identity", Timestamp: s.clock.Now()}, nil
	}

	cacheKey := fmt.Sprintf("exchange:rate:%s:%s", base, quote)
	if s.cache != nil {
		if cached, err := s.cache.Get(ctx, cacheKey); err == nil {
			var rate ports.ExchangeRate
			if json.Unmarshal(cached, &rate) == nil && rate.Rate > 0 {
				return &rate, nil
			}
		}
	}

	rate, err := s.provider.GetRate(ctx, base, quote)
	if err != nil {
		return nil, fmt.Errorf("get exchange rate %s/%s: %w", base, quote, err)
	}
	if rate.Rate <= 0 {
		return nil, fmt.Errorf("invalid exchange rate for %s/%s", base, quote)
	}
	if s.cache != nil {
		if payload, err := json.Marshal(rate); err == nil {
			_ = s.cache.Set(ctx, cacheKey, payload, s.ttl)
		}
	}
	return rate, nil
}

func (s *ExchangeService) ConvertFiatToCrypto(ctx context.Context, fiat *types.Amount, fiatCurrency, cryptoCurrency string, cryptoDecimals int) (*types.Amount, *ports.ExchangeRate, error) {
	rate, err := s.GetRate(ctx, cryptoCurrency, fiatCurrency)
	if err != nil {
		return nil, nil, err
	}
	if rate.Rate <= 0 {
		return nil, nil, fmt.Errorf("invalid rate")
	}

	fiatRat := new(big.Rat).SetInt(fiat.Value)
	fiatRat.Quo(fiatRat, pow10Rat(fiat.Decimals))
	cryptoTokens := new(big.Rat).Quo(fiatRat, new(big.Rat).SetFloat64(rate.Rate))
	atomic := ratToAtomicCeil(cryptoTokens, cryptoDecimals)
	return &types.Amount{Value: atomic, Decimals: cryptoDecimals}, rate, nil
}

func (s *ExchangeService) Health(ctx context.Context) error {
	if s.provider == nil {
		return fmt.Errorf("exchange provider not configured")
	}
	return s.provider.Health(ctx)
}

func pow10Rat(decimals int) *big.Rat {
	return new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
}

func ratToAtomicCeil(value *big.Rat, decimals int) *big.Int {
	scaled := new(big.Rat).Mul(value, pow10Rat(decimals))
	num := new(big.Int).Set(scaled.Num())
	den := new(big.Int).Set(scaled.Denom())
	quotient, remainder := new(big.Int).QuoRem(num, den, new(big.Int))
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}
