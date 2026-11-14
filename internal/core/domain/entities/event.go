package entities

import (
	"time"

	"github.com/google/uuid"
)

type EventType string

const (
	EventPaymentIntentCreated   EventType = "payment_intent.created"
	EventPaymentIntentUpdated   EventType = "payment_intent.updated"
	EventPaymentIntentSucceeded EventType = "payment_intent.succeeded"
	EventPaymentIntentFailed    EventType = "payment_intent.failed"
	EventPaymentIntentExpired   EventType = "payment_intent.expired"
	EventPaymentIntentCanceled  EventType = "payment_intent.canceled"
	EventPaymentIntentRefunded  EventType = "payment_intent.refunded"
	EventEscrowCreated          EventType = "escrow.created"
	EventEscrowReleased         EventType = "escrow.released"
	EventEscrowRefunded         EventType = "escrow.refunded"
	EventEscrowCanceled         EventType = "escrow.canceled"
	EventWebhookTest            EventType = "webhook.test"
)

type Event struct {
	ID            uuid.UUID              `json:"id"`
	Type          EventType              `json:"type"`
	AggregateType string                 `json:"aggregate_type"`
	AggregateID   uuid.UUID              `json:"aggregate_id"`
	MerchantID    uuid.UUID              `json:"merchant_id"`
	OccurredAt    time.Time              `json:"occurred_at"`
	Data          map[string]interface{} `json:"data"`
}

func NewEvent(eventType EventType, aggregateType string, aggregateID, merchantID uuid.UUID, now time.Time, data map[string]interface{}) *Event {
	if data == nil {
		data = map[string]interface{}{}
	}
	return &Event{
		ID:            uuid.New(),
		Type:          eventType,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		MerchantID:    merchantID,
		OccurredAt:    now.UTC(),
		Data:          data,
	}
}
