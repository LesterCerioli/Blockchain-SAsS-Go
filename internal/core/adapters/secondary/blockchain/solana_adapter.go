package blockchain

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

type SolanaOptions struct {
	RPCURL                string
	DepositAddress        string
	Network               types.BlockchainNetwork
	RequiredConfirmations int
	Timeout               time.Duration
	FeeMultiplier         float64
}

type SolanaAdapter struct {
	client                *JSONRPCClient
	network               types.BlockchainNetwork
	depositAddress        string
	requiredConfirmations int
	feeMultiplier         float64
}

func NewSolanaAdapter(options SolanaOptions) *SolanaAdapter {
	if options.Network == "" {
		options.Network = types.NetworkDevnet
	}
	if options.RequiredConfirmations <= 0 {
		options.RequiredConfirmations = 1
	}
	if options.FeeMultiplier <= 0 {
		options.FeeMultiplier = 1.0
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &SolanaAdapter{
		client:                NewJSONRPCClient(options.RPCURL, "", "", timeout),
		network:               options.Network,
		depositAddress:        options.DepositAddress,
		requiredConfirmations: options.RequiredConfirmations,
		feeMultiplier:         options.FeeMultiplier,
	}
}

func (a *SolanaAdapter) Name() string { return "solana" }

func (a *SolanaAdapter) Type() types.BlockchainType { return types.BlockchainSolana }

func (a *SolanaAdapter) Network() types.BlockchainNetwork { return a.network }

func (a *SolanaAdapter) Info() types.BlockchainInfo {
	info := types.GetBlockchainInfo(types.BlockchainSolana, a.network)
	info.Name = "Solana"
	info.Symbol = "SOL"
	info.Decimals = 9
	info.RequiredConfirmations = a.requiredConfirmations
	info.IsTokenSupported = true
	info.IsSmartContractSupported = true
	info.ExplorerURL = "https://explorer.solana.com"
	return info
}

func (a *SolanaAdapter) Health(ctx context.Context) error {
	var result string
	return a.client.Call(ctx, "getHealth", []interface{}{}, &result)
}

func (a *SolanaAdapter) GetBalance(ctx context.Context, address string) (*types.Amount, error) {
	var result struct {
		Value uint64 `json:"value"`
	}
	if err := a.client.Call(ctx, "getBalance", []interface{}{address, map[string]string{"commitment": "confirmed"}}, &result); err != nil {
		return nil, err
	}
	return types.NewAmount(fmt.Sprintf("%d", result.Value), 9)
}

func (a *SolanaAdapter) GenerateAddress(_ context.Context, _ string) (string, error) {
	if a.depositAddress != "" {
		return a.depositAddress, nil
	}
	return "", fmt.Errorf("%w: SOLANA_DEPOSIT_ADDRESS is not configured", entities.ErrUnsupported)
}

func (a *SolanaAdapter) ValidateAddress(_ context.Context, address string) (bool, error) {
	if len(address) < 32 || len(address) > 44 {
		return false, nil
	}
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	for _, r := range address {
		if !strings.ContainsRune(alphabet, r) {
			return false, nil
		}
	}
	return true, nil
}

type solanaSignatureStatus struct {
	Slot               uint64          `json:"slot"`
	Confirmations      *uint64         `json:"confirmations"`
	Err                json.RawMessage `json:"err"`
	ConfirmationStatus string          `json:"confirmationStatus"`
}

type solanaTransactionMeta struct {
	Err          json.RawMessage `json:"err"`
	Fee          uint64          `json:"fee"`
	PreBalances  []uint64        `json:"preBalances"`
	PostBalances []uint64        `json:"postBalances"`
}

type solanaTransaction struct {
	Slot        uint64                 `json:"slot"`
	BlockTime   *int64                 `json:"blockTime"`
	Meta        *solanaTransactionMeta `json:"meta"`
	Transaction struct {
		Message struct {
			AccountKeys []string `json:"accountKeys"`
		} `json:"message"`
	} `json:"transaction"`
}

func (a *SolanaAdapter) GetTransactionStatus(ctx context.Context, txHash string) (*ports.TransactionReceipt, error) {
	var statusResult struct {
		Value []*solanaSignatureStatus `json:"value"`
	}
	if err := a.client.Call(ctx, "getSignatureStatuses", []interface{}{
		[]string{txHash}, map[string]interface{}{"searchTransactionHistory": true},
	}, &statusResult); err != nil {
		return nil, err
	}
	var status *solanaSignatureStatus
	if len(statusResult.Value) > 0 {
		status = statusResult.Value[0]
	}
	if status == nil {
		return &ports.TransactionReceipt{TxHash: txHash, Status: types.StatusPending}, nil
	}

	var tx *solanaTransaction
	if err := a.client.Call(ctx, "getTransaction", []interface{}{
		txHash, map[string]interface{}{"encoding": "json", "maxSupportedTransactionVersion": 0},
	}, &tx); err != nil {
		return nil, err
	}

	confirmations := 0
	if status.Confirmations != nil {
		confirmations = int(*status.Confirmations)
	}

	failed := status.Err != nil && string(status.Err) != "null"
	var amount *types.Amount
	var fee *types.Amount
	from, to := "", ""
	if tx != nil && tx.Meta != nil {
		if tx.Meta.Err != nil && string(tx.Meta.Err) != "null" {
			failed = true
		}
		fee = &types.Amount{Value: new(big.Int).SetUint64(tx.Meta.Fee), Decimals: 9}
		amount, from, to = a.balanceDelta(tx)
	}

	txStatus := types.StatusPending
	if failed {
		txStatus = types.StatusFailed
	} else if confirmations >= a.requiredConfirmations || status.ConfirmationStatus == "finalized" {
		txStatus = types.StatusConfirmed
	}

	receipt := &ports.TransactionReceipt{
		TxHash:        txHash,
		From:          from,
		To:            to,
		Amount:        amount,
		Fee:           fee,
		Status:        txStatus,
		Confirmations: confirmations,
		BlockNumber:   int64(status.Slot),
	}
	if tx != nil && tx.BlockTime != nil {
		receipt.Timestamp = time.Unix(*tx.BlockTime, 0).UTC()
	}
	return receipt, nil
}

func (a *SolanaAdapter) balanceDelta(tx *solanaTransaction) (*types.Amount, string, string) {
	if tx.Meta == nil || len(tx.Transaction.Message.AccountKeys) == 0 {
		return nil, "", ""
	}
	maxPositive := int64(0)
	fromIndex, toIndex := -1, -1
	for i, key := range tx.Transaction.Message.AccountKeys {
		if i >= len(tx.Meta.PreBalances) || i >= len(tx.Meta.PostBalances) {
			break
		}
		delta := int64(tx.Meta.PostBalances[i]) - int64(tx.Meta.PreBalances[i])
		if delta > maxPositive {
			maxPositive = delta
			toIndex = i
		}
		if delta < 0 && fromIndex == -1 {
			fromIndex = i
		}
		_ = key
	}
	if maxPositive == 0 || toIndex < 0 {
		return nil, "", ""
	}
	from := ""
	if fromIndex >= 0 {
		from = tx.Transaction.Message.AccountKeys[fromIndex]
	}
	return &types.Amount{Value: new(big.Int).SetUint64(uint64(maxPositive)), Decimals: 9}, from, tx.Transaction.Message.AccountKeys[toIndex]
}

type solanaSignature struct {
	Signature          string          `json:"signature"`
	Slot               uint64          `json:"slot"`
	Err                json.RawMessage `json:"err"`
	BlockTime          *int64          `json:"blockTime"`
	ConfirmationStatus string          `json:"confirmationStatus"`
}

func (a *SolanaAdapter) GetAddressTransactions(ctx context.Context, address string) ([]ports.IncomingTransaction, error) {
	var signatures []solanaSignature
	if err := a.client.Call(ctx, "getSignaturesForAddress", []interface{}{
		address, map[string]interface{}{"limit": 25},
	}, &signatures); err != nil {
		return nil, err
	}
	result := make([]ports.IncomingTransaction, 0)
	for _, signature := range signatures {
		receipt, err := a.GetTransactionStatus(ctx, signature.Signature)
		if err != nil {
			continue
		}
		if receipt.Amount == nil || receipt.Amount.IsZero() {
			continue
		}
		if !strings.EqualFold(receipt.To, address) {
			continue
		}
		result = append(result, ports.IncomingTransaction{
			TxHash:        receipt.TxHash,
			From:          receipt.From,
			To:            receipt.To,
			Amount:        receipt.Amount,
			Fee:           receipt.Fee,
			Status:        receipt.Status,
			Confirmations: receipt.Confirmations,
			BlockNumber:   receipt.BlockNumber,
			Timestamp:     receipt.Timestamp,
		})
	}
	return result, nil
}

func (a *SolanaAdapter) VerifyTransaction(ctx context.Context, txHash, expectedAddress string, expectedAmount *types.Amount) (bool, error) {
	receipt, err := a.GetTransactionStatus(ctx, txHash)
	if err != nil {
		return false, err
	}
	if receipt.Status == types.StatusFailed {
		return false, nil
	}
	if expectedAddress != "" && !strings.EqualFold(receipt.To, expectedAddress) {
		return false, nil
	}
	if expectedAmount != nil && receipt.Amount != nil && receipt.Amount.Cmp(expectedAmount) < 0 {
		return false, nil
	}
	return true, nil
}

func (a *SolanaAdapter) EstimateFee(_ context.Context, priority types.TransactionPriority) (*ports.FeeEstimate, error) {
	fee := &types.Amount{Value: new(big.Int).SetUint64(5000), Decimals: 9}
	return &ports.FeeEstimate{
		Fee:              multiplyAmount(fee, a.feeMultiplier),
		FeeRate:          "5000 lamports",
		EstimatedSeconds: 15,
		Priority:         string(priority),
	}, nil
}

func (a *SolanaAdapter) SendTransaction(ctx context.Context, req ports.SendTransactionRequest) (string, error) {
	if strings.TrimSpace(req.RawTransaction) == "" {
		return "", fmt.Errorf("%w: solana requires a signed base64 transaction", entities.ErrUnsupported)
	}
	var result string
	if err := a.client.Call(ctx, "sendTransaction", []interface{}{
		req.RawTransaction, map[string]interface{}{"encoding": "base64", "preflightCommitment": "confirmed"},
	}, &result); err != nil {
		return "", err
	}
	return result, nil
}

var _ ports.BlockchainPort = (*SolanaAdapter)(nil)
