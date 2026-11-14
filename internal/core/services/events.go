package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
)

const EventsTopic = "payment.events"

type EventPublisher interface {
	PublishEvent(ctx context.Context, event *entities.Event) error
}

type QueueEventPublisher struct {
	queue ports.Queue
	topic string
}

func NewQueueEventPublisher(queue ports.Queue, topic string) *QueueEventPublisher {
	if topic == "" {
		topic = EventsTopic
	}
	return &QueueEventPublisher{queue: queue, topic: topic}
}

func (p *QueueEventPublisher) PublishEvent(ctx context.Context, event *entities.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if err := p.queue.Publish(ctx, p.topic, payload); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}
	return nil
}

type NopEventPublisher struct{}

func (NopEventPublisher) PublishEvent(context.Context, *entities.Event) error { return nil }
