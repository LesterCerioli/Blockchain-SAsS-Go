package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

type EthereumOptions struct {
	RPCURL                string
	DepositAddress        string
	Network               types.BlockchainNetwork
	RequiredConfirmations int
	LookbackBlocks        int
	Timeout               time.Duration
	FeeMultiplier         float64
}

type EthereumAdapter struct {
	client                *JSONRPCClient
	network               types.BlockchainNetwork
	depositAddress        string
	requiredConfirmations int
	lookbackBlocks        int
	feeMultiplier         float64
}

func NewEthereumAdapter(options EthereumOptions) *EthereumAdapter {
	if options.Network == "" {
		options.Network = types.NetworkEthereumSepolia
	}
	if options.RequiredConfirmations <= 0 {
		options.RequiredConfirmations = 12
	}
	if options.LookbackBlocks <= 0 {
		options.LookbackBlocks = 12
	}
	if options.FeeMultiplier <= 0 {
		options.FeeMultiplier = 1.0
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &EthereumAdapter{
		client:                NewJSONRPCClient(options.RPCURL, "", "", timeout),
		network:               options.Network,
		depositAddress:        options.DepositAddress,
		requiredConfirmations: options.RequiredConfirmations,
		lookbackBlocks:        options.LookbackBlocks,
		feeMultiplier:         options.FeeMultiplier,
	}
}

func (a *EthereumAdapter) Name() string { return "ethereum" }

func (a *EthereumAdapter) Type() types.BlockchainType { return types.BlockchainEthereum }

func (a *EthereumAdapter) Network() types.BlockchainNetwork { return a.network }

func (a *EthereumAdapter) Info() types.BlockchainInfo {
	info := types.GetBlockchainInfo(types.BlockchainEthereum, a.network)
	info.RequiredConfirmations = a.requiredConfirmations
	return info
}

func (a *EthereumAdapter) Health(ctx context.Context) error {
	var result string
	return a.client.Call(ctx, "eth_blockNumber", []interface{}{}, &result)
}

type ethReceipt struct {
	TransactionHash   string `json:"transactionHash"`
	BlockNumber       string `json:"blockNumber"`
	BlockHash         string `json:"blockHash"`
	From              string `json:"from"`
	To                string `json:"to"`
	Status            string `json:"status"`
	GasUsed           string `json:"gasUsed"`
	EffectiveGasPrice string `json:"effectiveGasPrice"`
}

type ethTransaction struct {
	Hash     string `json:"hash"`
	From     string `json:"from"`
	To       string `json:"to"`
	Value    string `json:"value"`
	Gas      string `json:"gas"`
	GasPrice string `json:"gasPrice"`
}

type ethBlock struct {
	Number       string           `json:"number"`
	Hash         string           `json:"hash"`
	Timestamp    string           `json:"timestamp"`
	Transactions []ethTransaction `json:"transactions"`
}

func (a *EthereumAdapter) GetBalance(ctx context.Context, address string) (*types.Amount, error) {
	var result string
	if err := a.client.Call(ctx, "eth_getBalance", []interface{}{address, "latest"}, &result); err != nil {
		return nil, err
	}
	value, err := parseHexBigInt(result)
	if err != nil {
		return nil, err
	}
	return &types.Amount{Value: value, Decimals: 18}, nil
}

func (a *EthereumAdapter) GenerateAddress(_ context.Context, _ string) (string, error) {
	if a.depositAddress != "" {
		return a.depositAddress, nil
	}
	return "", fmt.Errorf("%w: ETHEREUM_DEPOSIT_ADDRESS is not configured", entities.ErrUnsupported)
}

func (a *EthereumAdapter) ValidateAddress(_ context.Context, address string) (bool, error) {
	return types.BlockchainEthereum.ValidateAddress(address, a.network), nil
}

func (a *EthereumAdapter) latestBlockNumber(ctx context.Context) (int64, error) {
	var result string
	if err := a.client.Call(ctx, "eth_blockNumber", []interface{}{}, &result); err != nil {
		return 0, err
	}
	return parseHexInt64(result)
}

func (a *EthereumAdapter) GetTransactionStatus(ctx context.Context, txHash string) (*ports.TransactionReceipt, error) {
	var receipt *ethReceipt
	if err := a.client.Call(ctx, "eth_getTransactionReceipt", []interface{}{txHash}, &receipt); err != nil {
		return nil, err
	}
	if receipt == nil {
		return &ports.TransactionReceipt{TxHash: txHash, Status: types.StatusPending}, nil
	}

	var tx ethTransaction
	_ = a.client.Call(ctx, "eth_getTransactionByHash", []interface{}{txHash}, &tx)

	blockNumber, _ := parseHexInt64(receipt.BlockNumber)
	latest, _ := a.latestBlockNumber(ctx)
	confirmations := int64(0)
	if latest >= blockNumber && blockNumber > 0 {
		confirmations = latest - blockNumber + 1
	}

	value, _ := parseHexBigInt(tx.Value)
	gasUsed, _ := parseHexBigInt(receipt.GasUsed)
	gasPrice, _ := parseHexBigInt(receipt.EffectiveGasPrice)

	status := types.StatusPending
	if receipt.Status == "0x0" {
		status = types.StatusFailed
	} else if confirmations >= int64(a.requiredConfirmations) {
		status = types.StatusConfirmed
	}

	return &ports.TransactionReceipt{
		TxHash:        txHash,
		From:          receipt.From,
		To:            receipt.To,
		Amount:        &types.Amount{Value: value, Decimals: 18},
		Fee:           mergeFee(gasUsed, gasPrice),
		Status:        status,
		Confirmations: int(confirmations),
		BlockNumber:   blockNumber,
		BlockHash:     receipt.BlockHash,
	}, nil
}

func (a *EthereumAdapter) GetAddressTransactions(ctx context.Context, address string) ([]ports.IncomingTransaction, error) {
	latest, err := a.latestBlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	start := latest - int64(a.lookbackBlocks) + 1
	if start < 0 {
		start = 0
	}

	result := make([]ports.IncomingTransaction, 0)
	for blockNumber := start; blockNumber <= latest; blockNumber++ {
		var block *ethBlock
		if err := a.client.Call(ctx, "eth_getBlockByNumber", []interface{}{formatHexInt64(blockNumber), true}, &block); err != nil {
			return nil, err
		}
		if block == nil {
			continue
		}
		for _, tx := range block.Transactions {
			if !strings.EqualFold(tx.To, address) {
				continue
			}
			value, _ := parseHexBigInt(tx.Value)
			if value.Sign() <= 0 {
				continue
			}
			result = append(result, ports.IncomingTransaction{
				TxHash:        tx.Hash,
				From:          tx.From,
				To:            tx.To,
				Amount:        &types.Amount{Value: value, Decimals: 18},
				Status:        types.StatusConfirmed,
				Confirmations: int(latest - blockNumber + 1),
				BlockNumber:   blockNumber,
				BlockHash:     block.Hash,
			})
		}
	}
	return result, nil
}

func (a *EthereumAdapter) VerifyTransaction(ctx context.Context, txHash, expectedAddress string, expectedAmount *types.Amount) (bool, error) {
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

func (a *EthereumAdapter) EstimateFee(ctx context.Context, priority types.TransactionPriority) (*ports.FeeEstimate, error) {
	var gasPriceHex string
	if err := a.client.Call(ctx, "eth_gasPrice", []interface{}{}, &gasPriceHex); err != nil {
		return nil, err
	}
	gasPrice, err := parseHexBigInt(gasPriceHex)
	if err != nil {
		return nil, err
	}
	gasLimit := big.NewInt(21000)
	if priority == types.PriorityHigh {
		gasLimit = big.NewInt(60000)
	}
	fee := new(big.Int).Mul(gasPrice, gasLimit)
	amount := &types.Amount{Value: fee, Decimals: 18}
	return &ports.FeeEstimate{
		Fee:              multiplyAmount(amount, a.feeMultiplier),
		FeeRate:          gasPriceHex,
		EstimatedSeconds: 30,
		Priority:         string(priority),
	}, nil
}

func (a *EthereumAdapter) SendTransaction(ctx context.Context, req ports.SendTransactionRequest) (string, error) {
	if strings.TrimSpace(req.RawTransaction) == "" {
		return "", fmt.Errorf("%w: ethereum requires a signed raw transaction", entities.ErrUnsupported)
	}
	var result string
	if err := a.client.Call(ctx, "eth_sendRawTransaction", []interface{}{req.RawTransaction}, &result); err != nil {
		return "", err
	}
	return result, nil
}

var _ ports.BlockchainPort = (*EthereumAdapter)(nil)
