package repositories

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/google/uuid"
)

type MemoryStore struct {
	mu        sync.RWMutex
	merchants map[uuid.UUID]*entities.Merchant
	payments  map[uuid.UUID]*entities.PaymentIntent
	escrows   map[uuid.UUID]*entities.Escrow
	txByHash  map[string]*entities.BlockchainTransaction
	webhooks  map[uuid.UUID]*ports.WebhookDelivery
	auditLogs []*entities.AuditLog
}

func NewMemoryRepository() *MemoryStore {
	return &MemoryStore{
		merchants: make(map[uuid.UUID]*entities.Merchant),
		payments:  make(map[uuid.UUID]*entities.PaymentIntent),
		escrows:   make(map[uuid.UUID]*entities.Escrow),
		txByHash:  make(map[string]*entities.BlockchainTransaction),
		webhooks:  make(map[uuid.UUID]*ports.WebhookDelivery),
	}
}

func (s *MemoryStore) Repositories() ports.Repositories {
	return ports.Repositories{
		Merchants:    &MemoryMerchantRepository{store: s},
		Payments:     &MemoryPaymentRepository{store: s},
		Escrows:      &MemoryEscrowRepository{store: s},
		Transactions: &MemoryTransactionRepository{store: s},
		Webhooks:     &MemoryWebhookRepository{store: s},
		Audit:        &MemoryAuditRepository{store: s},
	}
}

func cloneAmount(a *types.Amount) *types.Amount {
	if a == nil {
		return nil
	}
	if a.Value == nil {
		return &types.Amount{Decimals: a.Decimals}
	}
	return &types.Amount{Value: new(big.Int).Set(a.Value), Decimals: a.Decimals}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneMerchant(m *entities.Merchant) *entities.Merchant {
	if m == nil {
		return nil
	}
	c := *m
	return &c
}

func clonePayment(p *entities.PaymentIntent) *entities.PaymentIntent {
	if p == nil {
		return nil
	}
	c := *p
	c.Amount = cloneAmount(p.Amount)
	c.CryptoAmount = cloneAmount(p.CryptoAmount)
	c.PaidAmount = cloneAmount(p.PaidAmount)
	c.FeeAmount = cloneAmount(p.FeeAmount)
	c.Metadata = cloneStringMap(p.Metadata)
	if p.CompletedAt != nil {
		t := *p.CompletedAt
		c.CompletedAt = &t
	}
	return &c
}

func cloneEscrow(e *entities.Escrow) *entities.Escrow {
	if e == nil {
		return nil
	}
	c := *e
	c.Amount = cloneAmount(e.Amount)
	c.CryptoAmount = cloneAmount(e.CryptoAmount)
	if e.ReleasedAt != nil {
		t := *e.ReleasedAt
		c.ReleasedAt = &t
	}
	if e.RefundedAt != nil {
		t := *e.RefundedAt
		c.RefundedAt = &t
	}
	return &c
}

func cloneTransaction(t *entities.BlockchainTransaction) *entities.BlockchainTransaction {
	if t == nil {
		return nil
	}
	c := *t
	c.Amount = cloneAmount(t.Amount)
	c.Fee = cloneAmount(t.Fee)
	c.Metadata = cloneStringMap(t.Metadata)
	return &c
}

func cloneWebhook(w *ports.WebhookDelivery) *ports.WebhookDelivery {
	if w == nil {
		return nil
	}
	c := *w
	if w.NextAttemptAt != nil {
		t := *w.NextAttemptAt
		c.NextAttemptAt = &t
	}
	return &c
}

func cloneAudit(a *entities.AuditLog) *entities.AuditLog {
	if a == nil {
		return nil
	}
	c := *a
	return &c
}

func txKey(chain types.BlockchainType, network types.BlockchainNetwork, hash string) string {
	return fmt.Sprintf("%s|%s|%s", chain, network, strings.ToLower(strings.TrimSpace(hash)))
}

func sliceBounds(limit, offset, total int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return offset, end
}

type MemoryMerchantRepository struct{ store *MemoryStore }

func (r *MemoryMerchantRepository) Create(_ context.Context, merchant *entities.Merchant) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	for _, existing := range r.store.merchants {
		if strings.EqualFold(existing.Email, merchant.Email) {
			return fmt.Errorf("%w: merchant email already registered", entities.ErrConflict)
		}
		if existing.APIKeyHash == merchant.APIKeyHash {
			return fmt.Errorf("%w: api key already registered", entities.ErrConflict)
		}
	}
	r.store.merchants[merchant.ID] = cloneMerchant(merchant)
	return nil
}

func (r *MemoryMerchantRepository) GetByID(_ context.Context, id uuid.UUID) (*entities.Merchant, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	merchant, ok := r.store.merchants[id]
	if !ok {
		return nil, fmt.Errorf("%w: merchant %s", entities.ErrNotFound, id)
	}
	return cloneMerchant(merchant), nil
}

func (r *MemoryMerchantRepository) GetByEmail(_ context.Context, email string) (*entities.Merchant, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	for _, merchant := range r.store.merchants {
		if strings.EqualFold(merchant.Email, email) {
			return cloneMerchant(merchant), nil
		}
	}
	return nil, fmt.Errorf("%w: merchant with email %s", entities.ErrNotFound, email)
}

func (r *MemoryMerchantRepository) GetByAPIKeyHash(_ context.Context, hash string) (*entities.Merchant, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	for _, merchant := range r.store.merchants {
		if merchant.APIKeyHash == hash {
			return cloneMerchant(merchant), nil
		}
	}
	return nil, fmt.Errorf("%w: merchant for api key", entities.ErrNotFound)
}

func (r *MemoryMerchantRepository) GetByAPIKeyPrefix(_ context.Context, prefix string) (*entities.Merchant, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	for _, merchant := range r.store.merchants {
		if merchant.APIKeyPrefix == prefix {
			return cloneMerchant(merchant), nil
		}
	}
	return nil, fmt.Errorf("%w: merchant for api key prefix", entities.ErrNotFound)
}

func (r *MemoryMerchantRepository) Update(_ context.Context, merchant *entities.Merchant) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.merchants[merchant.ID]; !ok {
		return fmt.Errorf("%w: merchant %s", entities.ErrNotFound, merchant.ID)
	}
	r.store.merchants[merchant.ID] = cloneMerchant(merchant)
	return nil
}

func (r *MemoryMerchantRepository) List(_ context.Context, pagination ports.Pagination) ([]*entities.Merchant, int, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	merchants := make([]*entities.Merchant, 0, len(r.store.merchants))
	for _, merchant := range r.store.merchants {
		merchants = append(merchants, cloneMerchant(merchant))
	}
	sort.Slice(merchants, func(i, j int) bool { return merchants[i].CreatedAt.After(merchants[j].CreatedAt) })
	total := len(merchants)
	start, end := sliceBounds(pagination.Limit, pagination.Offset, total)
	return merchants[start:end], total, nil
}

type MemoryPaymentRepository struct{ store *MemoryStore }

func (r *MemoryPaymentRepository) Create(_ context.Context, payment *entities.PaymentIntent) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.payments[payment.ID]; ok {
		return fmt.Errorf("%w: payment intent %s", entities.ErrConflict, payment.ID)
	}
	if payment.IdempotencyKey != "" {
		for _, existing := range r.store.payments {
			if existing.MerchantID == payment.MerchantID && existing.IdempotencyKey == payment.IdempotencyKey {
				return fmt.Errorf("%w: idempotency key already used", entities.ErrConflict)
			}
		}
	}
	r.store.payments[payment.ID] = clonePayment(payment)
	return nil
}

func (r *MemoryPaymentRepository) GetByID(_ context.Context, id uuid.UUID) (*entities.PaymentIntent, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	payment, ok := r.store.payments[id]
	if !ok {
		return nil, fmt.Errorf("%w: payment intent %s", entities.ErrNotFound, id)
	}
	return clonePayment(payment), nil
}

func (r *MemoryPaymentRepository) Update(_ context.Context, payment *entities.PaymentIntent) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.payments[payment.ID]; !ok {
		return fmt.Errorf("%w: payment intent %s", entities.ErrNotFound, payment.ID)
	}
	r.store.payments[payment.ID] = clonePayment(payment)
	return nil
}

func (r *MemoryPaymentRepository) FindByIdempotencyKey(_ context.Context, merchantID uuid.UUID, key string) (*entities.PaymentIntent, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	for _, payment := range r.store.payments {
		if payment.MerchantID == merchantID && payment.IdempotencyKey == key {
			return clonePayment(payment), nil
		}
	}
	return nil, fmt.Errorf("%w: payment intent with idempotency key", entities.ErrNotFound)
}

func (r *MemoryPaymentRepository) List(_ context.Context, filter ports.PaymentFilter) ([]*entities.PaymentIntent, int, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*entities.PaymentIntent, 0)
	for _, payment := range r.store.payments {
		if filter.MerchantID != uuid.Nil && payment.MerchantID != filter.MerchantID {
			continue
		}
		if filter.Blockchain != "" && payment.Blockchain != filter.Blockchain {
			continue
		}
		if filter.Network != "" && payment.Network != filter.Network {
			continue
		}
		if len(filter.Statuses) > 0 {
			matched := false
			for _, status := range filter.Statuses {
				if payment.Status == status {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if filter.From != nil && payment.CreatedAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && payment.CreatedAt.After(*filter.To) {
			continue
		}
		result = append(result, clonePayment(payment))
	}
	sort.Slice(result, func(i, j int) bool {
		if filter.SortBy == "amount" && result[i].Amount != nil && result[j].Amount != nil {
			cmp := result[i].Amount.Cmp(result[j].Amount)
			if filter.SortOrder == "asc" {
				return cmp < 0
			}
			return cmp > 0
		}
		if filter.SortOrder == "asc" {
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	total := len(result)
	start, end := sliceBounds(filter.Pagination.Limit, filter.Pagination.Offset, total)
	return result[start:end], total, nil
}

func (r *MemoryPaymentRepository) ListByDepositAddress(_ context.Context, blockchain types.BlockchainType, network types.BlockchainNetwork, address string) ([]*entities.PaymentIntent, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*entities.PaymentIntent, 0)
	for _, payment := range r.store.payments {
		if payment.Blockchain == blockchain && payment.Network == network &&
			strings.EqualFold(payment.DepositAddress, address) && payment.CanAcceptPayment() {
			result = append(result, clonePayment(payment))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (r *MemoryPaymentRepository) ListPendingConfirmation(_ context.Context, limit int) ([]*entities.PaymentIntent, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*entities.PaymentIntent, 0)
	for _, payment := range r.store.payments {
		if payment.TxHash != "" && payment.CanAcceptPayment() {
			result = append(result, clonePayment(payment))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.Before(result[j].UpdatedAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *MemoryPaymentRepository) ListExpirable(_ context.Context, now time.Time, limit int) ([]*entities.PaymentIntent, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*entities.PaymentIntent, 0)
	for _, payment := range r.store.payments {
		if (payment.Status == entities.PaymentStatusRequiresPayment || payment.Status == entities.PaymentStatusUnderReview) && payment.IsExpired(now) {
			result = append(result, clonePayment(payment))
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type MemoryEscrowRepository struct{ store *MemoryStore }

func (r *MemoryEscrowRepository) Create(_ context.Context, escrow *entities.Escrow) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.escrows[escrow.ID]; ok {
		return fmt.Errorf("%w: escrow %s", entities.ErrConflict, escrow.ID)
	}
	r.store.escrows[escrow.ID] = cloneEscrow(escrow)
	return nil
}

func (r *MemoryEscrowRepository) GetByID(_ context.Context, id uuid.UUID) (*entities.Escrow, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	escrow, ok := r.store.escrows[id]
	if !ok {
		return nil, fmt.Errorf("%w: escrow %s", entities.ErrNotFound, id)
	}
	return cloneEscrow(escrow), nil
}

func (r *MemoryEscrowRepository) Update(_ context.Context, escrow *entities.Escrow) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.escrows[escrow.ID]; !ok {
		return fmt.Errorf("%w: escrow %s", entities.ErrNotFound, escrow.ID)
	}
	r.store.escrows[escrow.ID] = cloneEscrow(escrow)
	return nil
}

func (r *MemoryEscrowRepository) ListByMerchant(_ context.Context, merchantID uuid.UUID, pagination ports.Pagination) ([]*entities.Escrow, int, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*entities.Escrow, 0)
	for _, escrow := range r.store.escrows {
		if escrow.MerchantID == merchantID {
			result = append(result, cloneEscrow(escrow))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	total := len(result)
	start, end := sliceBounds(pagination.Limit, pagination.Offset, total)
	return result[start:end], total, nil
}

func (r *MemoryEscrowRepository) ListReleasable(_ context.Context, now time.Time, limit int) ([]*entities.Escrow, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*entities.Escrow, 0)
	for _, escrow := range r.store.escrows {
		if escrow.Status == entities.EscrowStatusFunded && escrow.CanRelease(now, false) {
			result = append(result, cloneEscrow(escrow))
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type MemoryTransactionRepository struct{ store *MemoryStore }

func (r *MemoryTransactionRepository) Create(_ context.Context, tx *entities.BlockchainTransaction) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	key := txKey(tx.Blockchain, tx.Network, tx.TxHash)
	if _, ok := r.store.txByHash[key]; ok {
		return fmt.Errorf("%w: transaction %s", entities.ErrConflict, tx.TxHash)
	}
	r.store.txByHash[key] = cloneTransaction(tx)
	return nil
}

func (r *MemoryTransactionRepository) Update(_ context.Context, tx *entities.BlockchainTransaction) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	key := txKey(tx.Blockchain, tx.Network, tx.TxHash)
	if _, ok := r.store.txByHash[key]; !ok {
		return fmt.Errorf("%w: transaction %s", entities.ErrNotFound, tx.TxHash)
	}
	r.store.txByHash[key] = cloneTransaction(tx)
	return nil
}

func (r *MemoryTransactionRepository) GetByHash(_ context.Context, blockchain types.BlockchainType, network types.BlockchainNetwork, hash string) (*entities.BlockchainTransaction, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	tx, ok := r.store.txByHash[txKey(blockchain, network, hash)]
	if !ok {
		return nil, fmt.Errorf("%w: transaction %s", entities.ErrNotFound, hash)
	}
	return cloneTransaction(tx), nil
}

func (r *MemoryTransactionRepository) ExistsByHash(_ context.Context, blockchain types.BlockchainType, network types.BlockchainNetwork, hash string) (bool, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	_, ok := r.store.txByHash[txKey(blockchain, network, hash)]
	return ok, nil
}

func (r *MemoryTransactionRepository) ListByPayment(_ context.Context, paymentIntentID uuid.UUID) ([]*entities.BlockchainTransaction, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*entities.BlockchainTransaction, 0)
	for _, tx := range r.store.txByHash {
		if tx.PaymentIntentID == paymentIntentID {
			result = append(result, cloneTransaction(tx))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

type MemoryWebhookRepository struct{ store *MemoryStore }

func (r *MemoryWebhookRepository) Create(_ context.Context, delivery *ports.WebhookDelivery) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.webhooks[delivery.ID]; ok {
		return fmt.Errorf("%w: webhook delivery %s", entities.ErrConflict, delivery.ID)
	}
	r.store.webhooks[delivery.ID] = cloneWebhook(delivery)
	return nil
}

func (r *MemoryWebhookRepository) Update(_ context.Context, delivery *ports.WebhookDelivery) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.webhooks[delivery.ID]; !ok {
		return fmt.Errorf("%w: webhook delivery %s", entities.ErrNotFound, delivery.ID)
	}
	r.store.webhooks[delivery.ID] = cloneWebhook(delivery)
	return nil
}

func (r *MemoryWebhookRepository) GetByID(_ context.Context, id uuid.UUID) (*ports.WebhookDelivery, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	delivery, ok := r.store.webhooks[id]
	if !ok {
		return nil, fmt.Errorf("%w: webhook delivery %s", entities.ErrNotFound, id)
	}
	return cloneWebhook(delivery), nil
}

func (r *MemoryWebhookRepository) ListByMerchant(_ context.Context, merchantID uuid.UUID, pagination ports.Pagination) ([]*ports.WebhookDelivery, int, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*ports.WebhookDelivery, 0)
	for _, delivery := range r.store.webhooks {
		if delivery.MerchantID == merchantID {
			result = append(result, cloneWebhook(delivery))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	total := len(result)
	start, end := sliceBounds(pagination.Limit, pagination.Offset, total)
	return result[start:end], total, nil
}

func (r *MemoryWebhookRepository) ListPending(_ context.Context, now time.Time, limit int) ([]*ports.WebhookDelivery, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*ports.WebhookDelivery, 0)
	for _, delivery := range r.store.webhooks {
		if delivery.Status == "pending" || delivery.Status == "retrying" {
			if delivery.NextAttemptAt == nil || !delivery.NextAttemptAt.After(now) {
				result = append(result, cloneWebhook(delivery))
			}
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type MemoryAuditRepository struct{ store *MemoryStore }

func (r *MemoryAuditRepository) Append(_ context.Context, entry *entities.AuditLog) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.auditLogs = append(r.store.auditLogs, cloneAudit(entry))
	return nil
}

func (r *MemoryAuditRepository) ListByMerchant(_ context.Context, merchantID uuid.UUID, pagination ports.Pagination) ([]*entities.AuditLog, int, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	result := make([]*entities.AuditLog, 0)
	for _, entry := range r.store.auditLogs {
		if merchantID == uuid.Nil || entry.MerchantID == merchantID {
			result = append(result, cloneAudit(entry))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	total := len(result)
	start, end := sliceBounds(pagination.Limit, pagination.Offset, total)
	return result[start:end], total, nil
}
