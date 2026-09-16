package gitea

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gh-gateway/internal/actions"
)

func TestListWorkflowRunsMapsQueryAndEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/repos/o/r/actions/runs" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		want := url.Values{"head_sha": {"abc"}, "page": {"1"}, "limit": {"100"}}
		if r.URL.Query().Encode() != want.Encode() {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "token fixed" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"total_count":1,"workflow_runs":[{"id":101,"display_title":"CI","path":".gitea/workflows/ci.yml@refs/heads/main","status":"completed","conclusion":"failure","head_sha":"abc","html_url":"https://git/runs/1"}]}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "fixed", server.Client())
	got, err := c.ListWorkflowRuns(context.Background(), "o", "r", "abc", 1, 100, "incoming")
	if err != nil || got.TotalCount != 1 || got.Runs[0].ID != 101 || got.Runs[0].DisplayTitle != "CI" {
		t.Fatalf("result = %+v, %v", got, err)
	}
}

func TestListWorkflowJobsPreservesMatrixJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/o/r/actions/runs/101/jobs" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "100" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"total_count":2,"jobs":[{"id":201,"name":"matrix","status":"completed","conclusion":"failure","html_url":"https://git/jobs/0"},{"id":202,"name":"matrix","status":"completed","conclusion":"cancelled","html_url":"https://git/jobs/1"}]}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "", server.Client())
	got, err := c.ListWorkflowJobs(context.Background(), "o", "r", 101, 1, 100, "")
	if err != nil || len(got.Jobs) != 2 || got.Jobs[0].ID == got.Jobs[1].ID {
		t.Fatalf("result = %+v, %v", got, err)
	}
}

func TestGetWorkflowRunExtractsFilenameAndKeepsDatabaseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":101,"run_number":7,"display_title":"CI","path":".gitea/workflows/ci.yml@refs/heads/feature","status":"completed","conclusion":"failure","head_sha":"abc"}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "", server.Client())
	got, err := c.GetWorkflowRun(context.Background(), "o", "r", 101, "")
	if err != nil || got.ID != 101 || got.ProviderWorkflowID != "ci.yml" {
		t.Fatalf("result = %+v, %v", got, err)
	}
}

func TestWorkflowsListThenGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/o/r/actions/workflows":
			_, _ = io.WriteString(w, `{"total_count":1,"workflows":[{"id":"ci.yml","name":"CI"}]}`)
		case "/api/v1/repos/o/r/actions/workflows/ci.yml":
			_, _ = io.WriteString(w, `{"id":"ci.yml","name":"CI"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "", server.Client())
	listed, err := c.ListWorkflows(context.Background(), "o", "r", "")
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	got, err := c.GetWorkflow(context.Background(), "o", "r", "ci.yml", "")
	if err != nil || got.ProviderID != "ci.yml" || got.Name != "CI" {
		t.Fatalf("get = %+v, %v", got, err)
	}
}

func TestOpenJobLogPassesBytesAndHeaders(t *testing.T) {
	want := "line one\n\x00line two\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/plain" || r.URL.Path != "/api/v1/repos/o/r/actions/jobs/201/logs" {
			t.Fatalf("request = %s accept=%q", r.URL.Path, r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment")
		_, _ = io.WriteString(w, want)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "", server.Client())
	log, err := c.OpenJobLog(context.Background(), "o", "r", 201, "")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Body.Close()
	body, _ := io.ReadAll(log.Body)
	if string(body) != want || !strings.HasPrefix(log.ContentType, "text/plain") || log.ContentDisposition != "attachment" {
		t.Fatalf("log = %q %+v", body, log)
	}
}

func TestActionsErrorsAreMappedWithoutBodies(t *testing.T) {
	for _, tt := range []struct {
		status int
		want   error
	}{{404, actions.ErrNotFound}, {401, actions.ErrUnauthorized}, {403, actions.ErrForbidden}, {500, nil}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tt.status)
			_, _ = io.WriteString(w, `{"message":"internal secret"}`)
		}))
		c, _ := NewClient(server.URL, "", server.Client())
		_, err := c.ListWorkflowRuns(context.Background(), "o", "r", "", 1, 30, "")
		server.Close()
		if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) || strings.Contains(err.Error(), "internal secret") {
			t.Fatalf("status %d error = %v", tt.status, err)
		}
	}
}

func TestOpenJobLogErrorsAreMapped(t *testing.T) {
	for _, tt := range []struct {
		status int
		want   error
	}{{404, actions.ErrNotFound}, {401, actions.ErrUnauthorized}, {403, actions.ErrForbidden}, {500, nil}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tt.status)
			_, _ = io.WriteString(w, "private Gitea error")
		}))
		c, _ := NewClient(server.URL, "", server.Client())
		_, err := c.OpenJobLog(context.Background(), "o", "r", 201, "")
		server.Close()
		if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) || strings.Contains(err.Error(), "private Gitea error") {
			t.Fatalf("status %d error = %v", tt.status, err)
		}
	}
}
