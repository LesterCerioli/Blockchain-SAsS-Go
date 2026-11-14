package blockchain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

type BitcoinOptions struct {
	RPCURL                string
	RPCUser               string
	RPCPassword           string
	DepositAddress        string
	Network               types.BlockchainNetwork
	RequiredConfirmations int
	Timeout               time.Duration
	FeeMultiplier         float64
}

type BitcoinAdapter struct {
	client                *JSONRPCClient
	network               types.BlockchainNetwork
	depositAddress        string
	requiredConfirmations int
	feeMultiplier         float64
}

func NewBitcoinAdapter(options BitcoinOptions) *BitcoinAdapter {
	if options.Network == "" {
		options.Network = types.NetworkBitcoinTestnet
	}
	if options.RequiredConfirmations <= 0 {
		options.RequiredConfirmations = 3
	}
	if options.FeeMultiplier <= 0 {
		options.FeeMultiplier = 1.0
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &BitcoinAdapter{
		client:                NewJSONRPCClient(options.RPCURL, options.RPCUser, options.RPCPassword, timeout),
		network:               options.Network,
		depositAddress:        options.DepositAddress,
		requiredConfirmations: options.RequiredConfirmations,
		feeMultiplier:         options.FeeMultiplier,
	}
}

func (a *BitcoinAdapter) Name() string { return "bitcoin" }

func (a *BitcoinAdapter) Type() types.BlockchainType { return types.BlockchainBitcoin }

func (a *BitcoinAdapter) Network() types.BlockchainNetwork { return a.network }

func (a *BitcoinAdapter) Info() types.BlockchainInfo {
	info := types.GetBitcoinNetworkInfo(a.network)
	info.RequiredConfirmations = a.requiredConfirmations
	return info
}

func (a *BitcoinAdapter) Health(ctx context.Context) error {
	var result map[string]interface{}
	return a.client.Call(ctx, "getblockchaininfo", []interface{}{}, &result)
}

func (a *BitcoinAdapter) GetBalance(ctx context.Context, _ string) (*types.Amount, error) {
	var balance float64
	if err := a.client.Call(ctx, "getbalance", []interface{}{}, &balance); err != nil {
		return nil, err
	}
	return amountFromFloat(balance, 8), nil
}

func (a *BitcoinAdapter) GenerateAddress(ctx context.Context, reference string) (string, error) {
	if a.depositAddress != "" {
		return a.depositAddress, nil
	}
	label := reference
	if label == "" {
		label = fmt.Sprintf("payment-%d", time.Now().UnixNano())
	}
	var address string
	if err := a.client.Call(ctx, "getnewaddress", []interface{}{label, "bech32"}, &address); err != nil {
		return "", err
	}
	return address, nil
}

func (a *BitcoinAdapter) ValidateAddress(ctx context.Context, address string) (bool, error) {
	var result struct {
		IsValid bool `json:"isvalid"`
	}
	if err := a.client.Call(ctx, "validateaddress", []interface{}{address}, &result); err != nil {
		return types.ValidateBitcoinAddress(address, a.network), nil
	}
	return result.IsValid, nil
}

type bitcoinTxDetail struct {
	Address  string  `json:"address"`
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
	Fee      float64 `json:"fee"`
}

type bitcoinTx struct {
	TxID          string            `json:"txid"`
	Amount        float64           `json:"amount"`
	Fee           float64           `json:"fee"`
	Confirmations int               `json:"confirmations"`
	BlockHash     string            `json:"blockhash"`
	BlockIndex    int64             `json:"blockindex"`
	BlockTime     int64             `json:"blocktime"`
	Time          int64             `json:"time"`
	Details       []bitcoinTxDetail `json:"details"`
}

func (a *BitcoinAdapter) GetTransactionStatus(ctx context.Context, txHash string) (*ports.TransactionReceipt, error) {
	var tx bitcoinTx
	if err := a.client.Call(ctx, "gettransaction", []interface{}{txHash, true}, &tx); err != nil {
		if rpcErr, ok := err.(*RPCError); ok && rpcErr.Code == -5 {
			return &ports.TransactionReceipt{TxHash: txHash, Status: types.StatusPending}, nil
		}
		return nil, err
	}

	status := types.StatusPending
	if tx.Confirmations >= a.requiredConfirmations {
		status = types.StatusConfirmed
	}
	amount := amountFromFloat(tx.Amount, 8)
	fee := amountFromFloat(tx.Fee, 8)
	to := ""
	from := ""
	for _, detail := range tx.Details {
		if detail.Category == "receive" && to == "" {
			to = detail.Address
		}
		if detail.Category == "send" && from == "" {
			from = detail.Address
		}
	}
	timestamp := time.Unix(tx.Time, 0).UTC()
	if tx.BlockTime > 0 {
		timestamp = time.Unix(tx.BlockTime, 0).UTC()
	}

	return &ports.TransactionReceipt{
		TxHash:        tx.TxID,
		From:          from,
		To:            to,
		Amount:        amount,
		Fee:           fee,
		Status:        status,
		Confirmations: tx.Confirmations,
		BlockNumber:   tx.BlockIndex,
		BlockHash:     tx.BlockHash,
		Timestamp:     timestamp,
	}, nil
}

func (a *BitcoinAdapter) GetAddressTransactions(ctx context.Context, address string) ([]ports.IncomingTransaction, error) {
	var transactions []bitcoinTx
	if err := a.client.Call(ctx, "listtransactions", []interface{}{"*", 100, 0, true}, &transactions); err != nil {
		return nil, err
	}
	result := make([]ports.IncomingTransaction, 0)
	for _, tx := range transactions {
		for _, detail := range tx.Details {
			if detail.Category != "receive" || !strings.EqualFold(detail.Address, address) {
				continue
			}
			status := types.StatusPending
			if tx.Confirmations >= a.requiredConfirmations {
				status = types.StatusConfirmed
			}
			result = append(result, ports.IncomingTransaction{
				TxHash:        tx.TxID,
				To:            detail.Address,
				Amount:        amountFromFloat(detail.Amount, 8),
				Status:        status,
				Confirmations: tx.Confirmations,
				BlockNumber:   tx.BlockIndex,
				BlockHash:     tx.BlockHash,
				Timestamp:     time.Unix(tx.Time, 0).UTC(),
			})
		}
	}
	return result, nil
}

func (a *BitcoinAdapter) VerifyTransaction(ctx context.Context, txHash, expectedAddress string, expectedAmount *types.Amount) (bool, error) {
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

func (a *BitcoinAdapter) EstimateFee(ctx context.Context, priority types.TransactionPriority) (*ports.FeeEstimate, error) {
	target := 6
	if priority == types.PriorityHigh {
		target = 1
	} else if priority == types.PriorityLow {
		target = 12
	}
	var result struct {
		FeeRate float64  `json:"feerate"`
		Errors  []string `json:"errors"`
	}
	if err := a.client.Call(ctx, "estimatesmartfee", []interface{}{target}, &result); err != nil {
		return nil, err
	}
	feeRate := result.FeeRate
	if feeRate <= 0 {
		feeRate = 0.00001
	}
	feeBTC := (feeRate / 1000) * 250
	fee := amountFromFloat(feeBTC, 8)
	return &ports.FeeEstimate{
		Fee:              multiplyAmount(fee, a.feeMultiplier),
		FeeRate:          fmt.Sprintf("%.2f sat/vB", feeRate*100000),
		EstimatedSeconds: target * 600,
		Priority:         string(priority),
	}, nil
}

func (a *BitcoinAdapter) SendTransaction(ctx context.Context, req ports.SendTransactionRequest) (string, error) {
	if strings.TrimSpace(req.RawTransaction) != "" {
		var result string
		if err := a.client.Call(ctx, "sendrawtransaction", []interface{}{req.RawTransaction}, &result); err != nil {
			return "", err
		}
		return result, nil
	}
	if req.To == "" || req.Amount == nil {
		return "", fmt.Errorf("%w: bitcoin requires raw transaction or destination and amount", entities.ErrInvalidAmount)
	}
	amount := req.Amount.ToDecimalString()
	var result string
	if err := a.client.Call(ctx, "sendtoaddress", []interface{}{req.To, amount}, &result); err != nil {
		return "", err
	}
	return result, nil
}

var _ ports.BlockchainPort = (*BitcoinAdapter)(nil)
