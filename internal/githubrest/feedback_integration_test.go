package githubrest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gh-gateway/internal/feedback"
	"gh-gateway/internal/gateway"
	"gh-gateway/internal/gitea"
)

func TestConversationCommentsVerticalSlice(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token incoming" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/v1/repos/foo/bar/issues/12/comments":
			_, _ = w.Write([]byte(`[{"id":7,"user":{"login":"reviewer"},"created_at":"2026-09-15T01:02:03Z","body":"hello","html_url":"https://git.example.test/foo/bar/issues/12#issuecomment-7"}]`))
		case "/api/v1/user":
			_, _ = w.Write([]byte(`{"login":"current"}`))
		case "/api/v1/repos/foo/bar/collaborators/reviewer":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, _ := gitea.NewClient(server.URL, "", server.Client())
	rest := NewRouter(Handlers{ConversationComments: NewConversationCommentsHandler(feedback.NewService(provider))})
	router := gateway.NewRouter(http.NotFoundHandler(), rest)
	request := httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/issues/12/comments?per_page=100&page=1", nil)
	request.Header.Set("Authorization", "token incoming")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"author_association":"COLLABORATOR"`) {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestConversationCommentsVerticalSliceHidesUpstreamBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"gitea private detail"}`))
	}))
	defer server.Close()
	provider, _ := gitea.NewClient(server.URL, "", server.Client())
	router := gateway.NewRouter(http.NotFoundHandler(), NewRouter(Handlers{ConversationComments: NewConversationCommentsHandler(feedback.NewService(provider))}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/repos/foo/bar/issues/12/comments", nil))
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "gitea") {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}
