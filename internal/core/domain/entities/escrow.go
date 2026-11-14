package entities

import (
	"fmt"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/google/uuid"
)

type EscrowStatus string

const (
	EscrowStatusPending  EscrowStatus = "pending"
	EscrowStatusFunded   EscrowStatus = "funded"
	EscrowStatusReleased EscrowStatus = "released"
	EscrowStatusRefunded EscrowStatus = "refunded"
	EscrowStatusCanceled EscrowStatus = "canceled"
	EscrowStatusExpired  EscrowStatus = "expired"
)

type Escrow struct {
	ID              uuid.UUID               `json:"id"`
	MerchantID      uuid.UUID               `json:"merchant_id"`
	PaymentIntentID uuid.UUID               `json:"payment_intent_id"`
	Amount          *types.Amount           `json:"amount"`
	CryptoAmount    *types.Amount           `json:"crypto_amount"`
	Currency        string                  `json:"currency"`
	CryptoCurrency  string                  `json:"crypto_currency"`
	Blockchain      types.BlockchainType    `json:"blockchain"`
	Network         types.BlockchainNetwork `json:"network"`
	Beneficiary     string                  `json:"beneficiary"`
	Arbiter         string                  `json:"arbiter,omitempty"`
	ReleaseAt       time.Time               `json:"release_at"`
	Status          EscrowStatus            `json:"status"`
	ReleasedAt      *time.Time              `json:"released_at,omitempty"`
	RefundedAt      *time.Time              `json:"refunded_at,omitempty"`
	TxHash          string                  `json:"tx_hash,omitempty"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

func NewEscrow(merchantID, paymentIntentID uuid.UUID, now time.Time) *Escrow {
	return &Escrow{
		ID:              uuid.New(),
		MerchantID:      merchantID,
		PaymentIntentID: paymentIntentID,
		Status:          EscrowStatusPending,
		CreatedAt:       now.UTC(),
		UpdatedAt:       now.UTC(),
	}
}

func (e *Escrow) Fund(now time.Time) error {
	if e.Status != EscrowStatusPending {
		return fmt.Errorf("%w: escrow %s is not pending", ErrInvalidState, e.ID)
	}
	e.Status = EscrowStatusFunded
	e.UpdatedAt = now.UTC()
	return nil
}

func (e *Escrow) CanRelease(now time.Time, force bool) bool {
	if e.Status != EscrowStatusFunded {
		return false
	}
	return force || e.ReleaseAt.IsZero() || !now.Before(e.ReleaseAt)
}

func (e *Escrow) Release(now time.Time, txHash string) error {
	if e.Status != EscrowStatusFunded {
		return fmt.Errorf("%w: escrow %s is not funded", ErrInvalidState, e.ID)
	}
	t := now.UTC()
	e.Status = EscrowStatusReleased
	e.ReleasedAt = &t
	e.TxHash = txHash
	e.UpdatedAt = t
	return nil
}

func (e *Escrow) Refund(now time.Time, txHash string) error {
	if e.Status != EscrowStatusFunded && e.Status != EscrowStatusPending {
		return fmt.Errorf("%w: escrow %s cannot be refunded from %s", ErrInvalidState, e.ID, e.Status)
	}
	t := now.UTC()
	e.Status = EscrowStatusRefunded
	e.RefundedAt = &t
	e.TxHash = txHash
	e.UpdatedAt = t
	return nil
}

func (e *Escrow) Cancel(now time.Time) error {
	if e.Status != EscrowStatusPending && e.Status != EscrowStatusFunded {
		return fmt.Errorf("%w: escrow %s cannot be canceled from %s", ErrInvalidState, e.ID, e.Status)
	}
	e.Status = EscrowStatusCanceled
	e.UpdatedAt = now.UTC()
	return nil
}

func (e *Escrow) Expire(now time.Time) error {
	if e.Status != EscrowStatusPending {
		return fmt.Errorf("%w: only pending escrows can expire", ErrInvalidState)
	}
	e.Status = EscrowStatusExpired
	e.UpdatedAt = now.UTC()
	return nil
}

func (e *Escrow) IsFinal() bool {
	switch e.Status {
	case EscrowStatusReleased, EscrowStatusRefunded, EscrowStatusCanceled, EscrowStatusExpired:
		return true
	default:
		return false
	}
}
