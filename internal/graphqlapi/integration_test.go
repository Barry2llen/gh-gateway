package graphqlapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gh-gateway/internal/gitea"
	"gh-gateway/internal/pullrequest"
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
	handler := NewRouter(repository.NewService(provider), nil)
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
	response := performGraphQLRequest(t, NewRouter(repository.NewService(provider), nil), repositoryInfoQuery, "RepositoryInfo", "")

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

func TestPullRequestForBranchVerticalSlice(t *testing.T) {
	t.Parallel()

	requests := 0
	giteaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("Authorization"); got != "token gateway-request" {
			t.Errorf("Authorization = %q, want token gateway-request", got)
		}
		switch r.URL.Path {
		case "/api/v1/repos/foo/bar":
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case "/api/v1/repos/foo/bar/pulls":
			_, _ = w.Write([]byte(`[
              {
                "id":11,"number":1,"html_url":"https://git.example.test/foo/bar/pulls/1",
                "state":"open","merged":false,
                "base":{"label":"main","repo_id":10,"repo":{"owner":{"id":7,"login":"foo"}}},
                "head":{"label":"feature","repo_id":10,"repo":{"owner":{"id":7,"login":"foo","full_name":"Foo"}}}
              }
            ]`))
		default:
			t.Errorf("unexpected Gitea path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer giteaServer.Close()

	provider, err := gitea.NewClient(giteaServer.URL, "", giteaServer.Client())
	if err != nil {
		t.Fatalf("gitea.NewClient() error = %v", err)
	}
	handler := NewRouter(repository.NewService(provider), pullrequest.NewService(provider))
	response := performGraphQLRequest(t, handler, pullRequestForBranchQuery, "PullRequestForBranch", "token gateway-request")
	if response.Code != http.StatusOK || requests != 2 {
		t.Fatalf("status/requests = %d/%d, want 200/2", response.Code, requests)
	}
	body := response.Body.String()
	for _, want := range []string{`"number":1`, `"url":"https://git.example.test/foo/bar/pulls/1"`, `"state":"OPEN"`, `"id":"11"`, `"name":"main"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s, missing %s", body, want)
		}
	}
}

func TestPullRequestForBranchVerticalSliceHidesGiteaErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status   int
		wantType string
	}{
		{status: http.StatusNotFound, wantType: "NOT_FOUND"},
		{status: http.StatusForbidden, wantType: "FORBIDDEN"},
		{status: http.StatusInternalServerError, wantType: "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.wantType, func(t *testing.T) {
			t.Parallel()
			giteaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"gitea private failure"}`))
			}))
			defer giteaServer.Close()
			provider, err := gitea.NewClient(giteaServer.URL, "", giteaServer.Client())
			if err != nil {
				t.Fatalf("gitea.NewClient() error = %v", err)
			}
			response := performGraphQLRequest(t, NewRouter(repository.NewService(provider), pullrequest.NewService(provider)), pullRequestForBranchQuery, "", "")
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"type":"`+tt.wantType+`"`) {
				t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "gitea private") {
				t.Fatalf("body leaked Gitea error: %s", response.Body.String())
			}
		})
	}
}
