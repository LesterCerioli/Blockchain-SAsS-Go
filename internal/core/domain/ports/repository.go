package ports

import (
	"context"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/google/uuid"
)

type Pagination struct {
	Limit  int
	Offset int
}

type PaymentFilter struct {
	MerchantID uuid.UUID
	Statuses   []entities.PaymentIntentStatus
	Blockchain types.BlockchainType
	Network    types.BlockchainNetwork
	From       *time.Time
	To         *time.Time
	Pagination Pagination
	SortBy     string
	SortOrder  string
}

type MerchantRepository interface {
	Create(ctx context.Context, merchant *entities.Merchant) error
	GetByID(ctx context.Context, id uuid.UUID) (*entities.Merchant, error)
	GetByEmail(ctx context.Context, email string) (*entities.Merchant, error)
	GetByAPIKeyHash(ctx context.Context, hash string) (*entities.Merchant, error)
	GetByAPIKeyPrefix(ctx context.Context, prefix string) (*entities.Merchant, error)
	Update(ctx context.Context, merchant *entities.Merchant) error
	List(ctx context.Context, pagination Pagination) ([]*entities.Merchant, int, error)
}

type PaymentIntentRepository interface {
	Create(ctx context.Context, payment *entities.PaymentIntent) error
	GetByID(ctx context.Context, id uuid.UUID) (*entities.PaymentIntent, error)
	Update(ctx context.Context, payment *entities.PaymentIntent) error
	FindByIdempotencyKey(ctx context.Context, merchantID uuid.UUID, key string) (*entities.PaymentIntent, error)
	List(ctx context.Context, filter PaymentFilter) ([]*entities.PaymentIntent, int, error)
	ListByDepositAddress(ctx context.Context, blockchain types.BlockchainType, network types.BlockchainNetwork, address string) ([]*entities.PaymentIntent, error)
	ListPendingConfirmation(ctx context.Context, limit int) ([]*entities.PaymentIntent, error)
	ListExpirable(ctx context.Context, now time.Time, limit int) ([]*entities.PaymentIntent, error)
}

type EscrowRepository interface {
	Create(ctx context.Context, escrow *entities.Escrow) error
	GetByID(ctx context.Context, id uuid.UUID) (*entities.Escrow, error)
	Update(ctx context.Context, escrow *entities.Escrow) error
	ListByMerchant(ctx context.Context, merchantID uuid.UUID, pagination Pagination) ([]*entities.Escrow, int, error)
	ListReleasable(ctx context.Context, now time.Time, limit int) ([]*entities.Escrow, error)
}

type TransactionRepository interface {
	Create(ctx context.Context, tx *entities.BlockchainTransaction) error
	Update(ctx context.Context, tx *entities.BlockchainTransaction) error
	GetByHash(ctx context.Context, blockchain types.BlockchainType, network types.BlockchainNetwork, hash string) (*entities.BlockchainTransaction, error)
	ExistsByHash(ctx context.Context, blockchain types.BlockchainType, network types.BlockchainNetwork, hash string) (bool, error)
	ListByPayment(ctx context.Context, paymentIntentID uuid.UUID) ([]*entities.BlockchainTransaction, error)
}

type WebhookDelivery struct {
	ID            uuid.UUID              `json:"id"`
	MerchantID    uuid.UUID              `json:"merchant_id"`
	EventType     string                 `json:"event_type"`
	URL           string                 `json:"url"`
	Payload       string                 `json:"payload"`
	Status        string                 `json:"status"`
	Attempts      int                    `json:"attempts"`
	StatusCode    int                    `json:"status_code"`
	LastError     string                 `json:"last_error,omitempty"`
	NextAttemptAt *time.Time             `json:"next_attempt_at,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

type WebhookDeliveryRepository interface {
	Create(ctx context.Context, delivery *WebhookDelivery) error
	Update(ctx context.Context, delivery *WebhookDelivery) error
	GetByID(ctx context.Context, id uuid.UUID) (*WebhookDelivery, error)
	ListByMerchant(ctx context.Context, merchantID uuid.UUID, pagination Pagination) ([]*WebhookDelivery, int, error)
	ListPending(ctx context.Context, now time.Time, limit int) ([]*WebhookDelivery, error)
}

type AuditRepository interface {
	Append(ctx context.Context, log *entities.AuditLog) error
	ListByMerchant(ctx context.Context, merchantID uuid.UUID, pagination Pagination) ([]*entities.AuditLog, int, error)
}

// Repositories bundles all persistence ports used by the application.
type Repositories struct {
	Merchants    MerchantRepository
	Payments     PaymentIntentRepository
	Escrows      EscrowRepository
	Transactions TransactionRepository
	Webhooks     WebhookDeliveryRepository
	Audit        AuditRepository
}
