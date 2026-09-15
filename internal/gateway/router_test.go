package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRouterDispatchesEnterpriseGraphQLAndRESTPaths(t *testing.T) {
	t.Parallel()

	graphQL := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(210) })
	rest := chi.NewRouter()
	rest.Get("/repos/{owner}/{repo}/commits/{sha}/pulls", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(211) })
	rest.Get("/user", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(212) })
	rest.Get("/repos/{owner}/{repo}/issues/{number}/comments", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(213) })
	router := NewRouter(graphQL, rest)

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{method: http.MethodPost, path: "/api/graphql", want: 210},
		{method: http.MethodGet, path: "/api/v3/repos/foo/bar/commits/abc/pulls", want: 211},
		{method: http.MethodGet, path: "/api/v3/user", want: 212},
		{method: http.MethodGet, path: "/api/v3/repos/foo/bar/issues/12/comments", want: 213},
		{method: http.MethodGet, path: "/repos/foo/bar/commits/abc/pulls", want: http.StatusNotFound},
		{method: http.MethodGet, path: "/user", want: http.StatusNotFound},
	}
	for _, tt := range tests {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
		if response.Code != tt.want {
			t.Errorf("%s %s status = %d, want %d", tt.method, tt.path, response.Code, tt.want)
		}
	}
}
