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
	"time"

	"gh-gateway/internal/pullrequest"
	"gh-gateway/internal/repository"
	"gh-gateway/internal/statuscheck"
)

type serviceStub struct {
	get func(context.Context, string, string, string) (repository.Repository, error)
}

type pullRequestServiceStub struct {
	find         func(context.Context, pullrequest.Query) (pullrequest.Result, error)
	findByNumber func(context.Context, pullrequest.NumberQuery) (pullrequest.PullRequest, error)
}

type statusCheckServiceStub struct {
	result statuscheck.Result
	err    error
	query  statuscheck.Query
}

func (s *statusCheckServiceStub) Get(_ context.Context, query statuscheck.Query) (statuscheck.Result, error) {
	s.query = query
	return s.result, s.err
}

func (s pullRequestServiceStub) FindForBranch(ctx context.Context, query pullrequest.Query) (pullrequest.Result, error) {
	return s.find(ctx, query)
}

func (s pullRequestServiceStub) FindByNumber(ctx context.Context, query pullrequest.NumberQuery) (pullrequest.PullRequest, error) {
	return s.findByNumber(ctx, query)
}

func (s serviceStub) Get(ctx context.Context, owner, name, authorization string) (repository.Repository, error) {
	return s.get(ctx, owner, name, authorization)
}

func TestHandlerDispatchesPullRequestByNumberWithExpandedMetadata(t *testing.T) {
	t.Parallel()

	mergedAt := mustTime(t, "2026-09-15T01:02:03Z")
	service := pullRequestServiceStub{findByNumber: func(_ context.Context, query pullrequest.NumberQuery) (pullrequest.PullRequest, error) {
		if query.Owner != "foo" || query.Repo != "bar" || query.Number != 12 || query.Authorization != "token secret" {
			t.Fatalf("query = %#v", query)
		}
		return pullrequest.PullRequest{
			Number: 12, URL: "https://git.example.test/foo/bar/pulls/12", State: pullrequest.StateMerged,
			MergedAt: &mergedAt, HeadRefName: "feature", HeadSHA: "abc123", Mergeable: pullrequest.Mergeable,
			MergeStateStatus:    pullrequest.MergeStateUnknown,
			HeadRepository:      &pullrequest.Repository{ID: "20", Name: "bar", NameWithOwner: "forker/bar"},
			HeadRepositoryOwner: &pullrequest.RepositoryOwner{ID: "2", Login: "forker", Name: "Fork User"},
		}, nil
	}}
	response := performGraphQLRequest(t, NewRouter(nil, service), pullRequestByNumberQuery, "PullRequestByNumber", "token secret")
	if response.Code != http.StatusOK {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{`"number":12`, `"state":"MERGED"`, `"headRefOid":"abc123"`, `"nameWithOwner":"forker/bar"`, `"mergeable":"MERGEABLE"`, `"mergeStateStatus":"UNKNOWN"`, `"reviewDecision":null`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s, missing %s", body, want)
		}
	}
	if strings.Contains(body, `"baseRefName"`) {
		t.Fatalf("body returned unselected field: %s", body)
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestHandlerSupportsChecksFeatureDetection(t *testing.T) {
	t.Parallel()
	queries := []struct{ query, want, absent string }{{`query PullRequest_fields{PullRequest:__type(name:"PullRequest"){fields(includeDeprecated:true){name}} StatusCheckRollupContextConnection:__type(name:"StatusCheckRollupContextConnection"){fields(includeDeprecated:true){name}}}`, `"PullRequest"`, `isInMergeQueue`}, {`query PullRequest_fields2{WorkflowRun:__type(name:"WorkflowRun"){fields(includeDeprecated:true){name}}}`, `"WorkflowRun"`, `"event"`}}
	for _, tt := range queries {
		response := performGraphQLRequest(t, NewRouter(nil, nil, nil), tt.query, "", "")
		if response.Code != 200 || !strings.Contains(response.Body.String(), tt.want) || strings.Contains(response.Body.String(), tt.absent) {
			t.Fatalf("body = %s", response.Body.String())
		}
	}
}

func TestHandlerReturnsStatusContextsWithCursorPageInfo(t *testing.T) {
	t.Parallel()
	contexts := make([]statuscheck.Context, 101)
	for i := range contexts {
		contexts[i] = statuscheck.Context{Name: fmt.Sprintf("ctx-%03d", i), State: statuscheck.StateSuccess, CreatedAt: time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC)}
	}
	id := encodePullRequestID("foo", "bar", 12)
	service := &statusCheckServiceStub{result: statuscheck.Result{HeadSHA: "head", Contexts: contexts}}
	response := performGraphQLRequestWithVariables(t, NewRouter(nil, nil, service), pullRequestStatusChecksQuery, "PullRequestStatusChecks", map[string]any{"id": id, "endCursor": nil}, "token")
	body := response.Body.String()
	if response.Code != 200 || !strings.Contains(body, `"__typename":"StatusContext"`) || !strings.Contains(body, `"hasNextPage":true`) || !strings.Contains(body, `"isRequired":false`) {
		t.Fatalf("body = %s", body)
	}
	if service.query.Owner != "foo" || service.query.Repo != "bar" || service.query.Number != 12 || service.query.Authorization != "token" {
		t.Fatalf("query = %#v", service.query)
	}
}

func TestHandlerStatusCursorAdvancesWithoutRepeating(t *testing.T) {
	t.Parallel()
	contexts := make([]statuscheck.Context, 101)
	for i := range contexts {
		contexts[i] = statuscheck.Context{Name: fmt.Sprintf("ctx-%03d", i), State: statuscheck.StateSuccess, CreatedAt: time.Now().UTC()}
	}
	id := encodePullRequestID("foo", "bar", 12)
	service := &statusCheckServiceStub{result: statuscheck.Result{HeadSHA: "head", Contexts: contexts}}
	first := performGraphQLRequestWithVariables(t, NewRouter(nil, nil, service), pullRequestStatusChecksQuery, "", map[string]any{"id": id, "endCursor": nil}, "")
	var payload struct {
		Data struct {
			Node struct {
				Rollup struct {
					Nodes []struct {
						Commit struct {
							Rollup struct {
								Contexts struct {
									PageInfo struct {
										EndCursor string `json:"endCursor"`
									} `json:"pageInfo"`
								} `json:"contexts"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"statusCheckRollup"`
			} `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	cursor := payload.Data.Node.Rollup.Nodes[0].Commit.Rollup.Contexts.PageInfo.EndCursor
	if cursor == "" {
		t.Fatalf("first body = %s", first.Body.String())
	}
	second := performGraphQLRequestWithVariables(t, NewRouter(nil, nil, service), pullRequestStatusChecksQuery, "", map[string]any{"id": id, "endCursor": cursor}, "")
	if strings.Contains(second.Body.String(), `"context":"ctx-099"`) || !strings.Contains(second.Body.String(), `"context":"ctx-100"`) || !strings.Contains(second.Body.String(), `"hasNextPage":false`) {
		t.Fatalf("second body = %s", second.Body.String())
	}
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
	return performGraphQLRequestWithVariables(t, handler, query, operationName, map[string]any{
		"owner":       "foo",
		"name":        "bar",
		"repo":        "bar",
		"headRefName": "feature",
		"states":      nil,
		"pr_number":   12,
	}, authorization)
}

func performGraphQLRequestWithVariables(t *testing.T, handler http.Handler, query, operationName string, variables map[string]any, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "operationName": operationName, "variables": variables})
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
