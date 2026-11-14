package services_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	"github.com/blockchain-saas/payment-gateway/internal/core/services"
	"github.com/blockchain-saas/payment-gateway/internal/shared/utils"
	"github.com/google/uuid"
)

const merchantTokenSecret = "test-secret-that-is-long-enough"

type merchantFixture struct {
	service *services.MerchantService
	repos   ports.Repositories
	clock   fixedClock
}

func newMerchantFixture(t *testing.T) *merchantFixture {
	t.Helper()
	clock := fixedClock{now: time.Now().UTC()}
	repos := newMerchantRepository()
	service := services.NewMerchantService(
		repos.Merchants, repos.Audit, services.NewValidationService(), clock, nil, "test",
		services.TokenOptions{
			Secret: merchantTokenSecret, Issuer: "issuer", Audience: []string{"merchant-api"}, TTL: time.Hour,
		},
	)
	return &merchantFixture{service: service, repos: repos, clock: clock}
}

func (f *merchantFixture) register(t *testing.T) (string, uuid.UUID) {
	t.Helper()
	merchant, apiKey, err := f.service.Register(context.Background(), services.RegisterMerchantInput{
		Name: "Test Merchant", Email: "merchant@example.com", WebhookURL: "https://example.com/hooks",
	})
	if err != nil {
		t.Fatalf("register merchant: %v", err)
	}
	return apiKey, merchant.ID
}

func TestMerchantServiceRegister(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()

	merchant, apiKey, err := f.service.Register(ctx, services.RegisterMerchantInput{
		Name: "  Acme  ", Email: "  ACME@Example.com ", WebhookURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merchant.Name != "Acme" || merchant.Email != "acme@example.com" {
		t.Fatalf("merchant = %+v", merchant)
	}
	if !strings.HasPrefix(apiKey, "pk_test_") {
		t.Fatalf("api key = %q", apiKey)
	}
	if merchant.WebhookSecret == "" {
		t.Fatal("expected webhook secret")
	}
	if !merchant.CanAcceptPayments() {
		t.Fatalf("merchant status = %s", merchant.Status)
	}

	logs, total, err := f.repos.Audit.ListByMerchant(ctx, merchant.ID, ports.Pagination{Limit: 10})
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if total != 1 || logs[0].Action != "merchant.created" {
		t.Fatalf("audit logs = %+v", logs)
	}
}

func TestMerchantServiceRegisterValidation(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()

	_, _, err := f.service.Register(ctx, services.RegisterMerchantInput{Name: "", Email: "merchant@example.com"})
	requireAppErrorCode(t, err, "validation_error")

	_, _, err = f.service.Register(ctx, services.RegisterMerchantInput{Name: "Acme", Email: "bad"})
	requireAppErrorCode(t, err, "validation_error")

	_, _, err = f.service.Register(ctx, services.RegisterMerchantInput{Name: "Acme", Email: "a@b.co", WebhookURL: "ftp://example.com"})
	requireAppErrorCode(t, err, "validation_error")
}

func TestMerchantServiceRegisterDuplicate(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()
	f.register(t)

	_, _, err := f.service.Register(ctx, services.RegisterMerchantInput{Name: "Other", Email: "MERCHANT@example.com"})
	requireAppErrorCode(t, err, "conflict")
}

func TestMerchantServiceAuthenticateAPIKey(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()
	apiKey, merchantID := f.register(t)

	merchant, err := f.service.AuthenticateAPIKey(ctx, " "+apiKey+" ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merchant.ID != merchantID {
		t.Fatalf("merchant id = %s, want %s", merchant.ID, merchantID)
	}

	_, err = f.service.AuthenticateAPIKey(ctx, "")
	requireAppErrorCode(t, err, "unauthorized")

	_, err = f.service.AuthenticateAPIKey(ctx, "pk_test_wrong")
	requireAppErrorCode(t, err, "unauthorized")

	stored, err := f.repos.Merchants.GetByID(ctx, merchantID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Suspend(f.clock.Now())
	if err := f.repos.Merchants.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.AuthenticateAPIKey(ctx, apiKey)
	requireAppErrorCode(t, err, "forbidden")
}

func TestMerchantServiceAuthenticateJWT(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()
	apiKey, merchantID := f.register(t)

	_, token, expires, err := f.service.LoginWithAPIKey(ctx, apiKey)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token == "" || expires.Before(f.clock.Now()) {
		t.Fatalf("token = %q expires = %s", token, expires)
	}

	merchant, err := f.service.AuthenticateJWT(ctx, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merchant.ID != merchantID {
		t.Fatalf("merchant id = %s, want %s", merchant.ID, merchantID)
	}

	_, err = f.service.AuthenticateJWT(ctx, "not-a-token")
	requireAppErrorCode(t, err, "unauthorized")
}

func TestMerchantServiceAuthenticateJWTClaims(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()
	_, merchantID := f.register(t)
	now := f.clock.Now()

	wrongIssuer, _, err := utils.CreateJWT(merchantTokenSecret, "other-issuer", []string{"merchant-api"}, merchantID, "merchant", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.AuthenticateJWT(ctx, wrongIssuer)
	requireAppErrorCode(t, err, "unauthorized")

	wrongAudience, _, err := utils.CreateJWT(merchantTokenSecret, "issuer", []string{"other"}, merchantID, "merchant", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.AuthenticateJWT(ctx, wrongAudience)
	requireAppErrorCode(t, err, "unauthorized")

	expired, _, err := utils.CreateJWT(merchantTokenSecret, "issuer", []string{"merchant-api"}, merchantID, "merchant", time.Hour, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.AuthenticateJWT(ctx, expired)
	requireAppErrorCode(t, err, "unauthorized")
}

func TestMerchantServiceLoginInvalidKey(t *testing.T) {
	f := newMerchantFixture(t)

	_, _, _, err := f.service.LoginWithAPIKey(context.Background(), "pk_test_missing")
	requireAppErrorCode(t, err, "unauthorized")
}

func TestMerchantServiceUpdateWebhook(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()
	_, merchantID := f.register(t)

	merchant, secret, err := f.service.UpdateWebhook(ctx, merchantID, "https://hooks.example.com/new", "actor", "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merchant.WebhookURL != "https://hooks.example.com/new" || secret == "" {
		t.Fatalf("merchant = %+v secret = %q", merchant, secret)
	}

	_, _, err = f.service.UpdateWebhook(ctx, merchantID, "ftp://bad", "actor", "", "")
	requireAppErrorCode(t, err, "validation_error")

	_, _, err = f.service.UpdateWebhook(ctx, uuid.New(), "https://hooks.example.com/new", "actor", "", "")
	requireAppErrorCode(t, err, "not_found")
}

func TestMerchantServiceRotateAPIKey(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()
	oldKey, merchantID := f.register(t)

	_, newKey, err := f.service.RotateAPIKey(ctx, merchantID, "actor", "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newKey == "" || newKey == oldKey {
		t.Fatalf("new key = %q", newKey)
	}

	if _, err := f.service.AuthenticateAPIKey(ctx, oldKey); err == nil {
		t.Fatal("old api key must stop working")
	}
	if _, err := f.service.AuthenticateAPIKey(ctx, newKey); err != nil {
		t.Fatalf("new api key failed: %v", err)
	}

	_, _, err = f.service.RotateAPIKey(ctx, uuid.New(), "actor", "", "")
	requireAppErrorCode(t, err, "not_found")
}

func TestMerchantServiceGetAndList(t *testing.T) {
	f := newMerchantFixture(t)
	ctx := context.Background()
	_, merchantID := f.register(t)

	merchant, err := f.service.Get(ctx, merchantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merchant.ID != merchantID {
		t.Fatalf("merchant id = %s", merchant.ID)
	}

	_, err = f.service.Get(ctx, uuid.New())
	requireAppErrorCode(t, err, "not_found")

	merchants, total, err := f.service.List(ctx, ports.Pagination{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(merchants) != 1 {
		t.Fatalf("total = %d merchants = %d", total, len(merchants))
	}
}
