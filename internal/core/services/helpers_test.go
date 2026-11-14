package services_test

import (
	"testing"

	"github.com/blockchain-saas/payment-gateway/internal/core/adapters/secondary/repositories"
	"github.com/blockchain-saas/payment-gateway/internal/core/domain/ports"
	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
)

func requireAppErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an application error with code %q, got nil", code)
	}
	appErr, ok := apperrors.As(err)
	if !ok {
		t.Fatalf("expected an application error, got %T: %v", err, err)
	}
	if appErr.Code != code {
		t.Fatalf("error code = %q, want %q (message %q)", appErr.Code, code, appErr.Message)
	}
}

func newMerchantRepository() ports.Repositories {
	return repositories.NewMemoryRepository().Repositories()
}
