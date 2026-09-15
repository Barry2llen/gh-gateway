package user

import (
	"context"
	"errors"
	"testing"
)

type providerStub struct {
	result        AuthenticatedUser
	err           error
	authorization string
}

func (p *providerStub) GetAuthenticatedUser(_ context.Context, authorization string) (AuthenticatedUser, error) {
	p.authorization = authorization
	return p.result, p.err
}

func TestServiceGetsAuthenticatedUser(t *testing.T) {
	t.Parallel()

	provider := &providerStub{result: AuthenticatedUser{Login: "barry"}}
	got, err := NewService(provider).Get(context.Background(), "token incoming")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Login != "barry" || provider.authorization != "token incoming" {
		t.Fatalf("user/auth = %#v/%q", got, provider.authorization)
	}
}

func TestServicePreservesProviderErrorClassification(t *testing.T) {
	t.Parallel()

	provider := &providerStub{err: ErrUnauthorized}
	_, err := NewService(provider).Get(context.Background(), "")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Get() error = %v, want ErrUnauthorized", err)
	}
}
