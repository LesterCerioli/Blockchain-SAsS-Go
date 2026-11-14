package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/blockchain"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/google/uuid"
)

type stubChainAdapter struct {
	*blockchain.MockAdapter
	receipt      *ports.TransactionReceipt
	receiptErr   error
	verified     bool
	verifyErr    error
	sendHash     string
	sendErr      error
	validAddress bool
	validateErr  error
}

func newStubChainAdapter() *stubChainAdapter {
	return &stubChainAdapter{
		MockAdapter:  blockchain.NewMockAdapter(types.BlockchainEthereum, types.NetworkEthereumSepolia, 12),
		verified:     true,
		sendHash:     "0xstub-send",
		validAddress: true,
	}
}

func (a *stubChainAdapter) GetTransactionStatus(context.Context, string) (*ports.TransactionReceipt, error) {
	return a.receipt, a.receiptErr
}

func (a *stubChainAdapter) VerifyTransaction(context.Context, string, string, *types.Amount) (bool, error) {
	return a.verified, a.verifyErr
}

func (a *stubChainAdapter) SendTransaction(context.Context, ports.SendTransactionRequest) (string, error) {
	return a.sendHash, a.sendErr
}

func (a *stubChainAdapter) ValidateAddress(context.Context, string) (bool, error) {
	return a.validAddress, a.validateErr
}

type stubRegistry struct {
	adapter ports.BlockchainPort
	err     error
}

func (r stubRegistry) Get(types.BlockchainType, types.BlockchainNetwork) (ports.BlockchainPort, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.adapter, nil
}

func (r stubRegistry) Supported() []types.BlockchainInfo { return nil }

func (r stubRegistry) Health(context.Context) map[string]error { return nil }

func newPaymentServiceWithRegistry(t *testing.T, f *fixture, registry ports.BlockchainRegistry) *services.PaymentService {
	t.Helper()
	repos := f.store.Repositories()
	return services.NewPaymentService(
		repos.Merchants, repos.Payments, repos.Transactions, repos.Audit, registry,
		services.NewExchangeService(stubExchange{}, nil, time.Minute, f.clock, nil),
		services.NewValidationService(),
		services.NewFraudService(repos.Payments, services.FraudOptions{MaxAmountUSD: 1_000_000}, f.clock),
		nil, f.clock, nil,
		services.PaymentServiceOptions{DefaultExpiry: 15 * time.Minute, MaxExpiry: time.Hour},
	)
}

func TestPaymentServiceConfirmSuccess(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createPendingPayment(t, f, "0.000000000001", "confirm-success")

	adapter := newStubChainAdapter()
	adapter.receipt = &ports.TransactionReceipt{
		TxHash: "0xconfirmed", To: payment.DepositAddress, Amount: payment.CryptoAmount,
		Status: types.StatusConfirmed, Confirmations: 12,
	}
	service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

	confirmed, err := service.Confirm(ctx, mustUUID(t, f.merchant), payment.ID, " 0xconfirmed ", "actor", "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if confirmed.Status != entities.PaymentStatusSucceeded {
		t.Fatalf("status = %s, want succeeded", confirmed.Status)
	}
	if confirmed.TxHash != "0xconfirmed" || confirmed.Confirmations != 12 {
		t.Fatalf("payment = %+v", confirmed)
	}
}

func TestPaymentServiceConfirmProcessing(t *testing.T) {
	t.Run("nil receipt", func(t *testing.T) {
		f := newFixture(t)
		payment := createPendingPayment(t, f, "0.01", "confirm-nil")
		adapter := newStubChainAdapter()
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

		confirmed, err := service.Confirm(context.Background(), mustUUID(t, f.merchant), payment.ID, "0xpending", "actor", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if confirmed.Status != entities.PaymentStatusProcessing || confirmed.TxHash != "0xpending" {
			t.Fatalf("payment = %+v", confirmed)
		}
	})

	t.Run("underpaid receipt", func(t *testing.T) {
		f := newFixture(t)
		payment := createPendingPayment(t, f, "0.01", "confirm-underpaid")
		adapter := newStubChainAdapter()
		adapter.receipt = &ports.TransactionReceipt{
			TxHash: "0xunderpaid", Amount: mustAmount(t, "0.001", 18),
			Status: types.StatusConfirmed, Confirmations: 12,
		}
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

		confirmed, err := service.Confirm(context.Background(), mustUUID(t, f.merchant), payment.ID, "0xunderpaid", "actor", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if confirmed.Status != entities.PaymentStatusProcessing {
			t.Fatalf("status = %s, want processing", confirmed.Status)
		}
	})
}

func TestPaymentServiceConfirmAdapterErrors(t *testing.T) {
	t.Run("receipt error", func(t *testing.T) {
		f := newFixture(t)
		payment := createPendingPayment(t, f, "0.01", "confirm-receipt-error")
		adapter := newStubChainAdapter()
		adapter.receiptErr = errors.New("rpc down")
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

		_, err := service.Confirm(context.Background(), mustUUID(t, f.merchant), payment.ID, "0xerror", "actor", "", "")
		requireAppErrorCode(t, err, "service_unavailable")
	})

	t.Run("verify error", func(t *testing.T) {
		f := newFixture(t)
		payment := createPendingPayment(t, f, "0.01", "confirm-verify-error")
		adapter := newStubChainAdapter()
		adapter.receipt = &ports.TransactionReceipt{
			TxHash: "0xverify", Amount: payment.CryptoAmount, Status: types.StatusConfirmed, Confirmations: 12,
		}
		adapter.verifyErr = errors.New("verify failed")
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

		_, err := service.Confirm(context.Background(), mustUUID(t, f.merchant), payment.ID, "0xverify", "actor", "", "")
		requireAppErrorCode(t, err, "service_unavailable")
	})

	t.Run("verification mismatch", func(t *testing.T) {
		f := newFixture(t)
		payment := createPendingPayment(t, f, "0.01", "confirm-mismatch")
		adapter := newStubChainAdapter()
		adapter.receipt = &ports.TransactionReceipt{
			TxHash: "0xmismatch", Amount: payment.CryptoAmount, Status: types.StatusConfirmed, Confirmations: 12,
		}
		adapter.verified = false
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

		_, err := service.Confirm(context.Background(), mustUUID(t, f.merchant), payment.ID, "0xmismatch", "actor", "", "")
		requireAppErrorCode(t, err, "validation_error")
	})

	t.Run("registry error", func(t *testing.T) {
		f := newFixture(t)
		payment := createPendingPayment(t, f, "0.01", "confirm-registry-error")
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{err: entities.ErrUnsupported})

		_, err := service.Confirm(context.Background(), mustUUID(t, f.merchant), payment.ID, "0xunsupported", "actor", "", "")
		requireAppErrorCode(t, err, "unsupported")
	})
}

func TestPaymentServiceRefreshPendingErrors(t *testing.T) {
	prepare := func(t *testing.T) (*fixture, *entities.PaymentIntent) {
		t.Helper()
		f := newFixture(t)
		payment := createPendingPayment(t, f, "0.01", "refresh-error")
		if _, err := f.payments.HandleIncoming(context.Background(), ports.IncomingTransaction{
			Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
			TxHash: "0xpending", To: payment.DepositAddress, Amount: payment.CryptoAmount,
			Status: types.StatusPending, Confirmations: 0,
		}); err != nil {
			t.Fatalf("handle incoming: %v", err)
		}
		return f, payment
	}

	t.Run("registry error", func(t *testing.T) {
		f, _ := prepare(t)
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{err: entities.ErrUnsupported})
		updated, err := service.RefreshPending(context.Background())
		if err != nil || updated != 0 {
			t.Fatalf("updated = %d err = %v", updated, err)
		}
	})

	t.Run("nil receipt", func(t *testing.T) {
		f, _ := prepare(t)
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: newStubChainAdapter()})
		updated, err := service.RefreshPending(context.Background())
		if err != nil || updated != 0 {
			t.Fatalf("updated = %d err = %v", updated, err)
		}
	})

	t.Run("receipt error", func(t *testing.T) {
		f, _ := prepare(t)
		adapter := newStubChainAdapter()
		adapter.receiptErr = errors.New("rpc down")
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})
		updated, err := service.RefreshPending(context.Background())
		if err != nil || updated != 0 {
			t.Fatalf("updated = %d err = %v", updated, err)
		}
	})

	t.Run("failed receipt", func(t *testing.T) {
		f, payment := prepare(t)
		adapter := newStubChainAdapter()
		adapter.receipt = &ports.TransactionReceipt{
			TxHash: "0xpending", Amount: payment.CryptoAmount, Status: types.StatusFailed, Confirmations: 0,
		}
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})
		updated, err := service.RefreshPending(context.Background())
		if err != nil || updated != 1 {
			t.Fatalf("updated = %d err = %v", updated, err)
		}
		stored, err := f.payments.Get(context.Background(), mustUUID(t, f.merchant), payment.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != entities.PaymentStatusFailed {
			t.Fatalf("status = %s, want failed", stored.Status)
		}
	})
}

func TestPaymentServiceRefundErrors(t *testing.T) {
	t.Run("not succeeded", func(t *testing.T) {
		f := newFixture(t)
		payment := createPendingPayment(t, f, "0.01", "refund-pending")
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: newStubChainAdapter()})
		_, err := service.Refund(context.Background(), mustUUID(t, f.merchant), payment.ID, services.RefundInput{})
		requireAppErrorCode(t, err, "unprocessable_entity")
	})

	t.Run("invalid amount", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.02", "refund-bad-amount")
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: newStubChainAdapter()})
		_, err := service.Refund(context.Background(), mustUUID(t, f.merchant), payment.ID, services.RefundInput{Amount: "abc"})
		requireAppErrorCode(t, err, "validation_error")
	})

	t.Run("invalid destination", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.03", "refund-bad-destination")
		adapter := newStubChainAdapter()
		adapter.validAddress = false
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})
		_, err := service.Refund(context.Background(), mustUUID(t, f.merchant), payment.ID, services.RefundInput{DestinationAddress: "0xbad"})
		requireAppErrorCode(t, err, "validation_error")
	})

	t.Run("broadcast error", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.04", "refund-broadcast-error")
		adapter := newStubChainAdapter()
		adapter.sendErr = errors.New("broadcast failed")
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})
		_, err := service.Refund(context.Background(), mustUUID(t, f.merchant), payment.ID, services.RefundInput{})
		requireAppErrorCode(t, err, "service_unavailable")
	})

	t.Run("registry error", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.05", "refund-registry-error")
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{err: entities.ErrUnsupported})
		_, err := service.Refund(context.Background(), mustUUID(t, f.merchant), payment.ID, services.RefundInput{})
		requireAppErrorCode(t, err, "unsupported")
	})

	t.Run("refund to destination", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.06", "refund-destination")
		adapter := newStubChainAdapter()
		adapter.sendHash = "0xbroadcast"
		service := newPaymentServiceWithRegistry(t, f, stubRegistry{adapter: adapter})
		refunded, err := service.Refund(context.Background(), mustUUID(t, f.merchant), payment.ID, services.RefundInput{
			DestinationAddress: "0xdestination", Amount: "0.01",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if refunded.RefundTxHash != "0xbroadcast" || refunded.Status != entities.PaymentStatusRefunded {
			t.Fatalf("payment = %+v", refunded)
		}
	})
}

func TestPaymentServiceGetWithNilMerchant(t *testing.T) {
	f := newFixture(t)
	payment := createPendingPayment(t, f, "0.01", "nil-merchant")
	got, err := f.payments.Get(context.Background(), uuid.Nil, payment.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != payment.ID {
		t.Fatalf("payment id = %s", got.ID)
	}
}
