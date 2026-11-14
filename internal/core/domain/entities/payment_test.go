package entities

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPaymentIntentTransitions(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		from    PaymentIntentStatus
		to      PaymentIntentStatus
		wantErr bool
	}{
		{name: "requires payment to processing", from: PaymentStatusRequiresPayment, to: PaymentStatusProcessing},
		{name: "requires payment to succeeded", from: PaymentStatusRequiresPayment, to: PaymentStatusSucceeded},
		{name: "processing to confirmation", from: PaymentStatusProcessing, to: PaymentStatusRequiresConfirmation},
		{name: "confirmation to succeeded", from: PaymentStatusRequiresConfirmation, to: PaymentStatusSucceeded},
		{name: "succeeded to refunded", from: PaymentStatusSucceeded, to: PaymentStatusRefunded},
		{name: "requires payment to canceled", from: PaymentStatusRequiresPayment, to: PaymentStatusCanceled},
		{name: "succeeded cannot expire", from: PaymentStatusSucceeded, to: PaymentStatusExpired, wantErr: true},
		{name: "canceled is terminal", from: PaymentStatusCanceled, to: PaymentStatusProcessing, wantErr: true},
		{name: "expired is terminal", from: PaymentStatusExpired, to: PaymentStatusProcessing, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payment := NewPaymentIntent(uuid.New(), now)
			payment.Status = tt.from
			err := payment.Transition(tt.to, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error transitioning %s -> %s", tt.from, tt.to)
				}
				if payment.Status != tt.from {
					t.Fatalf("status changed on failed transition: %s", payment.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if payment.Status != tt.to {
				t.Fatalf("status = %s, want %s", payment.Status, tt.to)
			}
			if payment.IsFinal() && payment.CompletedAt == nil {
				t.Fatalf("final status %s must set CompletedAt", tt.to)
			}
		})
	}
}

func TestPaymentIntentIdempotentTransition(t *testing.T) {
	now := time.Now().UTC()
	payment := NewPaymentIntent(uuid.New(), now)
	if err := payment.Transition(PaymentStatusRequiresPayment, now); err != nil {
		t.Fatalf("same-status transition should be a no-op: %v", err)
	}
}

func TestApplyConfirmation(t *testing.T) {
	now := time.Now().UTC()
	payment := NewPaymentIntent(uuid.New(), now)
	payment.RequiredConfirmations = 6

	if err := payment.ApplyConfirmation("0xabc", 1, nil, nil, 6, now); err != nil {
		t.Fatalf("ApplyConfirmation pending: %v", err)
	}
	if payment.Status != PaymentStatusRequiresConfirmation {
		t.Fatalf("status = %s, want requires_confirmation", payment.Status)
	}

	if err := payment.ApplyConfirmation("0xabc", 6, nil, nil, 6, now); err != nil {
		t.Fatalf("ApplyConfirmation succeeded: %v", err)
	}
	if payment.Status != PaymentStatusSucceeded {
		t.Fatalf("status = %s, want succeeded", payment.Status)
	}
	if payment.TxHash != "0xabc" {
		t.Fatalf("tx hash = %s", payment.TxHash)
	}
}

func TestExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	payment := NewPaymentIntent(uuid.New(), now)
	payment.ExpiresAt = now.Add(time.Minute)
	if payment.IsExpired(now) {
		t.Fatal("payment should not be expired yet")
	}
	if !payment.IsExpired(now.Add(time.Minute)) {
		t.Fatal("payment should be expired")
	}
	if err := payment.Expire(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := payment.Expire(now.Add(time.Minute)); err != nil {
		t.Fatalf("re-expiring is a no-op, got %v", err)
	}
	if err := payment.Transition(PaymentStatusProcessing, now.Add(time.Minute)); err == nil {
		t.Fatal("expired payment must not transition to processing")
	}
}
