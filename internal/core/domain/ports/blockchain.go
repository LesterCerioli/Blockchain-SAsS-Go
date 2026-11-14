package ports

import (
	"context"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

type TransactionReceipt struct {
	TxHash        string                  `json:"tx_hash"`
	From          string                  `json:"from"`
	To            string                  `json:"to"`
	Amount        *types.Amount           `json:"amount"`
	Fee           *types.Amount           `json:"fee,omitempty"`
	Status        types.TransactionStatus `json:"status"`
	Confirmations int                     `json:"confirmations"`
	BlockNumber   int64                   `json:"block_number"`
	BlockHash     string                  `json:"block_hash"`
	Timestamp     time.Time               `json:"timestamp"`
	Raw           map[string]string       `json:"raw,omitempty"`
}

type IncomingTransaction struct {
	Blockchain    types.BlockchainType    `json:"blockchain"`
	Network       types.BlockchainNetwork `json:"network"`
	TxHash        string                  `json:"tx_hash"`
	From          string                  `json:"from"`
	To            string                  `json:"to"`
	Amount        *types.Amount           `json:"amount"`
	Fee           *types.Amount           `json:"fee,omitempty"`
	Status        types.TransactionStatus `json:"status"`
	Confirmations int                     `json:"confirmations"`
	BlockNumber   int64                   `json:"block_number"`
	BlockHash     string                  `json:"block_hash"`
	Timestamp     time.Time               `json:"timestamp"`
}

type FeeEstimate struct {
	Fee              *types.Amount `json:"fee"`
	FeeRate          string        `json:"fee_rate"`
	EstimatedSeconds int           `json:"estimated_seconds"`
	Priority         string        `json:"priority"`
}

type SendTransactionRequest struct {
	RawTransaction string
	From           string
	To             string
	Amount         *types.Amount
	Priority       types.TransactionPriority
	Reference      string
}

type BlockchainPort interface {
	Name() string
	Type() types.BlockchainType
	Network() types.BlockchainNetwork
	Info() types.BlockchainInfo
	Health(ctx context.Context) error
	GetBalance(ctx context.Context, address string) (*types.Amount, error)
	GenerateAddress(ctx context.Context, reference string) (string, error)
	ValidateAddress(ctx context.Context, address string) (bool, error)
	GetTransactionStatus(ctx context.Context, txHash string) (*TransactionReceipt, error)
	GetAddressTransactions(ctx context.Context, address string) ([]IncomingTransaction, error)
	VerifyTransaction(ctx context.Context, txHash, expectedAddress string, expectedAmount *types.Amount) (bool, error)
	EstimateFee(ctx context.Context, priority types.TransactionPriority) (*FeeEstimate, error)
	SendTransaction(ctx context.Context, req SendTransactionRequest) (string, error)
}

type BlockchainRegistry interface {
	Get(blockchain types.BlockchainType, network types.BlockchainNetwork) (BlockchainPort, error)
	Supported() []types.BlockchainInfo
	Health(ctx context.Context) map[string]error
}
