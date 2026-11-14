package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
)

type NotificationService struct {
	merchants ports.MerchantRepository
	notifiers map[ports.NotificationChannel]ports.NotificationPort
	logger    logger.Logger
}

func NewNotificationService(merchants ports.MerchantRepository, notifiers map[ports.NotificationChannel]ports.NotificationPort, log logger.Logger) *NotificationService {
	if log == nil {
		log = logger.NopLogger{}
	}
	return &NotificationService{merchants: merchants, notifiers: notifiers, logger: log}
}

func (s *NotificationService) NotifyEvent(ctx context.Context, event *entities.Event) error {
	emailNotifier, ok := s.notifiers[ports.ChannelEmail]
	if !ok {
		return nil
	}
	switch event.Type {
	case entities.EventPaymentIntentSucceeded, entities.EventPaymentIntentRefunded, entities.EventPaymentIntentFailed:
	default:
		return nil
	}
	merchant, err := s.merchants.GetByID(ctx, event.MerchantID)
	if err != nil || merchant.Email == "" {
		return nil
	}
	subject := fmt.Sprintf("Payment %s", strings.TrimPrefix(string(event.Type), "payment_intent."))
	body := fmt.Sprintf("Hello %s,\n\nYour payment %v is now %s.\n\nReference: %v\n",
		merchant.Name, event.Data["payment_intent_id"], event.Data["status"], event.Data["reference"])
	notification := ports.Notification{
		Channel:   ports.ChannelEmail,
		Recipient: merchant.Email,
		Subject:   subject,
		Body:      body,
	}
	if err := emailNotifier.Send(ctx, notification); err != nil {
		s.logger.Warn(ctx, "failed to send email notification", "merchant_id", merchant.ID, "error", err)
		return err
	}
	return nil
}
