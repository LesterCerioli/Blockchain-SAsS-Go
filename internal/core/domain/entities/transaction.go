package entities

import (
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/google/uuid"
)

type BlockchainTransaction struct {
	ID              uuid.UUID                  `json:"id"`
	PaymentIntentID uuid.UUID                  `json:"payment_intent_id,omitempty"`
	MerchantID      uuid.UUID                  `json:"merchant_id,omitempty"`
	TxHash          string                     `json:"tx_hash"`
	Blockchain      types.BlockchainType       `json:"blockchain"`
	Network         types.BlockchainNetwork    `json:"network"`
	FromAddress     string                     `json:"from_address,omitempty"`
	ToAddress       string                     `json:"to_address,omitempty"`
	Amount          *types.Amount              `json:"amount"`
	Fee             *types.Amount              `json:"fee,omitempty"`
	Status          types.TransactionStatus    `json:"status"`
	Direction       types.TransactionDirection `json:"direction"`
	Confirmations   int                        `json:"confirmations"`
	BlockNumber     int64                      `json:"block_number,omitempty"`
	BlockHash       string                     `json:"block_hash,omitempty"`
	Metadata        map[string]string          `json:"metadata,omitempty"`
	CreatedAt       time.Time                  `json:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at"`
}

func NewBlockchainTransaction(now time.Time) *BlockchainTransaction {
	return &BlockchainTransaction{
		ID:        uuid.New(),
		Status:    types.StatusPending,
		Metadata:  map[string]string{},
		CreatedAt: now.UTC(),
		UpdatedAt: now.UTC(),
	}
}

func (t *BlockchainTransaction) UpdateConfirmation(status types.TransactionStatus, confirmations, blockNumber int64, blockHash string, now time.Time) {
	t.Status = status
	t.Confirmations = int(confirmations)
	t.BlockNumber = blockNumber
	t.BlockHash = blockHash
	t.UpdatedAt = now.UTC()
}
