package graphqlapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gh-gateway/internal/gitea"
	"gh-gateway/internal/repository"
)

func TestRepositoryInfoVerticalSlice(t *testing.T) {
	t.Parallel()

	giteaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/repos/foo/bar" {
			t.Errorf("Gitea request = %s %s, want GET /api/v1/repos/foo/bar", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "token gateway-request" {
			t.Errorf("Authorization = %q, want token gateway-request", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "full_name":"forker/bar",
          "parent":{"id":42,"name":"bar","owner":{"id":7,"login":"foo"}}
        }`))
	}))
	defer giteaServer.Close()

	provider, err := gitea.NewClient(giteaServer.URL, "", giteaServer.Client())
	if err != nil {
		t.Fatalf("gitea.NewClient() error = %v", err)
	}
	handler := NewRouter(repository.NewService(provider))
	response := performGraphQLRequest(t, handler, repositoryInfoQuery, "", "token gateway-request")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var body struct {
		Data struct {
			Repository struct {
				NameWithOwner string `json:"nameWithOwner"`
				Parent        struct {
					ID    string `json:"id"`
					Name  string `json:"name"`
					Owner struct {
						ID    string `json:"id"`
						Login string `json:"login"`
					} `json:"owner"`
				} `json:"parent"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	got := body.Data.Repository
	if got.NameWithOwner != "forker/bar" || got.Parent.ID != "42" || got.Parent.Name != "bar" || got.Parent.Owner.ID != "7" || got.Parent.Owner.Login != "foo" {
		t.Fatalf("repository response = %#v, want mapped fork and string IDs", got)
	}
}

func TestRepositoryInfoVerticalSliceHidesGiteaNotFoundBody(t *testing.T) {
	t.Parallel()

	giteaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"gitea secret repository detail","url":"internal.invalid"}`))
	}))
	defer giteaServer.Close()

	provider, err := gitea.NewClient(giteaServer.URL, "", giteaServer.Client())
	if err != nil {
		t.Fatalf("gitea.NewClient() error = %v", err)
	}
	response := performGraphQLRequest(t, NewRouter(repository.NewService(provider)), repositoryInfoQuery, "RepositoryInfo", "")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"repository":null`) || !strings.Contains(body, `"type":"NOT_FOUND"`) {
		t.Fatalf("body = %s, want repository null and NOT_FOUND", body)
	}
	if strings.Contains(body, "gitea secret") || strings.Contains(body, "internal.invalid") {
		t.Fatalf("body leaked Gitea error details: %s", body)
	}
}
