package cache

import "github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"

type Options struct {
	Driver   string
	Addr     string
	Password string
	DB       int
}

func NewCache(options Options) ports.Cache {
	if options.Driver == "redis" {
		return NewRedisCache(options.Addr, options.Password, options.DB)
	}
	return NewMemoryCache()
}
