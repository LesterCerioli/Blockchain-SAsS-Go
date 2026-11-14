package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/google/uuid"
)

func newEscrowServiceWithRegistry(t *testing.T, f *fixture, registry ports.BlockchainRegistry) *services.EscrowService {
	t.Helper()
	repos := f.store.Repositories()
	return services.NewEscrowService(
		repos.Escrows, repos.Payments, repos.Audit, registry, nil, f.clock, nil,
		services.EscrowServiceOptions{MaxDuration: 720 * time.Hour},
	)
}

func TestEscrowServiceCreateAdapterErrors(t *testing.T) {
	t.Run("registry error", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.04", "escrow-registry")
		service := newEscrowServiceWithRegistry(t, f, stubRegistry{err: entities.ErrUnsupported})

		_, err := service.Create(context.Background(), services.CreateEscrowInput{
			MerchantID: mustUUID(t, f.merchant), PaymentIntentID: payment.ID, Beneficiary: "0xbeneficiary",
		})
		requireAppErrorCode(t, err, "unsupported")
	})

	t.Run("invalid beneficiary", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.05", "escrow-invalid-beneficiary")
		adapter := newStubChainAdapter()
		adapter.validAddress = false
		service := newEscrowServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

		_, err := service.Create(context.Background(), services.CreateEscrowInput{
			MerchantID: mustUUID(t, f.merchant), PaymentIntentID: payment.ID, Beneficiary: "0xbeneficiary",
		})
		requireAppErrorCode(t, err, "validation_error")
	})

	t.Run("address validation error", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.06", "escrow-validate-error")
		adapter := newStubChainAdapter()
		adapter.validateErr = errors.New("validator failed")
		service := newEscrowServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

		_, err := service.Create(context.Background(), services.CreateEscrowInput{
			MerchantID: mustUUID(t, f.merchant), PaymentIntentID: payment.ID, Beneficiary: "0xbeneficiary",
		})
		requireAppErrorCode(t, err, "validation_error")
	})
}

func TestEscrowServiceAccessControl(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	payment := createConfirmedPayment(t, f, "0.07", "escrow-access")
	escrow := createEscrowForPayment(t, f, payment, nil)
	otherMerchant := uuid.New()

	_, err := f.escrows.Get(ctx, otherMerchant, escrow.ID)
	requireAppErrorCode(t, err, "not_found")

	_, err = f.escrows.Release(ctx, otherMerchant, escrow.ID, false, services.EscrowExecutionInput{TxHash: "0xrelease"})
	requireAppErrorCode(t, err, "not_found")

	_, err = f.escrows.Refund(ctx, otherMerchant, escrow.ID, services.EscrowExecutionInput{TxHash: "0xrefund"})
	requireAppErrorCode(t, err, "not_found")

	_, err = f.escrows.Cancel(ctx, otherMerchant, escrow.ID, "actor", "", "")
	requireAppErrorCode(t, err, "not_found")
}

func TestEscrowServiceExecutionErrors(t *testing.T) {
	t.Run("registry error", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.08", "escrow-exec-registry")
		escrow := createEscrowForPayment(t, f, payment, nil)
		service := newEscrowServiceWithRegistry(t, f, stubRegistry{err: entities.ErrUnsupported})

		_, err := service.Release(context.Background(), mustUUID(t, f.merchant), escrow.ID, false, services.EscrowExecutionInput{})
		requireAppErrorCode(t, err, "unsupported")
	})

	t.Run("release send error", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.09", "escrow-exec-send")
		escrow := createEscrowForPayment(t, f, payment, nil)
		adapter := newStubChainAdapter()
		adapter.sendErr = errors.New("broadcast failed")
		service := newEscrowServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

		_, err := service.Release(context.Background(), mustUUID(t, f.merchant), escrow.ID, false, services.EscrowExecutionInput{})
		requireAppErrorCode(t, err, "service_unavailable")
	})

	t.Run("refund send error", func(t *testing.T) {
		f := newFixture(t)
		payment := createConfirmedPayment(t, f, "0.1", "escrow-refund-send")
		escrow := createEscrowForPayment(t, f, payment, nil)
		adapter := newStubChainAdapter()
		adapter.sendErr = errors.New("broadcast failed")
		service := newEscrowServiceWithRegistry(t, f, stubRegistry{adapter: adapter})

		_, err := service.Refund(context.Background(), mustUUID(t, f.merchant), escrow.ID, services.EscrowExecutionInput{})
		requireAppErrorCode(t, err, "service_unavailable")
	})
}
