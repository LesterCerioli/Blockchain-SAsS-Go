package workers

import (
	"context"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
)

type ExpirationWorker struct {
	service  *services.PaymentService
	interval time.Duration
	logger   logger.Logger
}

func NewExpirationWorker(service *services.PaymentService, interval time.Duration, log logger.Logger) *ExpirationWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	return &ExpirationWorker{service: service, interval: interval, logger: log}
}

func (w *ExpirationWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := w.service.ExpireStale(ctx)
			if err != nil {
				w.logger.Warn(ctx, "expiration worker failed", "error", err)
				continue
			}
			if count > 0 {
				w.logger.Info(ctx, "expired payment intents", "count", count)
			}
		}
	}
}

type ConfirmationWorker struct {
	service  *services.PaymentService
	interval time.Duration
	logger   logger.Logger
}

func NewConfirmationWorker(service *services.PaymentService, interval time.Duration, log logger.Logger) *ConfirmationWorker {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	return &ConfirmationWorker{service: service, interval: interval, logger: log}
}

func (w *ConfirmationWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := w.service.RefreshPending(ctx)
			if err != nil {
				w.logger.Warn(ctx, "confirmation worker failed", "error", err)
				continue
			}
			if count > 0 {
				w.logger.Info(ctx, "refreshed pending payments", "count", count)
			}
		}
	}
}
