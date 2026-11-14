package repositories

import (
	"context"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

type Options struct {
	Driver          string
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	AutoMigrate     bool
}

type Bundle struct {
	Repositories ports.Repositories
	Ping         func(ctx context.Context) error
	Close        func() error
}

func NewRepositoryFactory(ctx context.Context, options Options) (*Bundle, error) {
	switch options.Driver {
	case "postgres", "postgresql":
		store, err := NewPostgresStore(ctx, options.DSN, options.MaxOpenConns, options.MaxIdleConns, options.ConnMaxLifetime)
		if err != nil {
			return nil, err
		}
		if options.AutoMigrate {
			if err := store.Migrate(ctx); err != nil {
				store.Close()
				return nil, err
			}
		}
		return &Bundle{
			Repositories: store.Repositories(),
			Ping:         store.Ping,
			Close:        store.Close,
		}, nil
	default:
		store := NewMemoryRepository()
		return &Bundle{
			Repositories: store.Repositories(),
			Ping:         func(context.Context) error { return nil },
			Close:        func() error { return nil },
		}, nil
	}
}
