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

func TestTransparentRouterFallsBackAndRejectsOtherHosts(t *testing.T) {
	t.Parallel()
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/assets/app.js" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(214)
	})
	router := NewTransparentRouter(http.NotFoundHandler(), http.NotFoundHandler(), fallback, "git.example.com")

	request := httptest.NewRequest(http.MethodGet, "https://git.example.com/assets/app.js", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 214 {
		t.Fatalf("fallback status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "https://evil.example/assets/app.js", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusMisdirectedRequest {
		t.Fatalf("mismatched host status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "https://git.example.com/assets/app.js", nil)
	request.Host = "attacker@git.example.com"
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusMisdirectedRequest {
		t.Fatalf("userinfo host status = %d", response.Code)
	}
}
