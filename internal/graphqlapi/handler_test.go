package graphqlapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gh-gateway/internal/repository"
)

type serviceStub struct {
	get func(context.Context, string, string, string) (repository.Repository, error)
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

	response := performGraphQLRequest(t, NewRouter(service), repositoryInfoQuery, "RepositoryInfo", "token secret")
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
	response := performGraphQLRequest(t, NewRouter(service), repositoryInfoQuery, "", "")

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
	response := performGraphQLRequest(t, NewRouter(service), `query ViewerInfo { viewer { login } }`, "", "")
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
	NewRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func performGraphQLRequest(t *testing.T, handler http.Handler, query, operationName, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"query":         query,
		"operationName": operationName,
		"variables": map[string]any{
			"owner": "foo",
			"name":  "bar",
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
