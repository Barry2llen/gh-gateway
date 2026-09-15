package githubrest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gh-gateway/internal/gateway"
	"gh-gateway/internal/gitea"
	"gh-gateway/internal/pullrequest"
)

func TestCommitPullRequestsVerticalSlice(t *testing.T) {
	t.Parallel()

	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/repos/foo/bar/pulls" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "token gateway-request" {
			t.Errorf("Authorization = %q", got)
		}
		pages++
		page := r.URL.Query().Get("page")
		wantQuery := url.Values{"state": {"all"}, "page": {page}, "limit": {"30"}}
		if r.URL.Query().Encode() != wantQuery.Encode() {
			t.Errorf("query = %s, want %s", r.URL.RawQuery, wantQuery.Encode())
		}
		switch page {
		case "1":
			w.Header().Set("X-Total-Count", "31")
			_, _ = w.Write([]byte(`[{"id":2,"number":2,"html_url":"https://git.example.test/foo/bar/pulls/2","state":"open","merged":false,"merge_commit_sha":null,"base":{"label":"main","repo_id":10},"head":{"label":"other","sha":"other","repo_id":10}}]`))
		case "2":
			w.Header().Set("X-Total-Count", "31")
			_, _ = w.Write([]byte(`[{"id":1,"number":1,"html_url":"https://git.example.test/foo/bar/pulls/1","state":"open","merged":false,"merge_commit_sha":null,"base":{"label":"main","repo_id":10},"head":{"label":"feature","sha":"wanted","repo_id":10}}]`))
		default:
			t.Errorf("unexpected page %q", page)
		}
	}))
	defer server.Close()

	provider, err := gitea.NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	router := gateway.NewRouter(http.NotFoundHandler(), NewHandler(pullrequest.NewService(provider)), http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/commits/wanted/pulls", nil)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "token gateway-request")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	want := `[{"number":1,"html_url":"https://git.example.test/foo/bar/pulls/1","state":"open"}]`
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != want {
		t.Fatalf("status/body = %d/%s, want 200/%s", response.Code, response.Body.String(), want)
	}
	if pages != 2 {
		t.Fatalf("pages = %d, want 2", pages)
	}
}

func TestCommitPullRequestsVerticalSliceHidesGiteaErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		wantStatus int
	}{
		{name: "not found", status: http.StatusNotFound, body: `{"message":"gitea not found detail"}`, wantStatus: http.StatusNotFound},
		{name: "forbidden", status: http.StatusForbidden, body: `{"message":"gitea forbidden detail"}`, wantStatus: http.StatusForbidden},
		{name: "server error", status: http.StatusInternalServerError, body: `{"message":"gitea server detail"}`, wantStatus: http.StatusBadGateway},
		{name: "invalid json", status: http.StatusOK, body: `{"secret":"gitea invalid detail"`, wantStatus: http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			provider, err := gitea.NewClient(server.URL, "", server.Client())
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			router := gateway.NewRouter(http.NotFoundHandler(), NewHandler(pullrequest.NewService(provider)), http.NotFoundHandler())
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/commits/wanted/pulls", nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if strings.Contains(response.Body.String(), "gitea") {
				t.Fatalf("body leaked Gitea detail: %s", response.Body.String())
			}
		})
	}
}
