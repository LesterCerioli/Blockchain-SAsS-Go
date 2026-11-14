package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/google/uuid"
)

func createPendingPayment(t *testing.T, f *fixture, amount, reference string) *entities.PaymentIntent {
	t.Helper()
	payment, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
		MerchantID: mustUUID(t, f.merchant), Amount: amount, Currency: "ETH",
		Blockchain: "ethereum", Network: "sepolia", Reference: reference,
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	return payment
}

func TestPaymentServiceCreateValidation(t *testing.T) {
	t.Run("unknown merchant", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
			MerchantID: uuid.New(), Amount: "0.01", Currency: "ETH", Blockchain: "ethereum", Network: "sepolia",
		})
		requireAppErrorCode(t, err, "not_found")
	})

	t.Run("suspended merchant", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		repos := f.store.Repositories()
		merchant, err := repos.Merchants.GetByID(ctx, mustUUID(t, f.merchant))
		if err != nil {
			t.Fatal(err)
		}
		merchant.Suspend(f.clock.Now())
		if err := repos.Merchants.Update(ctx, merchant); err != nil {
			t.Fatal(err)
		}
		_, err = f.payments.Create(ctx, services.CreatePaymentInput{
			MerchantID: mustUUID(t, f.merchant), Amount: "0.01", Currency: "ETH", Blockchain: "ethereum", Network: "sepolia",
		})
		requireAppErrorCode(t, err, "forbidden")
	})

	t.Run("invalid amount", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
			MerchantID: mustUUID(t, f.merchant), Amount: "abc", Currency: "ETH", Blockchain: "ethereum", Network: "sepolia",
		})
		requireAppErrorCode(t, err, "validation_error")
	})

	t.Run("zero amount", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
			MerchantID: mustUUID(t, f.merchant), Amount: "0", Currency: "ETH", Blockchain: "ethereum", Network: "sepolia",
		})
		requireAppErrorCode(t, err, "validation_error")
	})

	t.Run("unsupported blockchain", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
			MerchantID: mustUUID(t, f.merchant), Amount: "0.01", Currency: "ETH", Blockchain: "dogecoin",
		})
		requireAppErrorCode(t, err, "validation_error")
	})

	t.Run("expiry below minimum", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
			MerchantID: mustUUID(t, f.merchant), Amount: "0.01", Currency: "ETH",
			Blockchain: "ethereum", Network: "sepolia", ExpiresIn: 30,
		})
		requireAppErrorCode(t, err, "validation_error")
	})

	t.Run("expiry above configured maximum", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
			MerchantID: mustUUID(t, f.merchant), Amount: "0.01", Currency: "ETH",
			Blockchain: "ethereum", Network: "sepolia", ExpiresIn: 7200,
		})
		requireAppErrorCode(t, err, "validation_error")
	})
}

func TestPaymentServiceCreateIdempotency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	input := services.CreatePaymentInput{
		MerchantID: mustUUID(t, f.merchant), Amount: "0.01", Currency: "ETH",
		Blockchain: "ethereum", Network: "sepolia", Reference: "order-idem",
		IdempotencyKey: "idem-key", Metadata: map[string]string{"cart": "42"},
	}
	first, err := f.payments.Create(ctx, input)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if first.Metadata["cart"] != "42" {
		t.Fatalf("metadata = %+v", first.Metadata)
	}

	input.Amount = "0.02"
	second, err := f.payments.Create(ctx, input)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("idempotent create returned %s, want %s", second.ID, first.ID)
	}

	_, total, err := f.payments.List(ctx, ports.PaymentFilter{MerchantID: mustUUID(t, f.merchant)})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
}

func TestPaymentServiceCreateCryptoMode(t *testing.T) {
	f := newFixture(t)
	payment, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
		MerchantID: mustUUID(t, f.merchant), Amount: "0.005", Currency: "ETH",
		Blockchain: "ethereum", Network: "sepolia",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payment.Currency != "ETH" || payment.CryptoCurrency != "ETH" {
		t.Fatalf("currencies = %s/%s", payment.Currency, payment.CryptoCurrency)
	}
	if payment.Amount.ToDecimalString() != "0.005" || payment.CryptoAmount.ToDecimalString() != "0.005" {
		t.Fatalf("amounts = %s/%s", payment.Amount.ToDecimalString(), payment.CryptoAmount.ToDecimalString())
	}
	if _, ok := payment.Metadata["exchange_rate"]; ok {
		t.Fatal("crypto mode must not record an exchange rate")
	}
}

func TestPaymentServiceCreateFiatMode(t *testing.T) {
	f := newFixture(t)
	payment, err := f.payments.Create(context.Background(), services.CreatePaymentInput{
		MerchantID: mustUUID(t, f.merchant), Amount: "2", Currency: "USD",
		Blockchain: "ethereum", Network: "sepolia",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payment.Currency != "USD" || payment.CryptoCurrency != "ETH" {
		t.Fatalf("currencies = %s/%s", payment.Currency, payment.CryptoCurrency)
	}
	if payment.CryptoAmount.ToDecimalString() != "1" {
		t.Fatalf("crypto amount = %s, want 1", payment.CryptoAmount.ToDecimalString())
	}
	if payment.Metadata["exchange_rate"] != "2.00000000" {
		t.Fatalf("exchange rate = %q", payment.Metadata["exchange_rate"])
	}
	if payment.Metadata["exchange_source"] != "stub" {
		t.Fatalf("exchange source = %q", payment.Metadata["exchange_source"])
	}
}

func TestPaymentServiceCancel(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createPendingPayment(t, f, "0.01", "cancel")

	canceled, err := f.payments.Cancel(ctx, mustUUID(t, f.merchant), payment.ID, "actor", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canceled.Status != entities.PaymentStatusCanceled {
		t.Fatalf("status = %s", canceled.Status)
	}

	_, err = f.payments.Cancel(ctx, mustUUID(t, f.merchant), payment.ID, "actor", "", "")
	requireAppErrorCode(t, err, "conflict")

	processing := createPendingPayment(t, f, "0.02", "cancel-processing")
	if _, err := f.payments.HandleIncoming(ctx, ports.IncomingTransaction{
		Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
		TxHash: "0xprocessing", To: processing.DepositAddress, Amount: processing.CryptoAmount,
		Confirmations: 0, Status: types.StatusPending,
	}); err != nil {
		t.Fatalf("handle incoming: %v", err)
	}
	_, err = f.payments.Cancel(ctx, mustUUID(t, f.merchant), processing.ID, "actor", "", "")
	requireAppErrorCode(t, err, "conflict")

	_, err = f.payments.Cancel(ctx, mustUUID(t, f.merchant), uuid.New(), "actor", "", "")
	requireAppErrorCode(t, err, "not_found")
}

func TestPaymentServiceRefund(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createConfirmedPayment(t, f, "0.02", "refund")

	refunded, err := f.payments.Refund(ctx, mustUUID(t, f.merchant), payment.ID, services.RefundInput{TxHash: "0xrefund", Actor: "actor"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refunded.Status != entities.PaymentStatusRefunded || refunded.RefundTxHash != "0xrefund" {
		t.Fatalf("payment = %+v", refunded)
	}

	_, err = f.payments.Refund(ctx, mustUUID(t, f.merchant), payment.ID, services.RefundInput{TxHash: "0xagain"})
	requireAppErrorCode(t, err, "unprocessable_entity")

	pending := createPendingPayment(t, f, "0.03", "refund-pending")
	_, err = f.payments.Refund(ctx, mustUUID(t, f.merchant), pending.ID, services.RefundInput{TxHash: "0xany"})
	requireAppErrorCode(t, err, "unprocessable_entity")

	invalidAmount := createConfirmedPayment(t, f, "0.04", "refund-invalid")
	_, err = f.payments.Refund(ctx, mustUUID(t, f.merchant), invalidAmount.ID, services.RefundInput{TxHash: "0xany", Amount: "abc"})
	requireAppErrorCode(t, err, "validation_error")
}

func TestPaymentServiceRefundBroadcast(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	withDestination := createConfirmedPayment(t, f, "0.05", "refund-destination")
	refunded, err := f.payments.Refund(ctx, mustUUID(t, f.merchant), withDestination.ID, services.RefundInput{DestinationAddress: "0xdestination"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(refunded.RefundTxHash, "mock-") {
		t.Fatalf("refund tx hash = %q", refunded.RefundTxHash)
	}

	withoutTransaction := createConfirmedPayment(t, f, "0.06", "refund-empty")
	_, err = f.payments.Refund(ctx, mustUUID(t, f.merchant), withoutTransaction.ID, services.RefundInput{})
	requireAppErrorCode(t, err, "service_unavailable")
}

func TestPaymentServiceConfirmRules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createPendingPayment(t, f, "0.01", "confirm-rules")

	_, err := f.payments.Confirm(ctx, mustUUID(t, f.merchant), payment.ID, "", "actor", "", "")
	requireAppErrorCode(t, err, "validation_error")

	_, err = f.payments.Confirm(ctx, mustUUID(t, f.merchant), uuid.New(), "0xabc", "actor", "", "")
	requireAppErrorCode(t, err, "not_found")

	_, err = f.payments.Confirm(ctx, uuid.New(), payment.ID, "0xabc", "actor", "", "")
	requireAppErrorCode(t, err, "not_found")

	canceled, err := f.payments.Cancel(ctx, mustUUID(t, f.merchant), payment.ID, "actor", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.payments.Confirm(ctx, mustUUID(t, f.merchant), canceled.ID, "0xabc", "actor", "", "")
	requireAppErrorCode(t, err, "conflict")

	succeeded := createConfirmedPayment(t, f, "0.02", "confirm-succeeded")
	again, err := f.payments.Confirm(ctx, mustUUID(t, f.merchant), succeeded.ID, "0xignored", "actor", "", "")
	if err != nil {
		t.Fatalf("succeeded payment confirm should be idempotent: %v", err)
	}
	if again.Status != entities.PaymentStatusSucceeded {
		t.Fatalf("status = %s", again.Status)
	}

	expired := createPendingPayment(t, f, "0.03", "confirm-expired")
	stored, err := f.store.Repositories().Payments.GetByID(ctx, expired.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.ExpiresAt = f.clock.Now().Add(-time.Minute)
	if err := f.store.Repositories().Payments.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	_, err = f.payments.Confirm(ctx, mustUUID(t, f.merchant), expired.ID, "0xexpired", "actor", "", "")
	requireAppErrorCode(t, err, "unprocessable_entity")
}

func TestPaymentServiceGetAndList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createPendingPayment(t, f, "0.01", "get")
	canceled, err := f.payments.Cancel(ctx, mustUUID(t, f.merchant), payment.ID, "actor", "", "")
	if err != nil {
		t.Fatal(err)
	}

	stored, err := f.payments.Get(ctx, mustUUID(t, f.merchant), payment.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.ID != payment.ID {
		t.Fatalf("payment id = %s", stored.ID)
	}

	_, err = f.payments.Get(ctx, uuid.New(), payment.ID)
	requireAppErrorCode(t, err, "not_found")

	_, err = f.payments.Get(ctx, mustUUID(t, f.merchant), uuid.New())
	requireAppErrorCode(t, err, "not_found")

	createPendingPayment(t, f, "0.02", "list")
	payments, total, err := f.payments.List(ctx, ports.PaymentFilter{
		MerchantID: mustUUID(t, f.merchant), Statuses: []entities.PaymentIntentStatus{entities.PaymentStatusCanceled},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(payments) != 1 || payments[0].ID != canceled.ID {
		t.Fatalf("total = %d payments = %+v", total, payments)
	}
}

func TestPaymentServiceHandleIncoming(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.payments.HandleIncoming(ctx, ports.IncomingTransaction{})
	requireAppErrorCode(t, err, "validation_error")

	_, err = f.payments.HandleIncoming(ctx, ports.IncomingTransaction{
		Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
		TxHash: "0xmissing", To: "0xunknown", Amount: mustAmount(t, "1", 18),
	})
	if !errors.Is(err, entities.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	payment := createPendingPayment(t, f, "0.01", "incoming-underpaid")
	_, err = f.payments.HandleIncoming(ctx, ports.IncomingTransaction{
		Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
		TxHash: "0xunderpaid", To: payment.DepositAddress, Amount: mustAmount(t, "0.001", 18),
		Status: types.StatusConfirmed, Confirmations: 12,
	})
	if !errors.Is(err, entities.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	failed := createPendingPayment(t, f, "0.02", "incoming-failed")
	result, err := f.payments.HandleIncoming(ctx, ports.IncomingTransaction{
		Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
		TxHash: "0xfailed", To: failed.DepositAddress, Amount: failed.CryptoAmount,
		Status: types.StatusFailed, Confirmations: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != entities.PaymentStatusFailed {
		t.Fatalf("status = %s, want failed", result.Status)
	}
}

func TestPaymentServiceRefreshAndExpire(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	pending := createPendingPayment(t, f, "0.01", "refresh")
	if _, err := f.payments.HandleIncoming(ctx, ports.IncomingTransaction{
		Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
		TxHash: "0xrefresh", To: pending.DepositAddress, Amount: pending.CryptoAmount,
		Status: types.StatusPending, Confirmations: 0,
	}); err != nil {
		t.Fatalf("handle incoming: %v", err)
	}
	updated, err := f.payments.RefreshPending(ctx)
	if err != nil {
		t.Fatalf("refresh pending: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}

	expiring := createPendingPayment(t, f, "0.02", "expire")
	stored, err := f.store.Repositories().Payments.GetByID(ctx, expiring.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.ExpiresAt = f.clock.Now().Add(-time.Minute)
	if err := f.store.Repositories().Payments.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	expired, err := f.payments.ExpireStale(ctx)
	if err != nil {
		t.Fatalf("expire stale: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}
	result, err := f.payments.Get(ctx, mustUUID(t, f.merchant), expiring.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != entities.PaymentStatusExpired {
		t.Fatalf("status = %s, want expired", result.Status)
	}
	expired, err = f.payments.ExpireStale(ctx)
	if err != nil {
		t.Fatalf("expire stale: %v", err)
	}
	if expired != 0 {
		t.Fatalf("expired = %d, want 0", expired)
	}
}

func mustAmount(t *testing.T, value string, decimals int) *types.Amount {
	t.Helper()
	amount, err := types.NewAmountFromDecimal(value, decimals)
	if err != nil {
		t.Fatalf("parse amount %s: %v", value, err)
	}
	return amount
}
