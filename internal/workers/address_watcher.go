package workers

import (
	"context"
	"errors"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
)

type AddressWatcher struct {
	payments ports.PaymentIntentRepository
	registry ports.BlockchainRegistry
	service  *services.PaymentService
	interval time.Duration
	pageSize int
	logger   logger.Logger
}

func NewAddressWatcher(payments ports.PaymentIntentRepository, registry ports.BlockchainRegistry, service *services.PaymentService, interval time.Duration, log logger.Logger) *AddressWatcher {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	return &AddressWatcher{
		payments: payments, registry: registry, service: service,
		interval: interval, pageSize: 100, logger: log,
	}
}

func (w *AddressWatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scan(ctx)
		}
	}
}

func (w *AddressWatcher) scan(ctx context.Context) {
	payments, _, err := w.payments.List(ctx, ports.PaymentFilter{
		Statuses:   []entities.PaymentIntentStatus{entities.PaymentStatusRequiresPayment, entities.PaymentStatusUnderReview},
		Pagination: ports.Pagination{Limit: w.pageSize},
	})
	if err != nil {
		w.logger.Warn(ctx, "address watcher list failed", "error", err)
		return
	}
	for _, payment := range payments {
		if payment.DepositAddress == "" {
			continue
		}
		adapter, err := w.registry.Get(payment.Blockchain, payment.Network)
		if err != nil {
			continue
		}
		transactions, err := adapter.GetAddressTransactions(ctx, payment.DepositAddress)
		if err != nil {
			w.logger.Warn(ctx, "address watcher scan failed", "payment_id", payment.ID, "error", err)
			continue
		}
		for _, tx := range transactions {
			tx.Blockchain = payment.Blockchain
			tx.Network = payment.Network
			_, err := w.service.HandleIncoming(ctx, tx)
			if err != nil {
				if errors.Is(err, entities.ErrNotFound) {
					continue
				}
				w.logger.Warn(ctx, "failed to process incoming transaction", "tx_hash", tx.TxHash, "error", err)
			}
		}
	}
}
