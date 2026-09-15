package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterDispatchesEnterpriseGraphQLAndRESTPaths(t *testing.T) {
	t.Parallel()

	graphQL := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(210) })
	commitPulls := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(211) })
	router := NewRouter(graphQL, commitPulls)

	tests := []struct {
		method string
		path   string
		want   int
	}{
		{method: http.MethodPost, path: "/api/graphql", want: 210},
		{method: http.MethodGet, path: "/api/v3/repos/foo/bar/commits/abc/pulls", want: 211},
		{method: http.MethodGet, path: "/repos/foo/bar/commits/abc/pulls", want: http.StatusNotFound},
	}
	for _, tt := range tests {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
		if response.Code != tt.want {
			t.Errorf("%s %s status = %d, want %d", tt.method, tt.path, response.Code, tt.want)
		}
	}
}
