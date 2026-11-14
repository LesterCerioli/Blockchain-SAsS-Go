package services_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/blockchain"
	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/repositories"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/google/uuid"
)

func mustUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(value)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	return id
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

type stubExchange struct{}

func (stubExchange) Name() string { return "stub" }

func (stubExchange) GetRate(_ context.Context, base, quote string) (*ports.ExchangeRate, error) {
	return &ports.ExchangeRate{Base: base, Quote: quote, Rate: 2, Source: "stub", Timestamp: time.Now().UTC()}, nil
}

func (stubExchange) Health(context.Context) error { return nil }

type fixture struct {
	payments *services.PaymentService
	escrows  *services.EscrowService
	merchant string
	store    *repositories.MemoryStore
	clock    fixedClock
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	clock := fixedClock{now: time.Now().UTC()}
	store := repositories.NewMemoryRepository()
	repos := store.Repositories()

	merchantService := services.NewMerchantService(
		repos.Merchants, repos.Audit, services.NewValidationService(), clock, nil, "test",
		services.TokenOptions{Secret: strings.Repeat("s", 32), TTL: time.Hour},
	)
	merchant, _, err := merchantService.Register(ctx, services.RegisterMerchantInput{
		Name: "Test Merchant", Email: "merchant@example.com",
	})
	if err != nil {
		t.Fatalf("register merchant: %v", err)
	}

	registry := blockchain.NewRegistry(blockchain.Options{
		Mock: true,
		Chains: map[types.BlockchainType]blockchain.AdapterOptions{
			types.BlockchainEthereum: {
				Enabled: true, Network: types.NetworkEthereumSepolia,
				RequiredConfirmations: 12,
			},
		},
	})
	exchangeService := services.NewExchangeService(stubExchange{}, nil, time.Minute, clock, nil)
	fraud := services.NewFraudService(repos.Payments, services.FraudOptions{MaxAmountUSD: 1_000_000}, clock)
	paymentService := services.NewPaymentService(
		repos.Merchants, repos.Payments, repos.Transactions, repos.Audit, registry,
		exchangeService, services.NewValidationService(), fraud, nil, clock, nil,
		services.PaymentServiceOptions{DefaultExpiry: 15 * time.Minute, MaxExpiry: time.Hour},
	)
	escrowService := services.NewEscrowService(
		repos.Escrows, repos.Payments, repos.Audit, registry, nil, clock, nil,
		services.EscrowServiceOptions{MaxDuration: 720 * time.Hour},
	)

	return &fixture{
		payments: paymentService, escrows: escrowService,
		merchant: merchant.ID.String(), store: store, clock: clock,
	}
}

func (f *fixture) createPayment(t *testing.T) *types.Amount {
	t.Helper()
	payment, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
		MerchantID: mustUUID(t, f.merchant),
		Amount:     "0.01", Currency: "ETH",
		Blockchain: "ethereum", Network: "sepolia",
		Reference: "order-1",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if payment.DepositAddress == "" {
		t.Fatal("expected a deposit address")
	}
	if payment.Status != "requires_payment" {
		t.Fatalf("status = %s, want requires_payment", payment.Status)
	}
	return payment.CryptoAmount
}

func TestPaymentCreateAndIncomingConfirmation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	payment, err := f.payments.Create(ctx, services.CreatePaymentInput{
		MerchantID: mustUUID(t, f.merchant), Amount: "0.01", Currency: "ETH",
		Blockchain: "ethereum", Network: "sepolia", Reference: "order-1",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	confirmed, err := f.payments.HandleIncoming(ctx, ports.IncomingTransaction{
		Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
		TxHash: "0xabc123", From: "0xsender", To: payment.DepositAddress,
		Amount: payment.CryptoAmount, Confirmations: 12, Status: types.StatusConfirmed,
	})
	if err != nil {
		t.Fatalf("handle incoming: %v", err)
	}
	if confirmed.Status != "succeeded" {
		t.Fatalf("status = %s, want succeeded", confirmed.Status)
	}
	if confirmed.TxHash != "0xabc123" {
		t.Fatalf("tx hash = %s", confirmed.TxHash)
	}

	duplicate, err := f.payments.HandleIncoming(ctx, ports.IncomingTransaction{
		Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
		TxHash: "0xabc123", To: payment.DepositAddress,
		Amount: payment.CryptoAmount, Confirmations: 12, Status: types.StatusConfirmed,
	})
	if err != nil {
		t.Fatalf("duplicate should be ignored: %v", err)
	}
	if duplicate != nil {
		t.Fatal("duplicate transaction must not return a payment")
	}
}

func TestEscrowLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.createPayment(t)

	payment, err := f.payments.Create(ctx, services.CreatePaymentInput{
		MerchantID: mustUUID(t, f.merchant), Amount: "0.02", Currency: "ETH",
		Blockchain: "ethereum", Network: "sepolia", Reference: "order-2",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if _, err := f.payments.HandleIncoming(ctx, ports.IncomingTransaction{
		Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
		TxHash: "0xescrow", To: payment.DepositAddress,
		Amount: payment.CryptoAmount, Confirmations: 12, Status: types.StatusConfirmed,
	}); err != nil {
		t.Fatalf("confirm payment: %v", err)
	}

	escrow, err := f.escrows.Create(ctx, services.CreateEscrowInput{
		MerchantID: mustUUID(t, f.merchant), PaymentIntentID: payment.ID,
		Beneficiary: "0xbeneficiary",
	})
	if err != nil {
		t.Fatalf("create escrow: %v", err)
	}
	if escrow.Status != "funded" {
		t.Fatalf("escrow status = %s, want funded", escrow.Status)
	}

	released, err := f.escrows.Release(ctx, mustUUID(t, f.merchant), escrow.ID, false, services.EscrowExecutionInput{
		TxHash: "0xrelease",
	})
	if err != nil {
		t.Fatalf("release escrow: %v", err)
	}
	if released.Status != "released" {
		t.Fatalf("escrow status = %s, want released", released.Status)
	}
	if released.TxHash != "0xrelease" {
		t.Fatalf("escrow tx hash = %s", released.TxHash)
	}
}
