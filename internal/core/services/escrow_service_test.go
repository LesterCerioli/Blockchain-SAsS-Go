package services_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/google/uuid"
)

func createConfirmedPayment(t *testing.T, f *fixture, amount, reference string) *entities.PaymentIntent {
	t.Helper()
	ctx := context.Background()
	payment, err := f.payments.Create(ctx, services.CreatePaymentInput{
		MerchantID: mustUUID(t, f.merchant), Amount: amount, Currency: "ETH",
		Blockchain: "ethereum", Network: "sepolia", Reference: reference,
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	confirmed, err := f.payments.HandleIncoming(ctx, ports.IncomingTransaction{
		Blockchain: types.BlockchainEthereum, Network: types.NetworkEthereumSepolia,
		TxHash: "0x" + strings.ReplaceAll(uuid.NewString(), "-", ""), From: "0xsender",
		To: payment.DepositAddress, Amount: payment.CryptoAmount,
		Confirmations: 12, Status: types.StatusConfirmed,
	})
	if err != nil {
		t.Fatalf("confirm payment: %v", err)
	}
	if confirmed.Status != entities.PaymentStatusSucceeded {
		t.Fatalf("payment status = %s, want succeeded", confirmed.Status)
	}
	return confirmed
}

func createEscrowForPayment(t *testing.T, f *fixture, payment *entities.PaymentIntent, releaseAt *time.Time) *entities.Escrow {
	t.Helper()
	escrow, err := f.escrows.Create(context.Background(), services.CreateEscrowInput{
		MerchantID: mustUUID(t, f.merchant), PaymentIntentID: payment.ID,
		Beneficiary: "0xbeneficiary", Arbiter: "0xarbiter", ReleaseAt: releaseAt,
	})
	if err != nil {
		t.Fatalf("create escrow: %v", err)
	}
	return escrow
}

func TestEscrowServiceCreate(t *testing.T) {
	f := newFixture(t)
	payment := createConfirmedPayment(t, f, "0.05", "escrow-create")

	escrow, err := f.escrows.Create(context.Background(), services.CreateEscrowInput{
		MerchantID: mustUUID(t, f.merchant), PaymentIntentID: payment.ID,
		Beneficiary: "0xbeneficiary", Arbiter: "0xarbiter",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if escrow.Status != entities.EscrowStatusFunded {
		t.Fatalf("status = %s, want funded", escrow.Status)
	}
	if escrow.Amount.ToDecimalString() != payment.Amount.ToDecimalString() {
		t.Fatalf("amount = %s, want %s", escrow.Amount.ToDecimalString(), payment.Amount.ToDecimalString())
	}
	if escrow.CryptoAmount.ToDecimalString() != payment.CryptoAmount.ToDecimalString() {
		t.Fatalf("crypto amount = %s", escrow.CryptoAmount.ToDecimalString())
	}
	if !escrow.ReleaseAt.Equal(f.clock.Now()) {
		t.Fatalf("release_at = %s, want %s", escrow.ReleaseAt, f.clock.Now())
	}
}

func TestEscrowServiceCreateValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createConfirmedPayment(t, f, "0.04", "escrow-validation")

	pending, err := f.payments.Create(ctx, services.CreatePaymentInput{
		MerchantID: mustUUID(t, f.merchant), Amount: "0.01", Currency: "ETH",
		Blockchain: "ethereum", Network: "sepolia", Reference: "pending",
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	_, err = f.escrows.Create(ctx, services.CreateEscrowInput{
		MerchantID: mustUUID(t, f.merchant), PaymentIntentID: pending.ID, Beneficiary: "0xbeneficiary",
	})
	requireAppErrorCode(t, err, "unprocessable_entity")

	_, err = f.escrows.Create(ctx, services.CreateEscrowInput{
		MerchantID: uuid.New(), PaymentIntentID: payment.ID, Beneficiary: "0xbeneficiary",
	})
	requireAppErrorCode(t, err, "not_found")

	_, err = f.escrows.Create(ctx, services.CreateEscrowInput{
		MerchantID: mustUUID(t, f.merchant), PaymentIntentID: payment.ID, Beneficiary: "",
	})
	requireAppErrorCode(t, err, "validation_error")

	past := f.clock.Now().Add(-time.Minute)
	_, err = f.escrows.Create(ctx, services.CreateEscrowInput{
		MerchantID: mustUUID(t, f.merchant), PaymentIntentID: payment.ID, Beneficiary: "0xbeneficiary", ReleaseAt: &past,
	})
	requireAppErrorCode(t, err, "validation_error")

	farFuture := f.clock.Now().Add(721 * time.Hour)
	_, err = f.escrows.Create(ctx, services.CreateEscrowInput{
		MerchantID: mustUUID(t, f.merchant), PaymentIntentID: payment.ID, Beneficiary: "0xbeneficiary", ReleaseAt: &farFuture,
	})
	requireAppErrorCode(t, err, "validation_error")
}

func TestEscrowServiceRelease(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createConfirmedPayment(t, f, "0.06", "escrow-release")
	escrow := createEscrowForPayment(t, f, payment, nil)

	released, err := f.escrows.Release(ctx, mustUUID(t, f.merchant), escrow.ID, false, services.EscrowExecutionInput{
		TxHash: "0xrelease", Actor: "merchant",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if released.Status != entities.EscrowStatusReleased || released.TxHash != "0xrelease" {
		t.Fatalf("escrow = %+v", released)
	}

	_, err = f.escrows.Release(ctx, mustUUID(t, f.merchant), escrow.ID, false, services.EscrowExecutionInput{TxHash: "0xagain"})
	requireAppErrorCode(t, err, "unprocessable_entity")
}

func TestEscrowServiceReleaseRules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createConfirmedPayment(t, f, "0.07", "escrow-rules")
	future := f.clock.Now().Add(time.Hour)
	escrow := createEscrowForPayment(t, f, payment, &future)

	_, err := f.escrows.Release(ctx, mustUUID(t, f.merchant), escrow.ID, true, services.EscrowExecutionInput{TxHash: "0xforce"})
	requireAppErrorCode(t, err, "forbidden")

	_, err = f.escrows.Release(ctx, mustUUID(t, f.merchant), escrow.ID, false, services.EscrowExecutionInput{TxHash: "0xearly"})
	requireAppErrorCode(t, err, "unprocessable_entity")
}

func TestEscrowServiceReleaseBroadcastsTransaction(t *testing.T) {
	f := newFixture(t)
	payment := createConfirmedPayment(t, f, "0.08", "escrow-broadcast")
	escrow := createEscrowForPayment(t, f, payment, nil)

	released, err := f.escrows.Release(context.Background(), mustUUID(t, f.merchant), escrow.ID, false, services.EscrowExecutionInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(released.TxHash, "mock-") {
		t.Fatalf("tx hash = %q", released.TxHash)
	}
}

func TestEscrowServiceRefundAndCancel(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createConfirmedPayment(t, f, "0.09", "escrow-refund")

	escrow := createEscrowForPayment(t, f, payment, nil)
	refunded, err := f.escrows.Refund(ctx, mustUUID(t, f.merchant), escrow.ID, services.EscrowExecutionInput{TxHash: "0xrefund"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refunded.Status != entities.EscrowStatusRefunded || refunded.TxHash != "0xrefund" {
		t.Fatalf("escrow = %+v", refunded)
	}
	_, err = f.escrows.Refund(ctx, mustUUID(t, f.merchant), escrow.ID, services.EscrowExecutionInput{TxHash: "0xagain"})
	requireAppErrorCode(t, err, "unprocessable_entity")

	other := createEscrowForPayment(t, f, payment, nil)
	canceled, err := f.escrows.Cancel(ctx, mustUUID(t, f.merchant), other.ID, "merchant", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canceled.Status != entities.EscrowStatusCanceled {
		t.Fatalf("escrow = %+v", canceled)
	}
	_, err = f.escrows.Cancel(ctx, mustUUID(t, f.merchant), other.ID, "merchant", "", "")
	requireAppErrorCode(t, err, "unprocessable_entity")
}

func TestEscrowServiceReleaseDue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createConfirmedPayment(t, f, "0.1", "escrow-due")
	escrow := createEscrowForPayment(t, f, payment, nil)

	released, err := f.escrows.ReleaseDue(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if released != 1 {
		t.Fatalf("released = %d, want 1", released)
	}
	stored, err := f.escrows.Get(ctx, mustUUID(t, f.merchant), escrow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != entities.EscrowStatusReleased {
		t.Fatalf("escrow status = %s", stored.Status)
	}

	released, err = f.escrows.ReleaseDue(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if released != 0 {
		t.Fatalf("released = %d, want 0", released)
	}
}

func TestEscrowServiceGetAndList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createConfirmedPayment(t, f, "0.11", "escrow-get")
	escrow := createEscrowForPayment(t, f, payment, nil)

	stored, err := f.escrows.Get(ctx, mustUUID(t, f.merchant), escrow.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.ID != escrow.ID {
		t.Fatalf("escrow id = %s", stored.ID)
	}

	_, err = f.escrows.Get(ctx, uuid.New(), escrow.ID)
	requireAppErrorCode(t, err, "not_found")

	_, err = f.escrows.Get(ctx, mustUUID(t, f.merchant), uuid.New())
	requireAppErrorCode(t, err, "not_found")

	escrows, total, err := f.escrows.List(ctx, mustUUID(t, f.merchant), ports.Pagination{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(escrows) != 1 {
		t.Fatalf("total = %d escrows = %d", total, len(escrows))
	}
}
