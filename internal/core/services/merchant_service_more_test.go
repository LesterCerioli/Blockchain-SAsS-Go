package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
	"github.com/google/uuid"
)

type failingMerchantRepository struct {
	ports.MerchantRepository
	getByEmailErr error
	createErr     error
	updateErr     error
	listErr       error
}

func (r *failingMerchantRepository) GetByEmail(ctx context.Context, email string) (*entities.Merchant, error) {
	if r.getByEmailErr != nil {
		return nil, r.getByEmailErr
	}
	return r.MerchantRepository.GetByEmail(ctx, email)
}

func (r *failingMerchantRepository) Create(ctx context.Context, merchant *entities.Merchant) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.MerchantRepository.Create(ctx, merchant)
}

func (r *failingMerchantRepository) Update(ctx context.Context, merchant *entities.Merchant) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.MerchantRepository.Update(ctx, merchant)
}

func (r *failingMerchantRepository) List(ctx context.Context, pagination ports.Pagination) ([]*entities.Merchant, int, error) {
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return r.MerchantRepository.List(ctx, pagination)
}

func newMerchantServiceWithRepo(repos ports.Repositories, merchants ports.MerchantRepository) *services.MerchantService {
	return services.NewMerchantService(
		merchants, repos.Audit, services.NewValidationService(), fixedClock{now: time.Now().UTC()}, nil, "test",
		services.TokenOptions{
			Secret: merchantTokenSecret, Issuer: "issuer", Audience: []string{"merchant-api"}, TTL: time.Hour,
		},
	)
}

func TestMerchantServiceRepositoryErrors(t *testing.T) {
	t.Run("email lookup error", func(t *testing.T) {
		repos := newMerchantRepository()
		service := newMerchantServiceWithRepo(repos, &failingMerchantRepository{
			MerchantRepository: repos.Merchants, getByEmailErr: errors.New("lookup failed"),
		})

		_, _, err := service.Register(context.Background(), services.RegisterMerchantInput{
			Name: "Acme", Email: "acme@example.com",
		})
		requireAppErrorCode(t, err, "internal_error")
	})

	t.Run("create error", func(t *testing.T) {
		repos := newMerchantRepository()
		service := newMerchantServiceWithRepo(repos, &failingMerchantRepository{
			MerchantRepository: repos.Merchants, createErr: errors.New("insert failed"),
		})

		_, _, err := service.Register(context.Background(), services.RegisterMerchantInput{
			Name: "Acme", Email: "acme@example.com",
		})
		requireAppErrorCode(t, err, "internal_error")
	})

	t.Run("create conflict", func(t *testing.T) {
		repos := newMerchantRepository()
		service := newMerchantServiceWithRepo(repos, &failingMerchantRepository{
			MerchantRepository: repos.Merchants,
			getByEmailErr:      entities.ErrNotFound,
			createErr:          entities.ErrConflict,
		})

		_, _, err := service.Register(context.Background(), services.RegisterMerchantInput{
			Name: "Acme", Email: "acme@example.com",
		})
		requireAppErrorCode(t, err, "conflict")
	})

	t.Run("list error", func(t *testing.T) {
		f := newMerchantFixture(t)
		f.register(t)
		service := newMerchantServiceWithRepo(f.repos, &failingMerchantRepository{
			MerchantRepository: f.repos.Merchants, listErr: errors.New("list failed"),
		})

		_, _, err := service.List(context.Background(), ports.Pagination{Limit: 10})
		requireAppErrorCode(t, err, "internal_error")
	})

	t.Run("update webhook error", func(t *testing.T) {
		f := newMerchantFixture(t)
		_, merchantID := f.register(t)
		service := newMerchantServiceWithRepo(f.repos, &failingMerchantRepository{
			MerchantRepository: f.repos.Merchants, updateErr: errors.New("update failed"),
		})

		_, _, err := service.UpdateWebhook(context.Background(), merchantID, "https://example.com/hook", "actor", "", "")
		requireAppErrorCode(t, err, "internal_error")
	})

	t.Run("rotate key error", func(t *testing.T) {
		f := newMerchantFixture(t)
		_, merchantID := f.register(t)
		service := newMerchantServiceWithRepo(f.repos, &failingMerchantRepository{
			MerchantRepository: f.repos.Merchants, updateErr: errors.New("update failed"),
		})

		_, _, err := service.RotateAPIKey(context.Background(), merchantID, "actor", "", "")
		requireAppErrorCode(t, err, "internal_error")
	})
}

func TestMerchantServiceAuthenticateJWTUnknownAndInactive(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()
	apiKey, merchantID := f.register(t)
	now := f.clock.Now()

	unknown, _, err := utils.CreateJWT(merchantTokenSecret, "issuer", []string{"merchant-api"}, uuid.New(), "merchant", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.AuthenticateJWT(ctx, unknown)
	requireAppErrorCode(t, err, "unauthorized")

	_, token, _, err := f.service.LoginWithAPIKey(ctx, apiKey)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.repos.Merchants.GetByID(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Suspend(f.clock.Now())
	if err := f.repos.Merchants.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.AuthenticateJWT(ctx, token)
	requireAppErrorCode(t, err, "forbidden")
}

func TestMerchantServiceUpdateWebhookGeneratesSecret(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()
	_, merchantID := f.register(t)

	stored, err := f.repos.Merchants.GetByID(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	stored.WebhookSecret = ""
	if err := f.repos.Merchants.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}

	merchant, secret, err := f.service.UpdateWebhook(ctx, merchantID, "https://example.com/hook", "actor", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret == "" || merchant.WebhookSecret != secret {
		t.Fatalf("secret = %q merchant secret = %q", secret, merchant.WebhookSecret)
	}
}
