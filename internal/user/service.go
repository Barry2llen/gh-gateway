package user

import (
	"context"
	"errors"
)

var (
	ErrUnauthorized = errors.New("authentication required")
	ErrForbidden    = errors.New("user access forbidden")
)

type AuthenticatedUser struct {
	Login string
}

type Provider interface {
	GetAuthenticatedUser(ctx context.Context, authorization string) (AuthenticatedUser, error)
}

type Service struct {
	provider Provider
}

func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

func (s *Service) Get(ctx context.Context, authorization string) (AuthenticatedUser, error) {
	return s.provider.GetAuthenticatedUser(ctx, authorization)
}
