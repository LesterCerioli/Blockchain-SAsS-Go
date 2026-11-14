package services

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
)

type ValidationService struct {
	minExpiry time.Duration
	maxExpiry time.Duration
}

func NewValidationService() *ValidationService {
	return &ValidationService{minExpiry: 60 * time.Second, maxExpiry: 24 * time.Hour}
}

func (v *ValidationService) ValidateMerchant(name, email string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("merchant name is required")
	}
	if len(name) > 200 {
		return fmt.Errorf("merchant name is too long")
	}
	if !utils.ValidateEmail(email) {
		return fmt.Errorf("invalid merchant email")
	}
	return nil
}

func (v *ValidationService) ParseAmount(amount, currency string) (*types.Amount, error) {
	currency = utils.NormalizeCurrency(currency)
	if currency == "" {
		return nil, fmt.Errorf("currency is required")
	}
	decimals, ok := utils.GetCurrencyDecimals(currency)
	if !ok {
		return nil, fmt.Errorf("unsupported currency: %s", currency)
	}
	if err := utils.ValidateAmount(amount); err != nil {
		return nil, err
	}
	parsed, err := types.NewAmountFromDecimal(strings.TrimSpace(amount), decimals)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}
	if parsed.IsZero() {
		return nil, fmt.Errorf("amount must be greater than zero")
	}
	return parsed, nil
}

func (v *ValidationService) ResolveChainNetwork(blockchain, network string) (types.BlockchainType, types.BlockchainNetwork, error) {
	chain, err := utils.NormalizeBlockchain(blockchain)
	if err != nil {
		return "", "", err
	}
	resolvedNetwork, err := utils.NormalizeNetwork(chain, network)
	if err != nil {
		return "", "", err
	}
	return chain, resolvedNetwork, nil
}

func (v *ValidationService) Expiry(seconds int, fallback time.Duration) (time.Duration, error) {
	if seconds == 0 {
		seconds = int(fallback.Seconds())
	}
	expiry := time.Duration(seconds) * time.Second
	if expiry < v.minExpiry {
		return 0, fmt.Errorf("expiresIn must be at least %d seconds", int(v.minExpiry.Seconds()))
	}
	if expiry > v.maxExpiry {
		return 0, fmt.Errorf("expiresIn cannot exceed %d seconds", int(v.maxExpiry.Seconds()))
	}
	return expiry, nil
}

func (v *ValidationService) ValidateWebhookURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid webhook url: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("webhook url must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("webhook url must include a host")
	}
	return nil
}

func (v *ValidationService) IsCryptoCurrency(currency string) bool {
	switch utils.NormalizeCurrency(currency) {
	case "ETH", "BTC", "SOL", "ADA", "USDT", "USDC", "DAI", "BNB", "MATIC":
		return true
	default:
		return false
	}
}
