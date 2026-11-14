package entities

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type MerchantStatus string

const (
	MerchantStatusPending   MerchantStatus = "pending"
	MerchantStatusActive    MerchantStatus = "active"
	MerchantStatusSuspended MerchantStatus = "suspended"
	MerchantStatusClosed    MerchantStatus = "closed"
)

type Merchant struct {
	ID            uuid.UUID      `json:"id"`
	Name          string         `json:"name"`
	Email         string         `json:"email"`
	APIKeyHash    string         `json:"-"`
	APIKeyPrefix  string         `json:"api_key_prefix"`
	WebhookURL    string         `json:"webhook_url,omitempty"`
	WebhookSecret string         `json:"-"`
	Status        MerchantStatus `json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

func NewMerchant(name, email, apiKeyHash, apiKeyPrefix string, now time.Time) *Merchant {
	return &Merchant{
		ID:           uuid.New(),
		Name:         strings.TrimSpace(name),
		Email:        strings.ToLower(strings.TrimSpace(email)),
		APIKeyHash:   apiKeyHash,
		APIKeyPrefix: apiKeyPrefix,
		Status:       MerchantStatusActive,
		CreatedAt:    now.UTC(),
		UpdatedAt:    now.UTC(),
	}
}

func (m *Merchant) CanAcceptPayments() bool {
	return m != nil && m.Status == MerchantStatusActive
}

func (m *Merchant) Activate(now time.Time) {
	m.Status = MerchantStatusActive
	m.UpdatedAt = now.UTC()
}

func (m *Merchant) Suspend(now time.Time) {
	m.Status = MerchantStatusSuspended
	m.UpdatedAt = now.UTC()
}

func (m *Merchant) Close(now time.Time) {
	m.Status = MerchantStatusClosed
	m.UpdatedAt = now.UTC()
}

func (m *Merchant) SetWebhook(url, secret string, now time.Time) {
	m.WebhookURL = strings.TrimSpace(url)
	m.WebhookSecret = secret
	m.UpdatedAt = now.UTC()
}

func (m *Merchant) HasWebhook() bool {
	return m != nil && m.WebhookURL != ""
}
