package blockchain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

type CardanoOptions struct {
	APIURL                string
	APIKey                string
	DepositAddress        string
	Network               types.BlockchainNetwork
	RequiredConfirmations int
	Timeout               time.Duration
	FeeMultiplier         float64
}

type CardanoAdapter struct {
	baseURL               string
	apiKey                string
	httpClient            *http.Client
	network               types.BlockchainNetwork
	depositAddress        string
	requiredConfirmations int
	feeMultiplier         float64
}

func NewCardanoAdapter(options CardanoOptions) *CardanoAdapter {
	if options.Network == "" {
		options.Network = types.NetworkTestnet
	}
	if options.RequiredConfirmations <= 0 {
		options.RequiredConfirmations = 15
	}
	if options.FeeMultiplier <= 0 {
		options.FeeMultiplier = 1.0
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &CardanoAdapter{
		baseURL:               strings.TrimRight(options.APIURL, "/"),
		apiKey:                options.APIKey,
		httpClient:            &http.Client{Timeout: timeout},
		network:               options.Network,
		depositAddress:        options.DepositAddress,
		requiredConfirmations: options.RequiredConfirmations,
		feeMultiplier:         options.FeeMultiplier,
	}
}

func (a *CardanoAdapter) Name() string { return "cardano" }

func (a *CardanoAdapter) Type() types.BlockchainType { return types.BlockchainCardano }

func (a *CardanoAdapter) Network() types.BlockchainNetwork { return a.network }

func (a *CardanoAdapter) Info() types.BlockchainInfo {
	info := types.GetBlockchainInfo(types.BlockchainCardano, a.network)
	info.Name = "Cardano"
	info.Symbol = "ADA"
	info.Decimals = 6
	info.RequiredConfirmations = a.requiredConfirmations
	info.IsTokenSupported = true
	info.IsSmartContractSupported = true
	info.ExplorerURL = "https://cardanoscan.io"
	return info
}

func (a *CardanoAdapter) doJSON(ctx context.Context, method, path string, body []byte, out interface{}) error {
	url := a.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("project_id", a.apiKey)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("cardano api %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(payload, out)
}

func (a *CardanoAdapter) Health(ctx context.Context) error {
	var result map[string]interface{}
	return a.doJSON(ctx, http.MethodGet, "/health", nil, &result)
}

type cardanoAmount struct {
	Unit     string `json:"unit"`
	Quantity string `json:"quantity"`
}

type cardanoAddressInfo struct {
	Amount []cardanoAmount `json:"amount"`
}

func lovelaceFromAmounts(amounts []cardanoAmount) *big.Int {
	total := new(big.Int)
	for _, amount := range amounts {
		if amount.Unit != "lovelace" {
			continue
		}
		value, ok := new(big.Int).SetString(amount.Quantity, 10)
		if ok {
			total.Add(total, value)
		}
	}
	return total
}

func (a *CardanoAdapter) GetBalance(ctx context.Context, address string) (*types.Amount, error) {
	var info cardanoAddressInfo
	if err := a.doJSON(ctx, http.MethodGet, "/addresses/"+address, nil, &info); err != nil {
		return nil, err
	}
	return &types.Amount{Value: lovelaceFromAmounts(info.Amount), Decimals: 6}, nil
}

func (a *CardanoAdapter) GenerateAddress(_ context.Context, _ string) (string, error) {
	if a.depositAddress != "" {
		return a.depositAddress, nil
	}
	return "", fmt.Errorf("%w: CARDANO_DEPOSIT_ADDRESS is not configured", entities.ErrUnsupported)
}

func (a *CardanoAdapter) ValidateAddress(_ context.Context, address string) (bool, error) {
	return strings.HasPrefix(address, "addr1") || strings.HasPrefix(address, "addr_test1"), nil
}

type cardanoTx struct {
	Hash        string `json:"hash"`
	Block       string `json:"block"`
	BlockHeight int64  `json:"block_height"`
	BlockTime   int64  `json:"block_time"`
	Fees        string `json:"fees"`
}

type cardanoUTXO struct {
	Address string          `json:"address"`
	Amount  []cardanoAmount `json:"amount"`
}

type cardanoUTXOs struct {
	Hash    string        `json:"hash"`
	Inputs  []cardanoUTXO `json:"inputs"`
	Outputs []cardanoUTXO `json:"outputs"`
}

func (a *CardanoAdapter) latestHeight(ctx context.Context) (int64, error) {
	var block struct {
		Height int64 `json:"height"`
	}
	if err := a.doJSON(ctx, http.MethodGet, "/blocks/latest", nil, &block); err != nil {
		return 0, err
	}
	return block.Height, nil
}

func (a *CardanoAdapter) GetTransactionStatus(ctx context.Context, txHash string) (*ports.TransactionReceipt, error) {
	var tx cardanoTx
	if err := a.doJSON(ctx, http.MethodGet, "/txs/"+txHash, nil, &tx); err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return &ports.TransactionReceipt{TxHash: txHash, Status: types.StatusPending}, nil
		}
		return nil, err
	}
	var utxos cardanoUTXOs
	if err := a.doJSON(ctx, http.MethodGet, "/txs/"+txHash+"/utxos", nil, &utxos); err != nil {
		return nil, err
	}

	bestAddress := ""
	bestAmount := new(big.Int)
	totalOutput := new(big.Int)
	from := ""
	if len(utxos.Inputs) > 0 {
		from = utxos.Inputs[0].Address
	}
	for _, output := range utxos.Outputs {
		value := lovelaceFromAmounts(output.Amount)
		totalOutput.Add(totalOutput, value)
		if value.Cmp(bestAmount) > 0 {
			bestAmount = value
			bestAddress = output.Address
		}
	}

	latest, _ := a.latestHeight(ctx)
	confirmations := 0
	if latest >= tx.BlockHeight && tx.BlockHeight > 0 {
		confirmations = int(latest - tx.BlockHeight + 1)
	}
	status := types.StatusPending
	if confirmations >= a.requiredConfirmations {
		status = types.StatusConfirmed
	}
	fee, _ := new(big.Int).SetString(tx.Fees, 10)
	if fee == nil {
		fee = new(big.Int)
	}

	return &ports.TransactionReceipt{
		TxHash:        txHash,
		From:          from,
		To:            bestAddress,
		Amount:        &types.Amount{Value: totalOutput, Decimals: 6},
		Fee:           &types.Amount{Value: fee, Decimals: 6},
		Status:        status,
		Confirmations: confirmations,
		BlockNumber:   tx.BlockHeight,
		BlockHash:     tx.Block,
		Timestamp:     time.Unix(tx.BlockTime, 0).UTC(),
	}, nil
}

type cardanoAddressTx struct {
	TxHash      string `json:"tx_hash"`
	BlockHeight int64  `json:"block_height"`
	BlockTime   int64  `json:"block_time"`
}

func (a *CardanoAdapter) GetAddressTransactions(ctx context.Context, address string) ([]ports.IncomingTransaction, error) {
	var transactions []cardanoAddressTx
	path := "/addresses/" + address + "/transactions?order=desc&count=25"
	if err := a.doJSON(ctx, http.MethodGet, path, nil, &transactions); err != nil {
		return nil, err
	}
	result := make([]ports.IncomingTransaction, 0)
	for _, tx := range transactions {
		receipt, err := a.GetTransactionStatus(ctx, tx.TxHash)
		if err != nil || receipt.Amount == nil || receipt.Amount.IsZero() {
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
			BlockHash:     receipt.BlockHash,
			Timestamp:     receipt.Timestamp,
		})
	}
	return result, nil
}

func (a *CardanoAdapter) VerifyTransaction(ctx context.Context, txHash, expectedAddress string, expectedAmount *types.Amount) (bool, error) {
	var utxos cardanoUTXOs
	if err := a.doJSON(ctx, http.MethodGet, "/txs/"+txHash+"/utxos", nil, &utxos); err != nil {
		return false, err
	}
	total := new(big.Int)
	for _, output := range utxos.Outputs {
		if expectedAddress != "" && !strings.EqualFold(output.Address, expectedAddress) {
			continue
		}
		total.Add(total, lovelaceFromAmounts(output.Amount))
	}
	amount := &types.Amount{Value: total, Decimals: 6}
	if expectedAmount != nil && amount.Cmp(expectedAmount) < 0 {
		return false, nil
	}
	return true, nil
}

func (a *CardanoAdapter) EstimateFee(_ context.Context, priority types.TransactionPriority) (*ports.FeeEstimate, error) {
	fee := &types.Amount{Value: big.NewInt(170000), Decimals: 6}
	return &ports.FeeEstimate{
		Fee:              multiplyAmount(fee, a.feeMultiplier),
		FeeRate:          "170000 lovelace",
		EstimatedSeconds: 60,
		Priority:         string(priority),
	}, nil
}

func (a *CardanoAdapter) SendTransaction(ctx context.Context, req ports.SendTransactionRequest) (string, error) {
	if strings.TrimSpace(req.RawTransaction) == "" {
		return "", fmt.Errorf("%w: cardano requires a signed CBOR transaction", entities.ErrUnsupported)
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(req.RawTransaction, "0x"))
	if err != nil {
		return "", fmt.Errorf("invalid cbor transaction: %w", err)
	}
	url := a.baseURL + "/tx/submit"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/cbor")
	if a.apiKey != "" {
		httpReq.Header.Set("project_id", a.apiKey)
	}
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("cardano submit: status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var hash string
	if err := json.Unmarshal(payload, &hash); err != nil {
		return "", err
	}
	return hash, nil
}

var _ ports.BlockchainPort = (*CardanoAdapter)(nil)
