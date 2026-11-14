package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/google/uuid"
)

type fakeQueue struct {
	topic   string
	payload []byte
	err     error
}

func (q *fakeQueue) Publish(_ context.Context, topic string, payload []byte) error {
	q.topic = topic
	q.payload = payload
	return q.err
}

func (q *fakeQueue) Subscribe(string, ports.EventHandler) error { return nil }

func (q *fakeQueue) Close() error { return nil }

func TestQueueEventPublisher(t *testing.T) {
	queue := &fakeQueue{}
	publisher := services.NewQueueEventPublisher(queue, "")
	event := entities.NewEvent(entities.EventPaymentIntentCreated, "payment_intent", uuid.New(), uuid.New(), time.Now().UTC(), map[string]interface{}{"reference": "order-1"})

	if err := publisher.PublishEvent(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queue.topic != services.EventsTopic {
		t.Fatalf("topic = %q, want %q", queue.topic, services.EventsTopic)
	}

	var decoded entities.Event
	if err := json.Unmarshal(queue.payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded.ID != event.ID || decoded.Type != entities.EventPaymentIntentCreated {
		t.Fatalf("decoded event = %+v", decoded)
	}
}

func TestQueueEventPublisherCustomTopicAndError(t *testing.T) {
	queue := &fakeQueue{err: errors.New("queue offline")}
	publisher := services.NewQueueEventPublisher(queue, "custom.topic")
	event := entities.NewEvent(entities.EventEscrowReleased, "escrow", uuid.New(), uuid.New(), time.Now().UTC(), nil)

	if err := publisher.PublishEvent(context.Background(), event); err == nil {
		t.Fatal("expected publish error")
	}
	if queue.topic != "custom.topic" {
		t.Fatalf("topic = %q, want custom.topic", queue.topic)
	}
}

func TestNopEventPublisher(t *testing.T) {
	event := entities.NewEvent(entities.EventWebhookTest, "merchant", uuid.New(), uuid.New(), time.Now().UTC(), nil)
	if err := (services.NopEventPublisher{}).PublishEvent(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
