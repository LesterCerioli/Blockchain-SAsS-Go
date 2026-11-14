package blockchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

type MockAdapter struct {
	chain                 types.BlockchainType
	network               types.BlockchainNetwork
	requiredConfirmations int
}

func NewMockAdapter(chain types.BlockchainType, network types.BlockchainNetwork, requiredConfirmations int) *MockAdapter {
	if requiredConfirmations <= 0 {
		requiredConfirmations = 1
	}
	return &MockAdapter{chain: chain, network: network, requiredConfirmations: requiredConfirmations}
}

func (a *MockAdapter) Name() string { return "mock-" + string(a.chain) }

func (a *MockAdapter) Type() types.BlockchainType { return a.chain }

func (a *MockAdapter) Network() types.BlockchainNetwork { return a.network }

func (a *MockAdapter) Info() types.BlockchainInfo {
	info := types.GetBlockchainInfo(a.chain, a.network)
	info.RPCURL = "mock://" + string(a.chain)
	info.RequiredConfirmations = a.requiredConfirmations
	return info
}

func (a *MockAdapter) Health(context.Context) error { return nil }

func (a *MockAdapter) GetBalance(_ context.Context, address string) (*types.Amount, error) {
	decimals := a.Info().Decimals
	return types.NewAmount("1000000000000000000", decimals)
}

func (a *MockAdapter) GenerateAddress(_ context.Context, reference string) (string, error) {
	sum := sha256.Sum256([]byte(string(a.chain) + "|" + string(a.network) + "|" + reference))
	encoded := hex.EncodeToString(sum[:])
	switch a.chain {
	case types.BlockchainEthereum:
		return "0x" + encoded[:40], nil
	case types.BlockchainBitcoin:
		prefix := "tb1"
		if a.network == types.NetworkBitcoinMainnet {
			prefix = "bc1"
		}
		return prefix + encoded[:38], nil
	case types.BlockchainSolana:
		return "M" + encoded[:39], nil
	default:
		return "addr_test1" + encoded[:50], nil
	}
}

func (a *MockAdapter) ValidateAddress(_ context.Context, address string) (bool, error) {
	return len(address) > 0, nil
}

func (a *MockAdapter) GetTransactionStatus(_ context.Context, txHash string) (*ports.TransactionReceipt, error) {
	return &ports.TransactionReceipt{
		TxHash:        txHash,
		From:          "mock-sender",
		To:            "mock-recipient",
		Amount:        &types.Amount{Value: new(big.Int).SetInt64(1000000), Decimals: a.Info().Decimals},
		Fee:           &types.Amount{Value: new(big.Int).SetInt64(1000), Decimals: a.Info().Decimals},
		Status:        types.StatusConfirmed,
		Confirmations: a.requiredConfirmations,
		BlockNumber:   1,
		BlockHash:     "mock-block",
		Timestamp:     time.Now().UTC(),
	}, nil
}

func (a *MockAdapter) GetAddressTransactions(context.Context, string) ([]ports.IncomingTransaction, error) {
	return []ports.IncomingTransaction{}, nil
}

func (a *MockAdapter) VerifyTransaction(context.Context, string, string, *types.Amount) (bool, error) {
	return true, nil
}

func (a *MockAdapter) EstimateFee(_ context.Context, priority types.TransactionPriority) (*ports.FeeEstimate, error) {
	return &ports.FeeEstimate{
		Fee:              &types.Amount{Value: new(big.Int).SetInt64(1000), Decimals: a.Info().Decimals},
		FeeRate:          "mock",
		EstimatedSeconds: 1,
		Priority:         string(priority),
	}, nil
}

func (a *MockAdapter) SendTransaction(_ context.Context, req ports.SendTransactionRequest) (string, error) {
	if strings.TrimSpace(req.RawTransaction) == "" && req.To == "" {
		return "", fmt.Errorf("mock send requires raw transaction or destination")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", req.RawTransaction, req.To, req.Reference)))
	return "mock-" + hex.EncodeToString(sum[:16]), nil
}

var _ ports.BlockchainPort = (*MockAdapter)(nil)
