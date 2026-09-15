package gitea

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	userdomain "gh-gateway/internal/user"
)

func TestClientGetsAuthenticatedUser(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/user" {
			t.Errorf("request = %s %s, want GET /api/v1/user", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "token incoming" {
			t.Errorf("Authorization = %q, want token incoming", got)
		}
		_, _ = w.Write([]byte(`{"login":"barry","email":"private@example.test"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	got, err := client.GetAuthenticatedUser(context.Background(), "token incoming")
	if err != nil {
		t.Fatalf("GetAuthenticatedUser() error = %v", err)
	}
	if got.Login != "barry" {
		t.Fatalf("user = %#v, want login barry", got)
	}
}

func TestClientAuthenticatedUserConfiguredTokenTakesPrecedence(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token configured" {
			t.Errorf("Authorization = %q, want token configured", got)
		}
		_, _ = w.Write([]byte(`{"login":"barry"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "configured", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.GetAuthenticatedUser(context.Background(), "token incoming"); err != nil {
		t.Fatalf("GetAuthenticatedUser() error = %v", err)
	}
}

func TestClientAuthenticatedUserErrorsDoNotExposeResponseBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		wantTarget error
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"message":"gitea unauthorized secret"}`, wantTarget: userdomain.ErrUnauthorized},
		{name: "forbidden", status: http.StatusForbidden, body: `{"message":"gitea forbidden secret"}`, wantTarget: userdomain.ErrForbidden},
		{name: "server error", status: http.StatusInternalServerError, body: `{"message":"gitea server secret"}`},
		{name: "invalid JSON", status: http.StatusOK, body: `{"login":"gitea invalid secret"`},
		{name: "missing login", status: http.StatusOK, body: `{"email":"gitea missing login secret"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "", server.Client())
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			_, err = client.GetAuthenticatedUser(context.Background(), "")
			if err == nil {
				t.Fatal("GetAuthenticatedUser() error = nil")
			}
			if tt.wantTarget != nil && !errors.Is(err, tt.wantTarget) {
				t.Fatalf("error = %v, want %v", err, tt.wantTarget)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked Gitea response body: %v", err)
			}
		})
	}
}
