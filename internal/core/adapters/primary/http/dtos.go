package http

import (
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
)

type RegisterMerchantRequest struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	WebhookURL string `json:"webhook_url,omitempty"`
}

type MerchantResponse struct {
	ID           string                  `json:"id"`
	Name         string                  `json:"name"`
	Email        string                  `json:"email"`
	APIKeyPrefix string                  `json:"api_key_prefix"`
	WebhookURL   string                  `json:"webhook_url,omitempty"`
	Status       entities.MerchantStatus `json:"status"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
}

type RegisterMerchantResponse struct {
	Merchant      MerchantResponse `json:"merchant"`
	APIKey        string           `json:"api_key"`
	WebhookSecret string           `json:"webhook_secret"`
}

type LoginRequest struct {
	APIKey string `json:"api_key"`
}

type TokenResponse struct {
	Token     string           `json:"token"`
	TokenType string           `json:"token_type"`
	ExpiresAt time.Time        `json:"expires_at"`
	Merchant  MerchantResponse `json:"merchant"`
}

type UpdateWebhookRequest struct {
	WebhookURL string `json:"webhook_url"`
}

type UpdateWebhookResponse struct {
	Merchant      MerchantResponse `json:"merchant"`
	WebhookSecret string           `json:"webhook_secret"`
}

type RotateKeyResponse struct {
	Merchant MerchantResponse `json:"merchant"`
	APIKey   string           `json:"api_key"`
}

type CreatePaymentIntentRequest struct {
	Amount         string            `json:"amount"`
	Currency       string            `json:"currency"`
	CryptoCurrency string            `json:"crypto_currency,omitempty"`
	Blockchain     string            `json:"blockchain,omitempty"`
	Network        string            `json:"network,omitempty"`
	MerchantID     string            `json:"merchantId,omitempty"`
	Reference      string            `json:"reference,omitempty"`
	Description    string            `json:"description,omitempty"`
	ExpiresIn      int               `json:"expiresIn,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type PaymentIntentResponse struct {
	ID                    string                       `json:"id"`
	MerchantID            string                       `json:"merchant_id"`
	Reference             string                       `json:"reference,omitempty"`
	Description           string                       `json:"description,omitempty"`
	Amount                string                       `json:"amount"`
	Currency              string                       `json:"currency"`
	CryptoAmount          string                       `json:"crypto_amount"`
	CryptoCurrency        string                       `json:"crypto_currency"`
	Blockchain            types.BlockchainType         `json:"blockchain"`
	Network               types.BlockchainNetwork      `json:"network"`
	DepositAddress        string                       `json:"deposit_address"`
	Status                entities.PaymentIntentStatus `json:"status"`
	ExpiresAt             time.Time                    `json:"expires_at"`
	TxHash                string                       `json:"tx_hash,omitempty"`
	Confirmations         int                          `json:"confirmations"`
	RequiredConfirmations int                          `json:"required_confirmations"`
	PaidAmount            string                       `json:"paid_amount,omitempty"`
	FeeAmount             string                       `json:"fee_amount,omitempty"`
	RefundTxHash          string                       `json:"refund_tx_hash,omitempty"`
	RiskScore             int                          `json:"risk_score"`
	Metadata              map[string]string            `json:"metadata,omitempty"`
	CreatedAt             time.Time                    `json:"created_at"`
	UpdatedAt             time.Time                    `json:"updated_at"`
	CompletedAt           *time.Time                   `json:"completed_at,omitempty"`
}

type ConfirmPaymentRequest struct {
	TxHash string `json:"tx_hash"`
}

type RefundPaymentRequest struct {
	DestinationAddress string `json:"destination_address,omitempty"`
	Amount             string `json:"amount,omitempty"`
	SignedTransaction  string `json:"signed_transaction,omitempty"`
	TxHash             string `json:"tx_hash,omitempty"`
}

type CreateEscrowRequest struct {
	PaymentIntentID string     `json:"payment_intent_id"`
	Beneficiary     string     `json:"beneficiary"`
	Arbiter         string     `json:"arbiter,omitempty"`
	ReleaseAt       *time.Time `json:"release_at,omitempty"`
}

type ExecuteEscrowRequest struct {
	TxHash            string `json:"tx_hash,omitempty"`
	SignedTransaction string `json:"signed_transaction,omitempty"`
	Force             bool   `json:"force,omitempty"`
}

type EscrowResponse struct {
	ID              string                  `json:"id"`
	MerchantID      string                  `json:"merchant_id"`
	PaymentIntentID string                  `json:"payment_intent_id"`
	Amount          string                  `json:"amount"`
	Currency        string                  `json:"currency"`
	CryptoAmount    string                  `json:"crypto_amount"`
	CryptoCurrency  string                  `json:"crypto_currency"`
	Blockchain      types.BlockchainType    `json:"blockchain"`
	Network         types.BlockchainNetwork `json:"network"`
	Beneficiary     string                  `json:"beneficiary"`
	Arbiter         string                  `json:"arbiter,omitempty"`
	ReleaseAt       time.Time               `json:"release_at"`
	Status          entities.EscrowStatus   `json:"status"`
	ReleasedAt      *time.Time              `json:"released_at,omitempty"`
	RefundedAt      *time.Time              `json:"refunded_at,omitempty"`
	TxHash          string                  `json:"tx_hash,omitempty"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type RateResponse struct {
	Base      string    `json:"base"`
	Quote     string    `json:"quote"`
	Rate      float64   `json:"rate"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
}

type InboundWebhookRequest struct {
	Blockchain    string `json:"blockchain"`
	Network       string `json:"network"`
	TxHash        string `json:"tx_hash"`
	From          string `json:"from,omitempty"`
	To            string `json:"to"`
	Amount        string `json:"amount"`
	Fee           string `json:"fee,omitempty"`
	Confirmations int    `json:"confirmations"`
	Status        string `json:"status,omitempty"`
	BlockNumber   int64  `json:"block_number,omitempty"`
	BlockHash     string `json:"block_hash,omitempty"`
}

type WebhookDeliveryResponse struct {
	ID            string                 `json:"id"`
	EventType     string                 `json:"event_type"`
	URL           string                 `json:"url"`
	Status        string                 `json:"status"`
	Attempts      int                    `json:"attempts"`
	StatusCode    int                    `json:"status_code"`
	LastError     string                 `json:"last_error,omitempty"`
	NextAttemptAt *time.Time             `json:"next_attempt_at,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

func newMerchantResponse(merchant *entities.Merchant) MerchantResponse {
	return MerchantResponse{
		ID: merchant.ID.String(), Name: merchant.Name, Email: merchant.Email,
		APIKeyPrefix: merchant.APIKeyPrefix, WebhookURL: merchant.WebhookURL,
		Status: merchant.Status, CreatedAt: merchant.CreatedAt, UpdatedAt: merchant.UpdatedAt,
	}
}

func newPaymentIntentResponse(payment *entities.PaymentIntent) PaymentIntentResponse {
	return PaymentIntentResponse{
		ID: payment.ID.String(), MerchantID: payment.MerchantID.String(),
		Reference: payment.Reference, Description: payment.Description,
		Amount: decimalString(payment.Amount), Currency: payment.Currency,
		CryptoAmount: decimalString(payment.CryptoAmount), CryptoCurrency: payment.CryptoCurrency,
		Blockchain: payment.Blockchain, Network: payment.Network,
		DepositAddress: payment.DepositAddress, Status: payment.Status,
		ExpiresAt: payment.ExpiresAt, TxHash: payment.TxHash, Confirmations: payment.Confirmations,
		RequiredConfirmations: payment.RequiredConfirmations,
		PaidAmount:            decimalString(payment.PaidAmount), FeeAmount: decimalString(payment.FeeAmount),
		RefundTxHash: payment.RefundTxHash, RiskScore: payment.RiskScore, Metadata: payment.Metadata,
		CreatedAt: payment.CreatedAt, UpdatedAt: payment.UpdatedAt, CompletedAt: payment.CompletedAt,
	}
}

func newEscrowResponse(escrow *entities.Escrow) EscrowResponse {
	return EscrowResponse{
		ID: escrow.ID.String(), MerchantID: escrow.MerchantID.String(),
		PaymentIntentID: escrow.PaymentIntentID.String(),
		Amount:          decimalString(escrow.Amount), Currency: escrow.Currency,
		CryptoAmount: decimalString(escrow.CryptoAmount), CryptoCurrency: escrow.CryptoCurrency,
		Blockchain: escrow.Blockchain, Network: escrow.Network,
		Beneficiary: escrow.Beneficiary, Arbiter: escrow.Arbiter, ReleaseAt: escrow.ReleaseAt,
		Status: escrow.Status, ReleasedAt: escrow.ReleasedAt, RefundedAt: escrow.RefundedAt,
		TxHash: escrow.TxHash, CreatedAt: escrow.CreatedAt, UpdatedAt: escrow.UpdatedAt,
	}
}

func newWebhookDeliveryResponse(delivery *ports.WebhookDelivery) WebhookDeliveryResponse {
	return WebhookDeliveryResponse{
		ID: delivery.ID.String(), EventType: delivery.EventType, URL: delivery.URL,
		Status: delivery.Status, Attempts: delivery.Attempts, StatusCode: delivery.StatusCode,
		LastError: delivery.LastError, NextAttemptAt: delivery.NextAttemptAt,
		CreatedAt: delivery.CreatedAt, UpdatedAt: delivery.UpdatedAt, Metadata: delivery.Metadata,
	}
}

func decimalString(amount *types.Amount) string {
	if amount == nil {
		return ""
	}
	return amount.ToDecimalString()
}
