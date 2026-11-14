package services

import (
	"context"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/types"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
	"github.com/google/uuid"
)

type EscrowServiceOptions struct {
	DefaultReleaseDelay time.Duration
	MaxDuration         time.Duration
	AllowForceRelease   bool
}

type EscrowService struct {
	escrows  ports.EscrowRepository
	payments ports.PaymentIntentRepository
	audit    ports.AuditRepository
	registry ports.BlockchainRegistry
	events   EventPublisher
	clock    ports.Clock
	logger   logger.Logger
	options  EscrowServiceOptions
}

func NewEscrowService(escrows ports.EscrowRepository, payments ports.PaymentIntentRepository, audit ports.AuditRepository, registry ports.BlockchainRegistry, events EventPublisher, clock ports.Clock, log logger.Logger, options EscrowServiceOptions) *EscrowService {
	if clock == nil {
		clock = ports.SystemClock{}
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	if events == nil {
		events = NopEventPublisher{}
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 720 * time.Hour
	}
	return &EscrowService{
		escrows: escrows, payments: payments, audit: audit, registry: registry, events: events,
		clock: clock, logger: log, options: options,
	}
}

type CreateEscrowInput struct {
	MerchantID      uuid.UUID
	PaymentIntentID uuid.UUID
	Beneficiary     string
	Arbiter         string
	ReleaseAt       *time.Time
	Actor           string
	IPAddress       string
	UserAgent       string
}

func (s *EscrowService) Create(ctx context.Context, input CreateEscrowInput) (*entities.Escrow, error) {
	payment, err := s.payments.GetByID(ctx, input.PaymentIntentID)
	if err != nil {
		return nil, mapDomainError(err, "payment intent not found")
	}
	if payment.MerchantID != input.MerchantID {
		return nil, apperrors.NotFound("payment intent not found")
	}
	if payment.Status != entities.PaymentStatusSucceeded {
		return nil, apperrors.Unprocessable("escrow can only be created for succeeded payments")
	}
	adapter, err := s.registry.Get(payment.Blockchain, payment.Network)
	if err != nil {
		return nil, mapDomainError(err, "blockchain adapter not available")
	}
	valid, err := adapter.ValidateAddress(ctx, input.Beneficiary)
	if err != nil || !valid {
		return nil, apperrors.Validation("invalid beneficiary address", nil)
	}

	now := s.clock.Now()
	releaseAt := now.Add(s.options.DefaultReleaseDelay)
	if input.ReleaseAt != nil {
		releaseAt = input.ReleaseAt.UTC()
	}
	if releaseAt.Before(now) {
		return nil, apperrors.Validation("release_at cannot be in the past", nil)
	}
	if releaseAt.Sub(now) > s.options.MaxDuration {
		return nil, apperrors.Validation("release_at exceeds maximum escrow duration", nil)
	}

	escrow := entities.NewEscrow(input.MerchantID, payment.ID, now)
	escrow.Amount = payment.Amount
	escrow.CryptoAmount = payment.PaidAmount
	if escrow.CryptoAmount == nil {
		escrow.CryptoAmount = payment.CryptoAmount
	}
	escrow.Currency = payment.Currency
	escrow.CryptoCurrency = payment.CryptoCurrency
	escrow.Blockchain = payment.Blockchain
	escrow.Network = payment.Network
	escrow.Beneficiary = input.Beneficiary
	escrow.Arbiter = input.Arbiter
	escrow.ReleaseAt = releaseAt
	if err := escrow.Fund(now); err != nil {
		return nil, apperrors.Internal("failed to fund escrow", err)
	}
	if err := s.escrows.Create(ctx, escrow); err != nil {
		return nil, mapDomainError(err, "failed to create escrow")
	}
	s.auditLog(ctx, escrow, input.Actor, "escrow.created", input.IPAddress, input.UserAgent, map[string]interface{}{
		"payment_intent_id": payment.ID.String(), "release_at": releaseAt.Format(time.RFC3339),
	})
	s.publish(ctx, escrow, entities.EventEscrowCreated)
	return escrow, nil
}

func (s *EscrowService) Get(ctx context.Context, merchantID, escrowID uuid.UUID) (*entities.Escrow, error) {
	escrow, err := s.escrows.GetByID(ctx, escrowID)
	if err != nil {
		return nil, mapDomainError(err, "escrow not found")
	}
	if merchantID != uuid.Nil && escrow.MerchantID != merchantID {
		return nil, apperrors.NotFound("escrow not found")
	}
	return escrow, nil
}

func (s *EscrowService) List(ctx context.Context, merchantID uuid.UUID, pagination ports.Pagination) ([]*entities.Escrow, int, error) {
	return s.escrows.ListByMerchant(ctx, merchantID, pagination)
}

type EscrowExecutionInput struct {
	TxHash            string
	SignedTransaction string
	Actor             string
	IPAddress         string
	UserAgent         string
}

func (s *EscrowService) Release(ctx context.Context, merchantID, escrowID uuid.UUID, force bool, input EscrowExecutionInput) (*entities.Escrow, error) {
	escrow, err := s.Get(ctx, merchantID, escrowID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	if force && !s.options.AllowForceRelease {
		return nil, apperrors.Forbidden("force release is disabled")
	}
	if !escrow.CanRelease(now, force) {
		return nil, apperrors.Unprocessable("escrow is not ready for release")
	}
	txHash, err := s.execute(ctx, escrow, input)
	if err != nil {
		return nil, err
	}
	if err := escrow.Release(now, txHash); err != nil {
		return nil, mapDomainError(err, "invalid escrow state")
	}
	if err := s.escrows.Update(ctx, escrow); err != nil {
		return nil, apperrors.Internal("failed to release escrow", err)
	}
	s.auditLog(ctx, escrow, input.Actor, "escrow.released", input.IPAddress, input.UserAgent, map[string]interface{}{"tx_hash": txHash})
	s.publish(ctx, escrow, entities.EventEscrowReleased)
	return escrow, nil
}

func (s *EscrowService) Refund(ctx context.Context, merchantID, escrowID uuid.UUID, input EscrowExecutionInput) (*entities.Escrow, error) {
	escrow, err := s.Get(ctx, merchantID, escrowID)
	if err != nil {
		return nil, err
	}
	txHash, err := s.execute(ctx, escrow, input)
	if err != nil {
		return nil, err
	}
	if err := escrow.Refund(s.clock.Now(), txHash); err != nil {
		return nil, mapDomainError(err, "invalid escrow state")
	}
	if err := s.escrows.Update(ctx, escrow); err != nil {
		return nil, apperrors.Internal("failed to refund escrow", err)
	}
	s.auditLog(ctx, escrow, input.Actor, "escrow.refunded", input.IPAddress, input.UserAgent, map[string]interface{}{"tx_hash": txHash})
	s.publish(ctx, escrow, entities.EventEscrowRefunded)
	return escrow, nil
}

func (s *EscrowService) Cancel(ctx context.Context, merchantID, escrowID uuid.UUID, actor, ip, userAgent string) (*entities.Escrow, error) {
	escrow, err := s.Get(ctx, merchantID, escrowID)
	if err != nil {
		return nil, err
	}
	if err := escrow.Cancel(s.clock.Now()); err != nil {
		return nil, mapDomainError(err, "invalid escrow state")
	}
	if err := s.escrows.Update(ctx, escrow); err != nil {
		return nil, apperrors.Internal("failed to cancel escrow", err)
	}
	s.auditLog(ctx, escrow, actor, "escrow.canceled", ip, userAgent, nil)
	s.publish(ctx, escrow, entities.EventEscrowCanceled)
	return escrow, nil
}

func (s *EscrowService) ReleaseDue(ctx context.Context) (int, error) {
	due, err := s.escrows.ListReleasable(ctx, s.clock.Now(), 100)
	if err != nil {
		return 0, err
	}
	released := 0
	for _, escrow := range due {
		if err := escrow.Release(s.clock.Now(), ""); err != nil {
			continue
		}
		if err := s.escrows.Update(ctx, escrow); err != nil {
			s.logger.Warn(ctx, "failed to update due escrow", "escrow_id", escrow.ID, "error", err)
			continue
		}
		s.auditLog(ctx, escrow, "escrow-worker", "escrow.released", "", "system", nil)
		s.publish(ctx, escrow, entities.EventEscrowReleased)
		released++
	}
	return released, nil
}

func (s *EscrowService) execute(ctx context.Context, escrow *entities.Escrow, input EscrowExecutionInput) (string, error) {
	if input.TxHash != "" {
		return input.TxHash, nil
	}
	adapter, err := s.registry.Get(escrow.Blockchain, escrow.Network)
	if err != nil {
		return "", mapDomainError(err, "blockchain adapter not available")
	}
	txHash, err := adapter.SendTransaction(ctx, ports.SendTransactionRequest{
		RawTransaction: input.SignedTransaction,
		To:             escrow.Beneficiary,
		Amount:         escrow.CryptoAmount,
		Priority:       types.PriorityMedium,
		Reference:      "escrow:" + escrow.ID.String(),
	})
	if err != nil {
		return "", apperrors.ServiceUnavailable("failed to broadcast escrow transaction", err)
	}
	return txHash, nil
}

func (s *EscrowService) auditLog(ctx context.Context, escrow *entities.Escrow, actor, action, ip, userAgent string, details map[string]interface{}) {
	if s.audit == nil {
		return
	}
	if details == nil {
		details = map[string]interface{}{}
	}
	details["status"] = string(escrow.Status)
	entry := entities.NewAuditLog(s.clock.Now())
	entry.MerchantID = escrow.MerchantID
	entry.Actor = actor
	entry.Action = action
	entry.ResourceType = "escrow"
	entry.ResourceID = escrow.ID.String()
	entry.Details = details
	entry.IPAddress = ip
	entry.UserAgent = userAgent
	if err := s.audit.Append(ctx, entry); err != nil {
		s.logger.Warn(ctx, "failed to append audit log", "error", err)
	}
}

func (s *EscrowService) publish(ctx context.Context, escrow *entities.Escrow, eventType entities.EventType) {
	if s.events == nil {
		return
	}
	event := entities.NewEvent(eventType, "escrow", escrow.ID, escrow.MerchantID, s.clock.Now(), map[string]interface{}{
		"escrow_id": escrow.ID.String(), "payment_intent_id": escrow.PaymentIntentID.String(),
		"status": string(escrow.Status), "beneficiary": escrow.Beneficiary,
		"release_at": escrow.ReleaseAt.Format(time.RFC3339),
	})
	if err := s.events.PublishEvent(ctx, event); err != nil {
		s.logger.Warn(ctx, "failed to publish escrow event", "error", err)
	}
}
