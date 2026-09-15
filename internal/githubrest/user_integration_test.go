package githubrest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gh-gateway/internal/gateway"
	"gh-gateway/internal/gitea"
	userdomain "gh-gateway/internal/user"
)

func TestAuthenticatedUserVerticalSlice(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/user" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "token gateway-request" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"login":"barry"}`))
	}))
	defer server.Close()

	provider, err := gitea.NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	router := gateway.NewRouter(http.NotFoundHandler(), http.NotFoundHandler(), NewUserHandler(userdomain.NewService(provider)))
	request := httptest.NewRequest(http.MethodGet, "/api/v3/user", nil)
	request.Header.Set("Authorization", "token gateway-request")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"login":"barry"}` {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestAuthenticatedUserVerticalSliceHidesGiteaErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		wantStatus int
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"message":"gitea unauthorized detail"}`, wantStatus: http.StatusUnauthorized},
		{name: "forbidden", status: http.StatusForbidden, body: `{"message":"gitea forbidden detail"}`, wantStatus: http.StatusForbidden},
		{name: "server error", status: http.StatusInternalServerError, body: `{"message":"gitea server detail"}`, wantStatus: http.StatusBadGateway},
		{name: "invalid JSON", status: http.StatusOK, body: `{"login":"gitea invalid detail"`, wantStatus: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			provider, err := gitea.NewClient(server.URL, "", server.Client())
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			router := gateway.NewRouter(http.NotFoundHandler(), http.NotFoundHandler(), NewUserHandler(userdomain.NewService(provider)))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/user", nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if strings.Contains(response.Body.String(), "gitea") {
				t.Fatalf("body leaked Gitea detail: %s", response.Body.String())
			}
		})
	}
}

func TestAuthenticatedUserVerticalSliceMapsTransportErrorToBadGateway(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	baseURL := server.URL
	server.Close()

	provider, err := gitea.NewClient(baseURL, "", client)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	router := gateway.NewRouter(http.NotFoundHandler(), http.NotFoundHandler(), NewUserHandler(userdomain.NewService(provider)))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/user", nil))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", response.Code)
	}
}
