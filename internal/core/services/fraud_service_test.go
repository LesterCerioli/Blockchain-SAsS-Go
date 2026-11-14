package services_test

import (
	"context"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/repositories"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/google/uuid"
)

func seedFraudPayments(t *testing.T, repo ports.PaymentIntentRepository, merchantID uuid.UUID, count int, now time.Time) {
	t.Helper()
	for i := 0; i < count; i++ {
		payment := entities.NewPaymentIntent(merchantID, now.Add(-time.Duration(i+1)*time.Minute))
		payment.Blockchain = types.BlockchainEthereum
		payment.Network = types.NetworkEthereumSepolia
		if err := repo.Create(context.Background(), payment); err != nil {
			t.Fatalf("seed payment: %v", err)
		}
	}
}

func TestFraudServiceBlacklistedAddress(t *testing.T) {
	service := services.NewFraudService(nil, services.FraudOptions{BlacklistedAddresses: []string{" 0xBAD "}}, nil)

	assessment, err := service.Assess(context.Background(), uuid.New(), 1, types.BlockchainEthereum, types.NetworkEthereumSepolia, "0xbad")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !assessment.Blocked || assessment.Score != 100 {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestFraudServiceAmountLimits(t *testing.T) {
	service := services.NewFraudService(nil, services.FraudOptions{MaxAmountUSD: 100, RiskThreshold: 20}, nil)

	blocked, err := service.Assess(context.Background(), uuid.New(), 100, types.BlockchainEthereum, types.NetworkEthereumSepolia, "0xabc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !blocked.Blocked {
		t.Fatalf("expected amount at limit to be blocked: %+v", blocked)
	}

	review, err := service.Assess(context.Background(), uuid.New(), 80, types.BlockchainEthereum, types.NetworkEthereumSepolia, "0xabc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if review.Blocked || !review.Review || review.Score != 30 {
		t.Fatalf("assessment = %+v", review)
	}
}

func TestFraudServiceVelocity(t *testing.T) {
	store := repositories.NewMemoryRepository()
	repos := store.Repositories()
	merchantID := uuid.New()
	now := time.Now().UTC()

	seedFraudPayments(t, repos.Payments, merchantID, 4, now)
	service := services.NewFraudService(repos.Payments, services.FraudOptions{
		VelocityWindow: time.Hour, MaxPaymentsPerWindow: 4, RiskThreshold: 40,
	}, fixedClock{now: now})

	assessment, err := service.Assess(context.Background(), merchantID, 1, types.BlockchainEthereum, types.NetworkEthereumSepolia, "0xabc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !assessment.Review || assessment.Score != 50 {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestFraudServiceElevatedVelocity(t *testing.T) {
	store := repositories.NewMemoryRepository()
	repos := store.Repositories()
	merchantID := uuid.New()
	now := time.Now().UTC()

	seedFraudPayments(t, repos.Payments, merchantID, 2, now)
	service := services.NewFraudService(repos.Payments, services.FraudOptions{
		VelocityWindow: time.Hour, MaxPaymentsPerWindow: 4, RiskThreshold: 20,
	}, fixedClock{now: now})

	assessment, err := service.Assess(context.Background(), merchantID, 1, types.BlockchainEthereum, types.NetworkEthereumSepolia, "0xabc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !assessment.Review || assessment.Score != 20 {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestFraudServiceWithoutPaymentRepository(t *testing.T) {
	service := services.NewFraudService(nil, services.FraudOptions{
		VelocityWindow: time.Hour, MaxPaymentsPerWindow: 1,
	}, nil)

	assessment, err := service.Assess(context.Background(), uuid.New(), 1, types.BlockchainEthereum, types.NetworkEthereumSepolia, "0xabc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if assessment.Score != 0 || assessment.Blocked || assessment.Review {
		t.Fatalf("assessment = %+v", assessment)
	}
}
