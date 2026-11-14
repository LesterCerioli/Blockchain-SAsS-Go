package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/shared/logger"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
	"github.com/google/uuid"
)

type TokenOptions struct {
	Secret   string
	Issuer   string
	Audience []string
	TTL      time.Duration
}

type MerchantService struct {
	merchants  ports.MerchantRepository
	audit      ports.AuditRepository
	validation *ValidationService
	clock      ports.Clock
	logger     logger.Logger
	apiKeyEnv  string
	tokens     TokenOptions
}

func NewMerchantService(merchants ports.MerchantRepository, audit ports.AuditRepository, validation *ValidationService, clock ports.Clock, log logger.Logger, apiKeyEnv string, tokens TokenOptions) *MerchantService {
	if clock == nil {
		clock = ports.SystemClock{}
	}
	if log == nil {
		log = logger.NopLogger{}
	}
	if tokens.TTL <= 0 {
		tokens.TTL = 24 * time.Hour
	}
	return &MerchantService{
		merchants: merchants, audit: audit, validation: validation, clock: clock,
		logger: log, apiKeyEnv: apiKeyEnv, tokens: tokens,
	}
}

type RegisterMerchantInput struct {
	Name       string
	Email      string
	WebhookURL string
	Actor      string
	IPAddress  string
	UserAgent  string
}

func (s *MerchantService) Register(ctx context.Context, input RegisterMerchantInput) (*entities.Merchant, string, error) {
	if err := s.validation.ValidateMerchant(input.Name, input.Email); err != nil {
		return nil, "", apperrors.Validation(err.Error(), nil)
	}
	if err := s.validation.ValidateWebhookURL(input.WebhookURL); err != nil {
		return nil, "", apperrors.Validation(err.Error(), nil)
	}
	if _, err := s.merchants.GetByEmail(ctx, strings.ToLower(strings.TrimSpace(input.Email))); err == nil {
		return nil, "", apperrors.Conflict("merchant email already registered")
	} else if !errors.Is(err, entities.ErrNotFound) {
		return nil, "", apperrors.Internal("failed to check merchant email", err)
	}

	apiKey, apiKeyHash, prefix, err := utils.GenerateAPIKey(s.apiKeyEnv)
	if err != nil {
		return nil, "", apperrors.Internal("failed to generate api key", err)
	}
	webhookSecret, err := utils.RandomToken(32)
	if err != nil {
		return nil, "", apperrors.Internal("failed to generate webhook secret", err)
	}

	now := s.clock.Now()
	merchant := entities.NewMerchant(input.Name, input.Email, apiKeyHash, prefix, now)
	merchant.WebhookSecret = webhookSecret
	if err := s.merchants.Create(ctx, merchant); err != nil {
		if errors.Is(err, entities.ErrConflict) {
			return nil, "", apperrors.Conflict(err.Error())
		}
		return nil, "", apperrors.Internal("failed to create merchant", err)
	}
	s.auditLog(ctx, merchant.ID, input.Actor, "merchant.created", "merchant", merchant.ID.String(), map[string]interface{}{"email": merchant.Email}, input.IPAddress, input.UserAgent)
	return merchant, apiKey, nil
}

func (s *MerchantService) Get(ctx context.Context, id uuid.UUID) (*entities.Merchant, error) {
	merchant, err := s.merchants.GetByID(ctx, id)
	if err != nil {
		return nil, mapDomainError(err, "merchant not found")
	}
	return merchant, nil
}

func (s *MerchantService) List(ctx context.Context, pagination ports.Pagination) ([]*entities.Merchant, int, error) {
	merchants, total, err := s.merchants.List(ctx, pagination)
	if err != nil {
		return nil, 0, apperrors.Internal("failed to list merchants", err)
	}
	return merchants, total, nil
}

func (s *MerchantService) AuthenticateAPIKey(ctx context.Context, apiKey string) (*entities.Merchant, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, apperrors.Unauthorized("api key is required")
	}
	merchant, err := s.merchants.GetByAPIKeyHash(ctx, utils.HashAPIKey(apiKey))
	if err != nil {
		return nil, apperrors.Unauthorized("invalid api key")
	}
	if !merchant.CanAcceptPayments() {
		return nil, apperrors.Forbidden("merchant account is not active")
	}
	return merchant, nil
}

func (s *MerchantService) AuthenticateJWT(ctx context.Context, token string) (*entities.Merchant, error) {
	claims, err := utils.ParseJWT(s.tokens.Secret, token)
	if err != nil {
		return nil, apperrors.Unauthorized("invalid token: " + err.Error())
	}
	if s.tokens.Issuer != "" && claims.Issuer != s.tokens.Issuer {
		return nil, apperrors.Unauthorized("invalid token issuer")
	}
	if len(s.tokens.Audience) > 0 && !containsAudience(claims.Audience, s.tokens.Audience) {
		return nil, apperrors.Unauthorized("invalid token audience")
	}
	merchantID, err := uuid.Parse(claims.MerchantID)
	if err != nil {
		return nil, apperrors.Unauthorized("invalid token subject")
	}
	merchant, err := s.merchants.GetByID(ctx, merchantID)
	if err != nil {
		return nil, apperrors.Unauthorized("merchant not found")
	}
	if !merchant.CanAcceptPayments() {
		return nil, apperrors.Forbidden("merchant account is not active")
	}
	return merchant, nil
}

func (s *MerchantService) IssueToken(ctx context.Context, merchant *entities.Merchant) (string, time.Time, error) {
	token, expires, err := utils.CreateJWT(s.tokens.Secret, s.tokens.Issuer, s.tokens.Audience, merchant.ID, "merchant", s.tokens.TTL, s.clock.Now())
	if err != nil {
		return "", time.Time{}, apperrors.Internal("failed to issue token", err)
	}
	return token, expires, nil
}

func (s *MerchantService) LoginWithAPIKey(ctx context.Context, apiKey string) (*entities.Merchant, string, time.Time, error) {
	merchant, err := s.AuthenticateAPIKey(ctx, apiKey)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	token, expires, err := s.IssueToken(ctx, merchant)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return merchant, token, expires, nil
}

func (s *MerchantService) UpdateWebhook(ctx context.Context, merchantID uuid.UUID, webhookURL string, actor, ip, userAgent string) (*entities.Merchant, string, error) {
	if err := s.validation.ValidateWebhookURL(webhookURL); err != nil {
		return nil, "", apperrors.Validation(err.Error(), nil)
	}
	merchant, err := s.merchants.GetByID(ctx, merchantID)
	if err != nil {
		return nil, "", mapDomainError(err, "merchant not found")
	}
	secret := merchant.WebhookSecret
	if secret == "" {
		secret, err = utils.RandomToken(32)
		if err != nil {
			return nil, "", apperrors.Internal("failed to generate webhook secret", err)
		}
	}
	merchant.SetWebhook(webhookURL, secret, s.clock.Now())
	if err := s.merchants.Update(ctx, merchant); err != nil {
		return nil, "", apperrors.Internal("failed to update merchant", err)
	}
	s.auditLog(ctx, merchant.ID, actor, "merchant.webhook_updated", "merchant", merchant.ID.String(), map[string]interface{}{"webhook_url": webhookURL}, ip, userAgent)
	return merchant, secret, nil
}

func (s *MerchantService) RotateAPIKey(ctx context.Context, merchantID uuid.UUID, actor, ip, userAgent string) (*entities.Merchant, string, error) {
	merchant, err := s.merchants.GetByID(ctx, merchantID)
	if err != nil {
		return nil, "", mapDomainError(err, "merchant not found")
	}
	apiKey, hash, prefix, err := utils.GenerateAPIKey(s.apiKeyEnv)
	if err != nil {
		return nil, "", apperrors.Internal("failed to generate api key", err)
	}
	merchant.APIKeyHash = hash
	merchant.APIKeyPrefix = prefix
	merchant.UpdatedAt = s.clock.Now()
	if err := s.merchants.Update(ctx, merchant); err != nil {
		return nil, "", apperrors.Internal("failed to rotate api key", err)
	}
	s.auditLog(ctx, merchant.ID, actor, "merchant.api_key_rotated", "merchant", merchant.ID.String(), nil, ip, userAgent)
	return merchant, apiKey, nil
}

func (s *MerchantService) auditLog(ctx context.Context, merchantID uuid.UUID, actor, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string) {
	if s.audit == nil {
		return
	}
	entry := entities.NewAuditLog(s.clock.Now())
	entry.MerchantID = merchantID
	entry.Actor = actor
	entry.Action = action
	entry.ResourceType = resourceType
	entry.ResourceID = resourceID
	entry.Details = details
	entry.IPAddress = ip
	entry.UserAgent = userAgent
	if err := s.audit.Append(ctx, entry); err != nil {
		s.logger.Warn(ctx, "failed to append audit log", "error", err)
	}
}

func containsAudience(actual, expected []string) bool {
	for _, want := range expected {
		for _, have := range actual {
			if have == want {
				return true
			}
		}
	}
	return false
}

func merchantNotFound(err error) error {
	return fmt.Errorf("%w: %v", entities.ErrNotFound, err)
}
