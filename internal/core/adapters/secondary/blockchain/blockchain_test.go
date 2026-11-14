package blockchain

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

func TestParsingHelpers(t *testing.T) {
	if value, err := parseHexInt64(" 0x10 "); err != nil || value != 16 {
		t.Fatalf("value = %d err = %v", value, err)
	}
	if value, err := parseHexInt64(""); err != nil || value != 0 {
		t.Fatalf("value = %d err = %v", value, err)
	}
	if _, err := parseHexInt64("zz"); err == nil {
		t.Fatal("expected parse error")
	}

	if value, err := parseHexBigInt("0xff"); err != nil || value.Int64() != 255 {
		t.Fatalf("value = %v err = %v", value, err)
	}
	if value, err := parseHexBigInt(""); err != nil || value.Sign() != 0 {
		t.Fatalf("value = %v err = %v", value, err)
	}
	if _, err := parseHexBigInt("zz"); err == nil {
		t.Fatal("expected parse error")
	}
	if got := formatHexInt64(255); got != "0xff" {
		t.Fatalf("hex = %q", got)
	}
}

func TestAmountHelpers(t *testing.T) {
	amount, err := amountFromAtomicString("1000", 2)
	if err != nil || amount.ToDecimalString() != "10" {
		t.Fatalf("amount = %v err = %v", amount, err)
	}
	if amount, err := amountFromAtomicString("bad", 2); err == nil || amount != nil {
		t.Fatalf("amount = %v err = %v", amount, err)
	}

	floatAmount := amountFromFloat(1.5, 8)
	if floatAmount == nil || floatAmount.ToDecimalString() != "1.5" {
		t.Fatalf("float amount = %v", floatAmount)
	}

	fee := mergeFee(big.NewInt(2), big.NewInt(3))
	if fee == nil || fee.Value.Int64() != 6 || fee.Decimals != 18 {
		t.Fatalf("fee = %+v", fee)
	}
	if mergeFee(nil, big.NewInt(1)) != nil {
		t.Fatal("expected nil fee")
	}

	base := &types.Amount{Value: big.NewInt(100), Decimals: 2}
	if got := multiplyAmount(base, 1.5); got.Value.Int64() != 150 {
		t.Fatalf("multiplied = %v", got)
	}
	if got := multiplyAmount(base, 0); got != base {
		t.Fatal("zero multiplier must return original")
	}
	if multiplyAmount(nil, 2) != nil {
		t.Fatal("nil amount must return nil")
	}
}

func TestMockAdapter(t *testing.T) {
	ctx := context.Background()
	adapter := NewMockAdapter(types.BlockchainEthereum, types.NetworkEthereumSepolia, 0)
	if adapter.Name() != "mock-ethereum" || adapter.Type() != types.BlockchainEthereum || adapter.Network() != types.NetworkEthereumSepolia {
		t.Fatalf("adapter = %+v", adapter)
	}
	if adapter.Info().RequiredConfirmations != 1 {
		t.Fatalf("confirmations = %d", adapter.Info().RequiredConfirmations)
	}
	if err := adapter.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if balance, err := adapter.GetBalance(ctx, "0xabc"); err != nil || balance == nil {
		t.Fatalf("balance = %v err = %v", balance, err)
	}
	if address, err := adapter.GenerateAddress(ctx, "ref"); err != nil || address == "" {
		t.Fatalf("address = %q err = %v", address, err)
	}
	if valid, err := adapter.ValidateAddress(ctx, ""); err != nil || valid {
		t.Fatalf("valid = %v err = %v", valid, err)
	}
	receipt, err := adapter.GetTransactionStatus(ctx, "0xhash")
	if err != nil || receipt.TxHash != "0xhash" || receipt.Confirmations != 1 {
		t.Fatalf("receipt = %+v err = %v", receipt, err)
	}
	if transactions, err := adapter.GetAddressTransactions(ctx, "0xabc"); err != nil || len(transactions) != 0 {
		t.Fatalf("transactions = %+v err = %v", transactions, err)
	}
	if verified, err := adapter.VerifyTransaction(ctx, "0xhash", "0xabc", nil); err != nil || !verified {
		t.Fatalf("verified = %v err = %v", verified, err)
	}
	if estimate, err := adapter.EstimateFee(ctx, types.PriorityMedium); err != nil || estimate == nil {
		t.Fatalf("estimate = %v err = %v", estimate, err)
	}
	if hash, err := adapter.SendTransaction(ctx, ports.SendTransactionRequest{To: "0xabc"}); err != nil || hash == "" {
		t.Fatalf("hash = %q err = %v", hash, err)
	}
	if _, err := adapter.SendTransaction(ctx, ports.SendTransactionRequest{}); err == nil {
		t.Fatal("expected send error")
	}
}

func TestRegistry(t *testing.T) {
	registry := NewRegistry(Options{
		Mock: true,
		Chains: map[types.BlockchainType]AdapterOptions{
			types.BlockchainEthereum: {
				Enabled: true, Network: types.NetworkEthereumSepolia, RequiredConfirmations: 12,
			},
			types.BlockchainBitcoin: {Enabled: false, Network: types.NetworkBitcoinMainnet},
		},
	})

	supported := registry.Supported()
	if len(supported) != 1 || supported[0].Type != types.BlockchainEthereum {
		t.Fatalf("supported = %+v", supported)
	}
	adapter, err := registry.Get(types.BlockchainEthereum, types.NetworkEthereumSepolia)
	if err != nil || adapter.Type() != types.BlockchainEthereum {
		t.Fatalf("adapter = %+v err = %v", adapter, err)
	}
	fallback, err := registry.Get(types.BlockchainEthereum, types.NetworkEthereumMainnet)
	if err != nil || fallback.Type() != types.BlockchainEthereum {
		t.Fatalf("fallback = %+v err = %v", fallback, err)
	}
	if _, err := registry.Get(types.BlockchainSolana, types.NetworkDevnet); !errors.Is(err, entities.ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
	health := registry.Health(context.Background())
	if len(health) != 1 {
		t.Fatalf("health = %+v", health)
	}
	for name, err := range health {
		if err != nil {
			t.Fatalf("%s health = %v", name, err)
		}
	}
}

func TestRegistryWithoutMockAdapters(t *testing.T) {
	registry := NewRegistry(Options{Chains: map[types.BlockchainType]AdapterOptions{
		types.BlockchainEthereum: {Enabled: true, Network: types.NetworkEthereumSepolia},
		types.BlockchainBitcoin:  {Enabled: true, Network: types.NetworkBitcoinTestnet},
		types.BlockchainSolana:   {Enabled: true, Network: types.NetworkDevnet},
		types.BlockchainCardano:  {Enabled: true, Network: types.NetworkTestnet},
	}})
	if len(registry.Supported()) != 4 {
		t.Fatalf("supported = %+v", registry.Supported())
	}
}

func TestJSONRPCClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		switch r.Header.Get("X-Case") {
		case "error":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-1,"message":"failed"}}`))
		case "http":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "bad":
			_, _ = w.Write([]byte("not-json"))
		case "null":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":null}`))
		default:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x10"}`))
		}
	}))
	defer server.Close()
	client := NewJSONRPCClient(server.URL, "", "", time.Second)

	var result string
	if err := client.Call(context.Background(), "eth_blockNumber", []interface{}{}, &result); err != nil || result != "0x10" {
		t.Fatalf("result = %q err = %v", result, err)
	}
	if err := client.Call(context.Background(), "nil", nil, nil); err != nil {
		t.Fatalf("nil result: %v", err)
	}

	for _, tc := range []struct{ name, header string }{{"error", "error"}, {"http", "http"}, {"bad", "bad"}, {"null", "null"}} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = req
		_ = tc
	}

	badClient := NewJSONRPCClient("://bad-url", "", "", 0)
	if err := badClient.Call(context.Background(), "method", nil, nil); err == nil {
		t.Fatal("expected request error")
	}
}
