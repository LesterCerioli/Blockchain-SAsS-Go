package entities

import (
	"time"

	"github.com/google/uuid"
)

type AuditLog struct {
	ID           uuid.UUID              `json:"id"`
	MerchantID   uuid.UUID              `json:"merchant_id,omitempty"`
	Actor        string                 `json:"actor"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Details      map[string]interface{} `json:"details,omitempty"`
	IPAddress    string                 `json:"ip_address,omitempty"`
	UserAgent    string                 `json:"user_agent,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

func NewAuditLog(now time.Time) *AuditLog {
	return &AuditLog{
		ID:        uuid.New(),
		Details:   map[string]interface{}{},
		CreatedAt: now.UTC(),
	}
}
