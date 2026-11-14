package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/google/uuid"
)

type captureNotifier struct {
	channel  ports.NotificationChannel
	sent     []ports.Notification
	err      error
	sendFunc func(ports.Notification) error
}

func (n *captureNotifier) Channel() ports.NotificationChannel { return n.channel }

func (n *captureNotifier) Send(_ context.Context, notification ports.Notification) error {
	n.sent = append(n.sent, notification)
	if n.sendFunc != nil {
		return n.sendFunc(notification)
	}
	return n.err
}

func newNotificationFixture(t *testing.T) (ports.Repositories, *entities.Merchant, time.Time) {
	t.Helper()
	repos := newMerchantRepository()
	now := time.Now().UTC()
	merchant := entities.NewMerchant("Acme", "payments@acme.test", "hash", "pk_test_abc", now)
	if err := repos.Merchants.Create(context.Background(), merchant); err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	return repos, merchant, now
}

func paymentSucceededEvent(merchantID uuid.UUID, now time.Time) *entities.Event {
	return entities.NewEvent(entities.EventPaymentIntentSucceeded, "payment_intent", uuid.New(), merchantID, now, map[string]interface{}{
		"payment_intent_id": "pi_123", "status": "succeeded", "reference": "order-1",
	})
}

func TestNotificationServiceSendsEmail(t *testing.T) {
	repos, merchant, now := newNotificationFixture(t)
	notifier := &captureNotifier{channel: ports.ChannelEmail}
	service := services.NewNotificationService(repos.Merchants, map[ports.NotificationChannel]ports.NotificationPort{
		ports.ChannelEmail: notifier,
	}, nil)

	if err := service.NotifyEvent(context.Background(), paymentSucceededEvent(merchant.ID, now)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifier.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(notifier.sent))
	}
	notification := notifier.sent[0]
	if notification.Recipient != merchant.Email {
		t.Fatalf("recipient = %q", notification.Recipient)
	}
	if notification.Subject != "Payment succeeded" {
		t.Fatalf("subject = %q", notification.Subject)
	}
	if !strings.Contains(notification.Body, "Acme") || !strings.Contains(notification.Body, "order-1") {
		t.Fatalf("body = %q", notification.Body)
	}
}

func TestNotificationServiceIgnoresOtherEvents(t *testing.T) {
	repos, merchant, now := newNotificationFixture(t)
	notifier := &captureNotifier{channel: ports.ChannelEmail}
	service := services.NewNotificationService(repos.Merchants, map[ports.NotificationChannel]ports.NotificationPort{
		ports.ChannelEmail: notifier,
	}, nil)

	event := entities.NewEvent(entities.EventPaymentIntentCreated, "payment_intent", uuid.New(), merchant.ID, now, nil)
	if err := service.NotifyEvent(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifier.sent) != 0 {
		t.Fatalf("sent = %d, want 0", len(notifier.sent))
	}
}

func TestNotificationServiceWithoutEmailChannel(t *testing.T) {
	repos, merchant, now := newNotificationFixture(t)
	service := services.NewNotificationService(repos.Merchants, map[ports.NotificationChannel]ports.NotificationPort{}, nil)

	if err := service.NotifyEvent(context.Background(), paymentSucceededEvent(merchant.ID, now)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotificationServiceMissingMerchant(t *testing.T) {
	repos, _, now := newNotificationFixture(t)
	notifier := &captureNotifier{channel: ports.ChannelEmail}
	service := services.NewNotificationService(repos.Merchants, map[ports.NotificationChannel]ports.NotificationPort{
		ports.ChannelEmail: notifier,
	}, nil)

	if err := service.NotifyEvent(context.Background(), paymentSucceededEvent(uuid.New(), now)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifier.sent) != 0 {
		t.Fatalf("sent = %d, want 0", len(notifier.sent))
	}
}

func TestNotificationServiceSkipsMerchantWithoutEmail(t *testing.T) {
	repos := newMerchantRepository()
	now := time.Now().UTC()
	merchant := entities.NewMerchant("Acme", "", "hash", "pk_test_abc", now)
	if err := repos.Merchants.Create(context.Background(), merchant); err != nil {
		t.Fatal(err)
	}
	notifier := &captureNotifier{channel: ports.ChannelEmail}
	service := services.NewNotificationService(repos.Merchants, map[ports.NotificationChannel]ports.NotificationPort{
		ports.ChannelEmail: notifier,
	}, nil)

	if err := service.NotifyEvent(context.Background(), paymentSucceededEvent(merchant.ID, now)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifier.sent) != 0 {
		t.Fatalf("sent = %d, want 0", len(notifier.sent))
	}
}

func TestNotificationServicePropagatesSendError(t *testing.T) {
	repos, merchant, now := newNotificationFixture(t)
	sendErr := errors.New("smtp down")
	notifier := &captureNotifier{channel: ports.ChannelEmail, err: sendErr}
	service := services.NewNotificationService(repos.Merchants, map[ports.NotificationChannel]ports.NotificationPort{
		ports.ChannelEmail: notifier,
	}, nil)

	err := service.NotifyEvent(context.Background(), paymentSucceededEvent(merchant.ID, now))
	if !errors.Is(err, sendErr) {
		t.Fatalf("error = %v, want %v", err, sendErr)
	}
}
