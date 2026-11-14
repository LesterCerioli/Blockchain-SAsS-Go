package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/google/uuid"
)

type webhookStatusError struct {
	status int
}

func (e *webhookStatusError) Error() string { return "webhook endpoint returned an error" }

func (e *webhookStatusError) HTTPStatusCode() int { return e.status }

func createWebhookMerchant(t *testing.T, repos ports.Repositories, now time.Time, withWebhook bool) *entities.Merchant {
	t.Helper()
	merchant := entities.NewMerchant("Acme", "ops@acme.test", "hash", "pk_test_abc", now)
	if withWebhook {
		merchant.SetWebhook("https://hooks.example.com/payments", "whsec_test", now)
	}
	if err := repos.Merchants.Create(context.Background(), merchant); err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	return merchant
}

func TestWebhookServiceDispatchEvent(t *testing.T) {
	repos := newMerchantRepository()
	now := time.Now().UTC()
	merchant := createWebhookMerchant(t, repos, now, true)
	notifier := &captureNotifier{channel: ports.ChannelWebhook}
	service := services.NewWebhookService(repos.Merchants, repos.Webhooks, notifier, fixedClock{now: now}, nil, services.WebhookServiceOptions{MaxRetries: 3})

	event := entities.NewEvent(entities.EventPaymentIntentSucceeded, "payment_intent", uuid.New(), merchant.ID, now, map[string]interface{}{"status": "succeeded"})
	if err := service.DispatchEvent(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deliveries, total, err := service.ListDeliveries(context.Background(), merchant.ID, ports.Pagination{Limit: 10})
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if total != 1 || len(deliveries) != 1 {
		t.Fatalf("total = %d deliveries = %d", total, len(deliveries))
	}
	delivery := deliveries[0]
	if delivery.Status != "delivered" || delivery.Attempts != 1 || delivery.StatusCode != 200 {
		t.Fatalf("delivery = %+v", delivery)
	}
	if delivery.EventType != string(entities.EventPaymentIntentSucceeded) {
		t.Fatalf("event type = %s", delivery.EventType)
	}
	if len(notifier.sent) != 1 || notifier.sent[0].Recipient != merchant.WebhookURL {
		t.Fatalf("notifications = %+v", notifier.sent)
	}
	if notifier.sent[0].Secret != merchant.WebhookSecret {
		t.Fatal("expected webhook secret to be forwarded")
	}
}

func TestWebhookServiceDispatchWithoutWebhook(t *testing.T) {
	repos := newMerchantRepository()
	now := time.Now().UTC()
	merchant := createWebhookMerchant(t, repos, now, false)
	notifier := &captureNotifier{channel: ports.ChannelWebhook}
	service := services.NewWebhookService(repos.Merchants, repos.Webhooks, notifier, fixedClock{now: now}, nil, services.WebhookServiceOptions{})

	event := entities.NewEvent(entities.EventPaymentIntentSucceeded, "payment_intent", uuid.New(), merchant.ID, now, nil)
	if err := service.DispatchEvent(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, total, err := service.ListDeliveries(context.Background(), merchant.ID, ports.Pagination{})
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if len(notifier.sent) != 0 {
		t.Fatalf("sent = %d, want 0", len(notifier.sent))
	}
}

func TestWebhookServiceDispatchMissingMerchant(t *testing.T) {
	repos := newMerchantRepository()
	now := time.Now().UTC()
	service := services.NewWebhookService(repos.Merchants, repos.Webhooks, &captureNotifier{channel: ports.ChannelWebhook}, fixedClock{now: now}, nil, services.WebhookServiceOptions{})

	event := entities.NewEvent(entities.EventPaymentIntentSucceeded, "payment_intent", uuid.New(), uuid.New(), now, nil)
	err := service.DispatchEvent(context.Background(), event)
	requireAppErrorCode(t, err, "not_found")
}

func TestWebhookServiceRetriesUntilFailure(t *testing.T) {
	repos := newMerchantRepository()
	now := time.Now().UTC()
	merchant := createWebhookMerchant(t, repos, now, true)
	notifier := &captureNotifier{channel: ports.ChannelWebhook, err: &webhookStatusError{status: 500}}
	service := services.NewWebhookService(repos.Merchants, repos.Webhooks, notifier, fixedClock{now: now}, nil, services.WebhookServiceOptions{MaxRetries: 2})

	event := entities.NewEvent(entities.EventPaymentIntentSucceeded, "payment_intent", uuid.New(), merchant.ID, now, nil)
	if err := service.DispatchEvent(context.Background(), event); err == nil {
		t.Fatal("expected first delivery to fail")
	}

	deliveries, _, err := service.ListDeliveries(context.Background(), merchant.ID, ports.Pagination{})
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d", len(deliveries))
	}
	first := deliveries[0]
	if first.Status != "retrying" || first.Attempts != 1 || first.StatusCode != 500 {
		t.Fatalf("delivery = %+v", first)
	}
	if first.NextAttemptAt == nil {
		t.Fatal("expected a next attempt time")
	}

	future := services.NewWebhookService(repos.Merchants, repos.Webhooks, notifier, fixedClock{now: now.Add(2 * time.Hour)}, nil, services.WebhookServiceOptions{MaxRetries: 2})
	retried, err := future.RetryPending(context.Background())
	if err != nil {
		t.Fatalf("retry pending: %v", err)
	}
	if retried != 0 {
		t.Fatalf("retried = %d, want 0", retried)
	}

	deliveries, _, err = future.ListDeliveries(context.Background(), merchant.ID, ports.Pagination{})
	if err != nil {
		t.Fatal(err)
	}
	if deliveries[0].Status != "failed" || deliveries[0].Attempts != 2 {
		t.Fatalf("delivery = %+v", deliveries[0])
	}
}

func TestWebhookServiceRetryPendingSuccess(t *testing.T) {
	repos := newMerchantRepository()
	now := time.Now().UTC()
	merchant := createWebhookMerchant(t, repos, now, true)
	notifier := &captureNotifier{channel: ports.ChannelWebhook}
	service := services.NewWebhookService(repos.Merchants, repos.Webhooks, notifier, fixedClock{now: now}, nil, services.WebhookServiceOptions{})

	delivery := &ports.WebhookDelivery{
		ID: uuid.New(), MerchantID: merchant.ID, EventType: string(entities.EventWebhookTest),
		URL: merchant.WebhookURL, Payload: `{"ok":true}`, Status: "pending",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repos.Webhooks.Create(context.Background(), delivery); err != nil {
		t.Fatal(err)
	}

	retried, err := service.RetryPending(context.Background())
	if err != nil {
		t.Fatalf("retry pending: %v", err)
	}
	if retried != 1 {
		t.Fatalf("retried = %d, want 1", retried)
	}
	stored, err := repos.Webhooks.GetByID(context.Background(), delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "delivered" || stored.Attempts != 1 {
		t.Fatalf("delivery = %+v", stored)
	}
}

func TestWebhookServiceSendTest(t *testing.T) {
	repos := newMerchantRepository()
	now := time.Now().UTC()
	merchant := createWebhookMerchant(t, repos, now, true)
	notifier := &captureNotifier{channel: ports.ChannelWebhook}
	service := services.NewWebhookService(repos.Merchants, repos.Webhooks, notifier, fixedClock{now: now}, nil, services.WebhookServiceOptions{})

	delivery, err := service.SendTest(context.Background(), merchant.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if delivery.EventType != string(entities.EventWebhookTest) || delivery.Status != "delivered" {
		t.Fatalf("delivery = %+v", delivery)
	}
	if testFlag, _ := delivery.Metadata["test"].(bool); !testFlag {
		t.Fatalf("metadata = %+v", delivery.Metadata)
	}
}

func TestWebhookServiceSendTestErrors(t *testing.T) {
	repos := newMerchantRepository()
	now := time.Now().UTC()
	merchant := createWebhookMerchant(t, repos, now, false)
	service := services.NewWebhookService(repos.Merchants, repos.Webhooks, &captureNotifier{channel: ports.ChannelWebhook}, fixedClock{now: now}, nil, services.WebhookServiceOptions{})

	_, err := service.SendTest(context.Background(), merchant.ID)
	requireAppErrorCode(t, err, "validation_error")

	_, err = service.SendTest(context.Background(), uuid.New())
	requireAppErrorCode(t, err, "not_found")
}

func TestWebhookServiceSendTestDeliveryError(t *testing.T) {
	repos := newMerchantRepository()
	now := time.Now().UTC()
	merchant := createWebhookMerchant(t, repos, now, true)
	sendErr := errors.New("endpoint unreachable")
	service := services.NewWebhookService(repos.Merchants, repos.Webhooks, &captureNotifier{channel: ports.ChannelWebhook, err: sendErr}, fixedClock{now: now}, nil, services.WebhookServiceOptions{MaxRetries: 3})

	delivery, err := service.SendTest(context.Background(), merchant.ID)
	if !errors.Is(err, sendErr) {
		t.Fatalf("error = %v", err)
	}
	if delivery == nil || delivery.Status != "retrying" {
		t.Fatalf("delivery = %+v", delivery)
	}
}
