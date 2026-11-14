package services

import (
	"context"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/google/uuid"
)

type FraudOptions struct {
	MaxAmountUSD         float64
	VelocityWindow       time.Duration
	MaxPaymentsPerWindow int
	RiskThreshold        int
	BlacklistedAddresses []string
}

type RiskAssessment struct {
	Score   int      `json:"score"`
	Reasons []string `json:"reasons,omitempty"`
	Blocked bool     `json:"blocked"`
	Review  bool     `json:"review"`
}

type FraudService struct {
	payments  ports.PaymentIntentRepository
	clock     ports.Clock
	options   FraudOptions
	blacklist map[string]struct{}
}

func NewFraudService(payments ports.PaymentIntentRepository, options FraudOptions, clock ports.Clock) *FraudService {
	if options.VelocityWindow <= 0 {
		options.VelocityWindow = time.Hour
	}
	if options.RiskThreshold <= 0 {
		options.RiskThreshold = 70
	}
	if clock == nil {
		clock = ports.SystemClock{}
	}
	blacklist := make(map[string]struct{}, len(options.BlacklistedAddresses))
	for _, address := range options.BlacklistedAddresses {
		blacklist[strings.ToLower(strings.TrimSpace(address))] = struct{}{}
	}
	return &FraudService{payments: payments, clock: clock, options: options, blacklist: blacklist}
}

func (s *FraudService) Assess(ctx context.Context, merchantID uuid.UUID, amountUSD float64, blockchain types.BlockchainType, network types.BlockchainNetwork, address string) (*RiskAssessment, error) {
	assessment := &RiskAssessment{}

	if _, ok := s.blacklist[strings.ToLower(strings.TrimSpace(address))]; ok {
		assessment.Score = 100
		assessment.Blocked = true
		assessment.Reasons = append(assessment.Reasons, "deposit address is blacklisted")
		return assessment, nil
	}

	if s.options.MaxAmountUSD > 0 {
		if amountUSD >= s.options.MaxAmountUSD {
			assessment.Score = 100
			assessment.Blocked = true
			assessment.Reasons = append(assessment.Reasons, "amount exceeds maximum allowed")
			return assessment, nil
		}
		if amountUSD >= s.options.MaxAmountUSD*0.8 {
			assessment.Score += 30
			assessment.Reasons = append(assessment.Reasons, "amount close to maximum allowed")
		}
	}

	if s.options.MaxPaymentsPerWindow > 0 && s.payments != nil {
		now := s.clock.Now()
		from := now.Add(-s.options.VelocityWindow)
		_, total, err := s.payments.List(ctx, ports.PaymentFilter{
			MerchantID: merchantID,
			Blockchain: blockchain,
			Network:    network,
			From:       &from,
			Pagination: ports.Pagination{Limit: 1},
		})
		if err != nil {
			return nil, err
		}
		if total >= s.options.MaxPaymentsPerWindow {
			assessment.Score += 50
			assessment.Reasons = append(assessment.Reasons, "payment velocity limit exceeded")
		} else if total >= s.options.MaxPaymentsPerWindow/2 {
			assessment.Score += 20
			assessment.Reasons = append(assessment.Reasons, "elevated payment velocity")
		}
	}

	if assessment.Score >= s.options.RiskThreshold {
		assessment.Review = true
	}
	return assessment, nil
}
