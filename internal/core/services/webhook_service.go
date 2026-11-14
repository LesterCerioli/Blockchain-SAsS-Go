package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
	"github.com/google/uuid"
)

type WebhookServiceOptions struct {
	MaxRetries int
	Notifier   ports.NotificationPort
}

type WebhookService struct {
	merchants  ports.MerchantRepository
	deliveries ports.WebhookDeliveryRepository
	notifier   ports.NotificationPort
	clock      ports.Clock
	logger     logger.Logger
	options    WebhookServiceOptions
}

func NewWebhookService(merchants ports.MerchantRepository, deliveries ports.WebhookDeliveryRepository, notifier ports.NotificationPort, clock ports.Clock, log logger.Logger, options WebhookServiceOptions) *WebhookService {
	if clock == nil {
		clock = ports.SystemClock{}
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	if options.MaxRetries <= 0 {
		options.MaxRetries = 5
	}
	return &WebhookService{
		merchants: merchants, deliveries: deliveries, notifier: notifier, clock: clock,
		logger: log, options: options,
	}
}

func (s *WebhookService) DispatchEvent(ctx context.Context, event *entities.Event) error {
	merchant, err := s.merchants.GetByID(ctx, event.MerchantID)
	if err != nil {
		return mapDomainError(err, "merchant not found")
	}
	if !merchant.HasWebhook() {
		return nil
	}
	payload, err := marshalEvent(event)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	delivery := &ports.WebhookDelivery{
		ID:         uuid.New(),
		MerchantID: merchant.ID,
		EventType:  string(event.Type),
		URL:        merchant.WebhookURL,
		Payload:    string(payload),
		Status:     "pending",
		CreatedAt:  now,
		UpdatedAt:  now,
		Metadata:   map[string]interface{}{"event_id": event.ID.String()},
	}
	if err := s.deliveries.Create(ctx, delivery); err != nil {
		return apperrors.Internal("failed to persist webhook delivery", err)
	}
	return s.attempt(ctx, delivery, merchant)
}

func (s *WebhookService) SendTest(ctx context.Context, merchantID uuid.UUID) (*ports.WebhookDelivery, error) {
	merchant, err := s.merchants.GetByID(ctx, merchantID)
	if err != nil {
		return nil, mapDomainError(err, "merchant not found")
	}
	if !merchant.HasWebhook() {
		return nil, apperrors.Validation("merchant has no webhook configured", nil)
	}
	now := s.clock.Now()
	event := entities.NewEvent(entities.EventWebhookTest, "merchant", merchant.ID, merchant.ID, now, map[string]interface{}{
		"message": "This is a test webhook from the Blockchain Payment Gateway",
	})
	payload, _ := marshalEvent(event)
	delivery := &ports.WebhookDelivery{
		ID:         uuid.New(),
		MerchantID: merchant.ID,
		EventType:  string(event.Type),
		URL:        merchant.WebhookURL,
		Payload:    string(payload),
		Status:     "pending",
		CreatedAt:  now,
		UpdatedAt:  now,
		Metadata:   map[string]interface{}{"event_id": event.ID.String(), "test": true},
	}
	if err := s.deliveries.Create(ctx, delivery); err != nil {
		return nil, apperrors.Internal("failed to persist webhook delivery", err)
	}
	if err := s.attempt(ctx, delivery, merchant); err != nil {
		return delivery, err
	}
	return delivery, nil
}

func (s *WebhookService) ListDeliveries(ctx context.Context, merchantID uuid.UUID, pagination ports.Pagination) ([]*ports.WebhookDelivery, int, error) {
	return s.deliveries.ListByMerchant(ctx, merchantID, pagination)
}

func (s *WebhookService) RetryPending(ctx context.Context) (int, error) {
	pending, err := s.deliveries.ListPending(ctx, s.clock.Now(), 100)
	if err != nil {
		return 0, err
	}
	retried := 0
	for _, delivery := range pending {
		merchant, err := s.merchants.GetByID(ctx, delivery.MerchantID)
		if err != nil {
			continue
		}
		if err := s.attempt(ctx, delivery, merchant); err != nil {
			s.logger.Warn(ctx, "webhook retry failed", "delivery_id", delivery.ID, "error", err)
			continue
		}
		retried++
	}
	return retried, nil
}

func (s *WebhookService) attempt(ctx context.Context, delivery *ports.WebhookDelivery, merchant *entities.Merchant) error {
	notification := ports.Notification{
		Channel:   ports.ChannelWebhook,
		Recipient: delivery.URL,
		Subject:   delivery.EventType,
		Body:      delivery.Payload,
		Secret:    merchant.WebhookSecret,
		Data:      map[string]interface{}{"delivery_id": delivery.ID.String(), "event_type": delivery.EventType},
		Headers:   map[string]string{"X-Event-Type": delivery.EventType},
	}
	err := s.notifier.Send(ctx, notification)
	now := s.clock.Now()
	delivery.Attempts++
	delivery.UpdatedAt = now
	if err == nil {
		delivery.Status = "delivered"
		delivery.StatusCode = 200
		delivery.LastError = ""
		delivery.NextAttemptAt = nil
		_ = s.deliveries.Update(ctx, delivery)
		return nil
	}

	delivery.LastError = err.Error()
	if statusCoder, ok := err.(interface{ HTTPStatusCode() int }); ok {
		delivery.StatusCode = statusCoder.HTTPStatusCode()
	}
	if delivery.Attempts >= s.options.MaxRetries {
		delivery.Status = "failed"
		delivery.NextAttemptAt = nil
	} else {
		delivery.Status = "retrying"
		backoff := time.Duration(1<<uint(delivery.Attempts)) * 5 * time.Second
		if backoff > time.Hour {
			backoff = time.Hour
		}
		next := now.Add(backoff)
		delivery.NextAttemptAt = &next
	}
	_ = s.deliveries.Update(ctx, delivery)
	return err
}

func marshalEvent(event *entities.Event) ([]byte, error) {
	payload := map[string]interface{}{
		"id":             event.ID.String(),
		"type":           string(event.Type),
		"aggregate_type": event.AggregateType,
		"aggregate_id":   event.AggregateID.String(),
		"merchant_id":    event.MerchantID.String(),
		"created_at":     event.OccurredAt.Format(time.RFC3339),
		"data":           event.Data,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook payload: %w", err)
	}
	return encoded, nil
}
