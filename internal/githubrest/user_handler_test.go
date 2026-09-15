package githubrest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	userdomain "gh-gateway/internal/user"
)

type userServiceStub struct {
	result        userdomain.AuthenticatedUser
	err           error
	authorization string
}

func (s *userServiceStub) Get(_ context.Context, authorization string) (userdomain.AuthenticatedUser, error) {
	s.authorization = authorization
	return s.result, s.err
}

func TestUserHandlerReturnsAuthenticatedLogin(t *testing.T) {
	t.Parallel()

	service := &userServiceStub{result: userdomain.AuthenticatedUser{Login: "barry"}}
	request := httptest.NewRequest(http.MethodGet, "/api/v3/user", nil)
	request.Header.Set("Authorization", "token incoming")
	response := httptest.NewRecorder()
	NewUserHandler(service).ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status/content-type = %d/%q", response.Code, response.Header().Get("Content-Type"))
	}
	if got := strings.TrimSpace(response.Body.String()); got != `{"login":"barry"}` {
		t.Fatalf("body = %s", got)
	}
	if service.authorization != "token incoming" {
		t.Fatalf("Authorization = %q", service.authorization)
	}
}

func TestUserHandlerMapsErrorsWithoutLeakingDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "unauthorized", err: userdomain.ErrUnauthorized, status: http.StatusUnauthorized, body: `{"message":"Requires authentication."}`},
		{name: "forbidden", err: userdomain.ErrForbidden, status: http.StatusForbidden, body: `{"message":"Resource not accessible with the supplied credentials."}`},
		{name: "backend", err: errors.New("gitea secret detail"), status: http.StatusBadGateway, body: `{"message":"The upstream service could not be reached."}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			NewUserHandler(&userServiceStub{err: tt.err}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/user", nil))
			if response.Code != tt.status || strings.TrimSpace(response.Body.String()) != tt.body {
				t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "gitea secret") {
				t.Fatalf("body leaked backend detail: %s", response.Body.String())
			}
		})
	}
}
