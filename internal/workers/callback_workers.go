package workers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
)

type CallbackWorker struct {
	queue    ports.Queue
	webhooks *services.WebhookService
	notifier *services.NotificationService
	topic    string
	logger   logger.Logger
}

func NewCallbackWorker(queue ports.Queue, webhooks *services.WebhookService, notifier *services.NotificationService, topic string, log logger.Logger) *CallbackWorker {
	if topic == "" {
		topic = services.EventsTopic
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	return &CallbackWorker{queue: queue, webhooks: webhooks, notifier: notifier, topic: topic, logger: log}
}

func (w *CallbackWorker) Start(ctx context.Context) error {
	return w.queue.Subscribe(w.topic, func(ctx context.Context, payload []byte) error {
		var event entities.Event
		if err := json.Unmarshal(payload, &event); err != nil {
			w.logger.Warn(ctx, "invalid event payload", "error", err)
			return err
		}
		if w.webhooks != nil {
			if err := w.webhooks.DispatchEvent(ctx, &event); err != nil {
				w.logger.Warn(ctx, "webhook dispatch failed", "event_id", event.ID, "error", err)
			}
		}
		if w.notifier != nil {
			if err := w.notifier.NotifyEvent(ctx, &event); err != nil {
				w.logger.Warn(ctx, "notification failed", "event_id", event.ID, "error", err)
			}
		}
		return nil
	})
}

type WebhookRetryWorker struct {
	service  *services.WebhookService
	interval time.Duration
	logger   logger.Logger
}

func NewWebhookRetryWorker(service *services.WebhookService, interval time.Duration, log logger.Logger) *WebhookRetryWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	return &WebhookRetryWorker{service: service, interval: interval, logger: log}
}

func (w *WebhookRetryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := w.service.RetryPending(ctx)
			if err != nil {
				w.logger.Warn(ctx, "webhook retry worker failed", "error", err)
				continue
			}
			if count > 0 {
				w.logger.Info(ctx, "retried webhook deliveries", "count", count)
			}
		}
	}
}

type EscrowWorker struct {
	service  *services.EscrowService
	interval time.Duration
	logger   logger.Logger
}

func NewEscrowWorker(service *services.EscrowService, interval time.Duration, log logger.Logger) *EscrowWorker {
	if interval <= 0 {
		interval = time.Minute
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	return &EscrowWorker{service: service, interval: interval, logger: log}
}

func (w *EscrowWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := w.service.ReleaseDue(ctx)
			if err != nil {
				w.logger.Warn(ctx, "escrow worker failed", "error", err)
				continue
			}
			if count > 0 {
				w.logger.Info(ctx, "released due escrows", "count", count)
			}
		}
	}
}
