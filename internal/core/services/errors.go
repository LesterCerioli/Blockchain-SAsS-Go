package services

import (
	"errors"

	"github.com/blockchain-saas/payment-gateway/internal/core/domain/entities"
	apperrors "github.com/blockchain-saas/payment-gateway/pkg/errors"
)

func mapDomainError(err error, notFoundMessage string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, entities.ErrNotFound):
		return apperrors.NotFound(notFoundMessage)
	case errors.Is(err, entities.ErrConflict):
		return apperrors.Conflict(err.Error())
	case errors.Is(err, entities.ErrInvalidState):
		return apperrors.Unprocessable(err.Error())
	case errors.Is(err, entities.ErrExpired):
		return apperrors.Unprocessable(err.Error())
	case errors.Is(err, entities.ErrUnsupported):
		return apperrors.Unsupported(err.Error())
	case errors.Is(err, entities.ErrUnauthorized):
		return apperrors.Unauthorized(err.Error())
	case errors.Is(err, entities.ErrForbidden):
		return apperrors.Forbidden(err.Error())
	case errors.Is(err, entities.ErrInvalidAmount):
		return apperrors.Unprocessable(err.Error())
	case errors.Is(err, entities.ErrInvalidAddress):
		return apperrors.Unprocessable(err.Error())
	case errors.Is(err, entities.ErrBlocked):
		return apperrors.Forbidden(err.Error())
	case errors.Is(err, entities.ErrRateLimited):
		return apperrors.TooManyRequests(err.Error())
	default:
		return err
	}
}
