package entities

import (
	"fmt"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/google/uuid"
)

type PaymentIntentStatus string

const (
	PaymentStatusRequiresPayment      PaymentIntentStatus = "requires_payment"
	PaymentStatusUnderReview          PaymentIntentStatus = "under_review"
	PaymentStatusProcessing           PaymentIntentStatus = "processing"
	PaymentStatusRequiresConfirmation PaymentIntentStatus = "requires_confirmation"
	PaymentStatusSucceeded            PaymentIntentStatus = "succeeded"
	PaymentStatusFailed               PaymentIntentStatus = "failed"
	PaymentStatusExpired              PaymentIntentStatus = "expired"
	PaymentStatusCanceled             PaymentIntentStatus = "canceled"
	PaymentStatusRefunded             PaymentIntentStatus = "refunded"
)

type PaymentIntent struct {
	ID                    uuid.UUID               `json:"id"`
	MerchantID            uuid.UUID               `json:"merchant_id"`
	Reference             string                  `json:"reference,omitempty"`
	Description           string                  `json:"description,omitempty"`
	Amount                *types.Amount           `json:"amount"`
	Currency              string                  `json:"currency"`
	CryptoAmount          *types.Amount           `json:"crypto_amount"`
	CryptoCurrency        string                  `json:"crypto_currency"`
	Blockchain            types.BlockchainType    `json:"blockchain"`
	Network               types.BlockchainNetwork `json:"network"`
	DepositAddress        string                  `json:"deposit_address"`
	Status                PaymentIntentStatus     `json:"status"`
	ExpiresAt             time.Time               `json:"expires_at"`
	TxHash                string                  `json:"tx_hash,omitempty"`
	Confirmations         int                     `json:"confirmations"`
	RequiredConfirmations int                     `json:"required_confirmations"`
	PaidAmount            *types.Amount           `json:"paid_amount,omitempty"`
	FeeAmount             *types.Amount           `json:"fee_amount,omitempty"`
	RefundTxHash          string                  `json:"refund_tx_hash,omitempty"`
	Metadata              map[string]string       `json:"metadata,omitempty"`
	RiskScore             int                     `json:"risk_score"`
	IdempotencyKey        string                  `json:"-"`
	CreatedAt             time.Time               `json:"created_at"`
	UpdatedAt             time.Time               `json:"updated_at"`
	CompletedAt           *time.Time              `json:"completed_at,omitempty"`
}

func NewPaymentIntent(merchantID uuid.UUID, now time.Time) *PaymentIntent {
	return &PaymentIntent{
		ID:         uuid.New(),
		MerchantID: merchantID,
		Status:     PaymentStatusRequiresPayment,
		Metadata:   map[string]string{},
		CreatedAt:  now.UTC(),
		UpdatedAt:  now.UTC(),
	}
}

func (p *PaymentIntent) IsExpired(now time.Time) bool {
	return !p.ExpiresAt.IsZero() && !now.Before(p.ExpiresAt)
}

func (p *PaymentIntent) IsFinal() bool {
	switch p.Status {
	case PaymentStatusSucceeded, PaymentStatusFailed, PaymentStatusExpired,
		PaymentStatusCanceled, PaymentStatusRefunded:
		return true
	default:
		return false
	}
}

func (p *PaymentIntent) CanAcceptPayment() bool {
	switch p.Status {
	case PaymentStatusRequiresPayment, PaymentStatusUnderReview,
		PaymentStatusProcessing, PaymentStatusRequiresConfirmation:
		return true
	default:
		return false
	}
}

func (p *PaymentIntent) Transition(to PaymentIntentStatus, now time.Time) error {
	if p.Status == to {
		p.UpdatedAt = now.UTC()
		return nil
	}
	if !isAllowedPaymentTransition(p.Status, to) {
		return fmt.Errorf("%w: payment intent %s cannot transition from %s to %s",
			ErrInvalidState, p.ID, p.Status, to)
	}
	p.Status = to
	p.UpdatedAt = now.UTC()
	if to == PaymentStatusSucceeded || to == PaymentStatusFailed ||
		to == PaymentStatusCanceled || to == PaymentStatusRefunded {
		t := now.UTC()
		p.CompletedAt = &t
	}
	return nil
}

func isAllowedPaymentTransition(from, to PaymentIntentStatus) bool {
	allowed := map[PaymentIntentStatus]map[PaymentIntentStatus]bool{
		PaymentStatusRequiresPayment: {
			PaymentStatusUnderReview:          true,
			PaymentStatusProcessing:           true,
			PaymentStatusRequiresConfirmation: true,
			PaymentStatusSucceeded:            true,
			PaymentStatusExpired:              true,
			PaymentStatusCanceled:             true,
			PaymentStatusFailed:               true,
		},
		PaymentStatusUnderReview: {
			PaymentStatusRequiresPayment: true,
			PaymentStatusProcessing:      true,
			PaymentStatusExpired:         true,
			PaymentStatusCanceled:        true,
			PaymentStatusFailed:          true,
		},
		PaymentStatusProcessing: {
			PaymentStatusRequiresConfirmation: true,
			PaymentStatusSucceeded:            true,
			PaymentStatusFailed:               true,
			PaymentStatusExpired:              true,
			PaymentStatusCanceled:             true,
		},
		PaymentStatusRequiresConfirmation: {
			PaymentStatusProcessing: true,
			PaymentStatusSucceeded:  true,
			PaymentStatusFailed:     true,
			PaymentStatusExpired:    true,
		},
		PaymentStatusSucceeded: {
			PaymentStatusRefunded: true,
		},
		PaymentStatusFailed: {
			PaymentStatusProcessing: true,
		},
	}
	return allowed[from][to]
}

func (p *PaymentIntent) MarkUnderReview(score int, now time.Time) error {
	p.RiskScore = score
	return p.Transition(PaymentStatusUnderReview, now)
}

func (p *PaymentIntent) MarkProcessing(txHash string, now time.Time) error {
	if txHash == "" {
		return fmt.Errorf("%w: transaction hash is required", ErrInvalidState)
	}
	p.TxHash = txHash
	return p.Transition(PaymentStatusProcessing, now)
}

func (p *PaymentIntent) ApplyConfirmation(txHash string, confirmations int, paid, fee *types.Amount, required int, now time.Time) error {
	if txHash != "" {
		p.TxHash = txHash
	}
	if required > 0 {
		p.RequiredConfirmations = required
	}
	p.Confirmations = confirmations
	if paid != nil {
		p.PaidAmount = paid
	}
	if fee != nil {
		p.FeeAmount = fee
	}

	if confirmations >= p.RequiredConfirmations && p.RequiredConfirmations > 0 {
		return p.Transition(PaymentStatusSucceeded, now)
	}
	if confirmations > 0 {
		return p.Transition(PaymentStatusRequiresConfirmation, now)
	}
	return p.Transition(PaymentStatusProcessing, now)
}

func (p *PaymentIntent) MarkSucceeded(now time.Time) error {
	return p.Transition(PaymentStatusSucceeded, now)
}

func (p *PaymentIntent) MarkFailed(now time.Time) error {
	return p.Transition(PaymentStatusFailed, now)
}

func (p *PaymentIntent) Expire(now time.Time) error {
	return p.Transition(PaymentStatusExpired, now)
}

func (p *PaymentIntent) Cancel(now time.Time) error {
	return p.Transition(PaymentStatusCanceled, now)
}

func (p *PaymentIntent) MarkRefunded(txHash string, now time.Time) error {
	if p.Status == PaymentStatusRefunded {
		p.RefundTxHash = txHash
		return nil
	}
	if err := p.Transition(PaymentStatusRefunded, now); err != nil {
		return err
	}
	p.RefundTxHash = txHash
	return nil
}
