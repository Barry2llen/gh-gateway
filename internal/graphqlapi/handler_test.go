package graphqlapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gh-gateway/internal/pullrequest"
	"gh-gateway/internal/repository"
)

type serviceStub struct {
	get func(context.Context, string, string, string) (repository.Repository, error)
}

type pullRequestServiceStub struct {
	find func(context.Context, pullrequest.Query) (pullrequest.Result, error)
}

func (s pullRequestServiceStub) FindForBranch(ctx context.Context, query pullrequest.Query) (pullrequest.Result, error) {
	return s.find(ctx, query)
}

func (s serviceStub) Get(ctx context.Context, owner, name, authorization string) (repository.Repository, error) {
	return s.get(ctx, owner, name, authorization)
}

func TestHandlerReturnsRepositoryEnvelope(t *testing.T) {
	t.Parallel()

	service := serviceStub{get: func(_ context.Context, owner, name, authorization string) (repository.Repository, error) {
		if owner != "foo" || name != "bar" {
			t.Fatalf("service arguments = %q/%q, want foo/bar", owner, name)
		}
		if authorization != "token secret" {
			t.Fatalf("authorization = %q, want token secret", authorization)
		}
		return repository.Repository{NameWithOwner: "foo/bar"}, nil
	}}

	response := performGraphQLRequest(t, NewRouter(service, nil), repositoryInfoQuery, "RepositoryInfo", "token secret")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := strings.TrimSpace(response.Body.String()); got != `{"data":{"repository":{"nameWithOwner":"foo/bar","parent":null}}}` {
		t.Fatalf("body = %s", got)
	}
}

func TestHandlerMapsNotFoundToGraphQLError(t *testing.T) {
	t.Parallel()

	service := serviceStub{get: func(context.Context, string, string, string) (repository.Repository, error) {
		return repository.Repository{}, repository.ErrNotFound
	}}
	response := performGraphQLRequest(t, NewRouter(service, nil), repositoryInfoQuery, "", "")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var body struct {
		Data struct {
			Repository any `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Type    string   `json:"type"`
			Path    []string `json:"path"`
			Message string   `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if body.Data.Repository != nil {
		t.Fatalf("repository = %#v, want nil", body.Data.Repository)
	}
	if len(body.Errors) != 1 || body.Errors[0].Type != "NOT_FOUND" {
		t.Fatalf("errors = %#v, want one NOT_FOUND", body.Errors)
	}
	if len(body.Errors[0].Path) != 1 || body.Errors[0].Path[0] != "repository" {
		t.Fatalf("error path = %#v, want [repository]", body.Errors[0].Path)
	}
	if strings.Contains(response.Body.String(), "gitea internal detail") {
		t.Fatalf("body leaked upstream error: %s", response.Body.String())
	}
}

func TestHandlerRejectsUnsupportedOperationWithoutCallingService(t *testing.T) {
	t.Parallel()

	called := false
	service := serviceStub{get: func(context.Context, string, string, string) (repository.Repository, error) {
		called = true
		return repository.Repository{}, errors.New("unexpected")
	}}
	response := performGraphQLRequest(t, NewRouter(service, nil), `query ViewerInfo { viewer { login } }`, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if called {
		t.Fatal("service was called for unsupported operation")
	}
	if !strings.Contains(response.Body.String(), `"errors"`) {
		t.Fatalf("body = %s, want GraphQL errors", response.Body.String())
	}
}

func TestHandlerRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	service := serviceStub{get: func(context.Context, string, string, string) (repository.Repository, error) {
		t.Fatal("service called for malformed JSON")
		return repository.Repository{}, nil
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(`{"query":`))
	response := httptest.NewRecorder()
	NewRouter(service, nil).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestHandlerDispatchesPullRequestForBranch(t *testing.T) {
	t.Parallel()

	prService := pullRequestServiceStub{find: func(_ context.Context, query pullrequest.Query) (pullrequest.Result, error) {
		if query.Owner != "foo" || query.Repo != "bar" || query.HeadRefName != "feature" || query.Authorization != "token secret" {
			t.Fatalf("query = %#v", query)
		}
		return pullrequest.Result{
			DefaultBranch: "main",
			Nodes: []pullrequest.PullRequest{{
				Number:      1,
				URL:         "https://git.example.test/foo/bar/pulls/1",
				State:       pullrequest.StateOpen,
				ID:          "11",
				BaseRefName: "main",
				HeadRefName: "feature",
				HeadRepositoryOwner: &pullrequest.RepositoryOwner{
					ID: "7", Login: "foo", Name: "Foo",
				},
			}},
		}, nil
	}}
	response := performGraphQLRequest(t, NewRouter(nil, prService), pullRequestForBranchQuery, "", "token secret")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data := body["data"].(map[string]any)
	repo := data["repository"].(map[string]any)
	nodes := repo["pullRequests"].(map[string]any)["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["state"] != "OPEN" {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestHandlerReturnsEmptyPullRequestNodesWithoutErrors(t *testing.T) {
	t.Parallel()

	prService := pullRequestServiceStub{find: func(context.Context, pullrequest.Query) (pullrequest.Result, error) {
		return pullrequest.Result{DefaultBranch: "main", Nodes: []pullrequest.PullRequest{}}, nil
	}}
	response := performGraphQLRequest(t, NewRouter(nil, prService), pullRequestForBranchQuery, "PullRequestForBranch", "")
	got := strings.TrimSpace(response.Body.String())
	want := `{"data":{"repository":{"pullRequests":{"nodes":[]},"defaultBranchRef":{"name":"main"}}}}`
	if response.Code != http.StatusOK || got != want {
		t.Fatalf("status/body = %d %s, want 200 %s", response.Code, got, want)
	}
}

func TestHandlerHidesPullRequestServiceErrors(t *testing.T) {
	t.Parallel()

	prService := pullRequestServiceStub{find: func(context.Context, pullrequest.Query) (pullrequest.Result, error) {
		return pullrequest.Result{}, fmt.Errorf("wrapped: %w: gitea secret", pullrequest.ErrForbidden)
	}}
	response := performGraphQLRequest(t, NewRouter(nil, prService), pullRequestForBranchQuery, "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"type":"FORBIDDEN"`) {
		t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "gitea secret") {
		t.Fatalf("body leaked service error: %s", response.Body.String())
	}
}

func performGraphQLRequest(t *testing.T, handler http.Handler, query, operationName, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"query":         query,
		"operationName": operationName,
		"variables": map[string]any{
			"owner":       "foo",
			"name":        "bar",
			"repo":        "bar",
			"headRefName": "feature",
			"states":      nil,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/graphql", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
