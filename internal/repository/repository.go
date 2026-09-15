package repository

import (
	"context"
	"errors"
)

var (
	ErrNotFound  = errors.New("repository not found")
	ErrForbidden = errors.New("repository access forbidden")
)

type Owner struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

type Parent struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Owner Owner  `json:"owner"`
}

type Repository struct {
	NameWithOwner string  `json:"nameWithOwner"`
	Parent        *Parent `json:"parent"`
}

type Provider interface {
	GetRepository(ctx context.Context, owner, name, authorization string) (Repository, error)
}

type Service struct {
	provider Provider
}

func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

func (s *Service) Get(ctx context.Context, owner, name, authorization string) (Repository, error) {
	return s.provider.GetRepository(ctx, owner, name, authorization)
}
