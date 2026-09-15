package githubrest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gh-gateway/internal/pullrequest"
)

type serviceStub struct {
	result []pullrequest.PullRequest
	err    error
	query  pullrequest.CommitQuery
}

func (s *serviceStub) FindForCommit(_ context.Context, query pullrequest.CommitQuery) ([]pullrequest.PullRequest, error) {
	s.query = query
	return s.result, s.err
}

func restRouter(service CommitPullRequestService) http.Handler {
	router := chi.NewRouter()
	router.Get("/api/v3/repos/{owner}/{repo}/commits/{sha}/pulls", NewHandler(service).ServeHTTP)
	return router
}

func TestHandlerReturnsGitHubRESTPullRequestArray(t *testing.T) {
	t.Parallel()

	service := &serviceStub{result: []pullrequest.PullRequest{
		{Number: 1, URL: "https://git.example.test/foo/bar/pulls/1", State: pullrequest.StateOpen},
		{Number: 2, URL: "https://git.example.test/foo/bar/pulls/2", State: pullrequest.StateMerged},
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/commits/abc123/pulls", nil)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "token incoming")
	response := httptest.NewRecorder()
	restRouter(service).ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status/content-type = %d/%q", response.Code, response.Header().Get("Content-Type"))
	}
	want := `[{"number":1,"html_url":"https://git.example.test/foo/bar/pulls/1","state":"open"},{"number":2,"html_url":"https://git.example.test/foo/bar/pulls/2","state":"closed"}]`
	if strings.TrimSpace(response.Body.String()) != want {
		t.Fatalf("body = %s, want %s", response.Body.String(), want)
	}
	if service.query.Owner != "foo" || service.query.Repo != "bar" || service.query.SHA != "abc123" || service.query.Authorization != "token incoming" {
		t.Fatalf("query = %#v", service.query)
	}
}

func TestHandlerReturnsEmptyArray(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	restRouter(&serviceStub{result: []pullrequest.PullRequest{}}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/commits/missing/pulls", nil))
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestHandlerMapsServiceErrorsWithoutLeakingDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "not found", err: pullrequest.ErrNotFound, status: http.StatusNotFound, body: `{"message":"Not Found"}`},
		{name: "forbidden", err: pullrequest.ErrForbidden, status: http.StatusForbidden, body: `{"message":"Resource not accessible with the supplied credentials."}`},
		{name: "backend", err: errors.New("gitea secret detail"), status: http.StatusBadGateway, body: `{"message":"The upstream service could not be reached."}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			restRouter(&serviceStub{err: tt.err}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/commits/abc/pulls", nil))
			if response.Code != tt.status || strings.TrimSpace(response.Body.String()) != tt.body {
				t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "gitea secret") {
				t.Fatalf("body leaked backend detail: %s", response.Body.String())
			}
		})
	}
}
