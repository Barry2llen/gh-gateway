package githubrest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gh-gateway/internal/actions"
	"gh-gateway/internal/gateway"
	"gh-gateway/internal/gitea"
)

func TestActionsVerticalSlice(t *testing.T) {
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if got := r.Header.Get("Authorization"); got != "token incoming" {
			t.Errorf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/api/v1/repos/o/r/actions/runs":
			want := url.Values{"head_sha": {"abc"}, "page": {"1"}, "limit": {"100"}}
			if r.URL.Query().Encode() != want.Encode() {
				t.Errorf("query = %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"total_count":1,"workflow_runs":[{"id":101,"display_title":"CI","path":".gitea/workflows/ci.yml@refs/heads/feature","status":"completed","conclusion":"failure","head_sha":"abc","html_url":"https://git/runs/1"}]}`)
		case "/api/v1/repos/o/r/actions/runs/101/jobs":
			_, _ = io.WriteString(w, `{"total_count":1,"jobs":[{"id":201,"name":"test","status":"completed","conclusion":"failure","html_url":"https://git/jobs/0"}]}`)
		case "/api/v1/repos/o/r/actions/runs/101":
			_, _ = io.WriteString(w, `{"id":101,"display_title":"CI","path":".gitea/workflows/ci.yml@refs/heads/feature","status":"completed","conclusion":"failure","head_sha":"abc"}`)
		case "/api/v1/repos/o/r/actions/workflows":
			_, _ = io.WriteString(w, `{"total_count":1,"workflows":[{"id":"ci.yml","name":"CI"}]}`)
		case "/api/v1/repos/o/r/actions/workflows/ci.yml":
			_, _ = io.WriteString(w, `{"id":"ci.yml","name":"CI"}`)
		case "/api/v1/repos/o/r/actions/jobs/201/logs":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "real log\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, _ := gitea.NewClient(server.URL, "", server.Client())
	router := gateway.NewRouter(http.NotFoundHandler(), NewRouter(Handlers{Actions: NewActionsHandler(actions.NewService(provider))}))

	perform := func(method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("Authorization", "token incoming")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	if response := perform(http.MethodGet, "/api/v3/repos/o/r/actions/runs?head_sha=abc&per_page=100"); response.Code != 200 || !strings.Contains(response.Body.String(), `"id":101`) {
		t.Fatalf("runs = %d %s", response.Code, response.Body.String())
	}
	if response := perform(http.MethodGet, "/api/v3/repos/o/r/actions/runs/101/jobs?per_page=100"); response.Code != 200 || !strings.Contains(response.Body.String(), `"id":201`) {
		t.Fatalf("jobs = %d %s", response.Code, response.Body.String())
	}
	run := perform(http.MethodGet, "/api/v3/repos/o/r/actions/runs/101?exclude_pull_requests=true")
	if run.Code != 200 {
		t.Fatalf("run = %d %s", run.Code, run.Body.String())
	}
	workflowID := actions.WorkflowSurrogateID("o", "r", "ci.yml")
	if !strings.Contains(run.Body.String(), fmt.Sprintf(`"workflow_id":%d`, workflowID)) {
		t.Fatalf("run = %s", run.Body.String())
	}
	if response := perform(http.MethodGet, fmt.Sprintf("/api/v3/repos/o/r/actions/workflows/%d", workflowID)); response.Code != 200 || !strings.Contains(response.Body.String(), `"name":"CI"`) {
		t.Fatalf("workflow = %d %s", response.Code, response.Body.String())
	}
	if response := perform(http.MethodGet, "/api/v3/repos/o/r/actions/jobs/201/logs"); response.Code != 200 || response.Body.String() != "real log\n" {
		t.Fatalf("log = %d %q", response.Code, response.Body.String())
	}
	beforeUnsupported := len(requests)
	if response := perform(http.MethodPost, "/api/v3/repos/o/r/actions/runs/101/rerun-failed-jobs"); response.Code != 501 {
		t.Fatalf("rerun = %d %s", response.Code, response.Body.String())
	}
	if len(requests) != beforeUnsupported {
		t.Fatalf("unsupported rerun reached Gitea: %v", requests[beforeUnsupported:])
	}
}
