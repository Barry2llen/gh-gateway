package githubrest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gh-gateway/internal/actions"
)

type actionsServiceStub struct {
	runs          actions.RunPage
	jobs          actions.JobPage
	run           actions.WorkflowRun
	workflow      actions.Workflow
	log           actions.JobLog
	err           error
	callCount     int
	headSHA       string
	perPage       int
	runID         int64
	workflowID    int64
	authorization string
}

func (s *actionsServiceStub) ListRuns(_ context.Context, _, _, headSHA string, perPage int, authorization string) (actions.RunPage, error) {
	s.callCount++
	s.headSHA, s.perPage, s.authorization = headSHA, perPage, authorization
	return s.runs, s.err
}
func (s *actionsServiceStub) ListJobs(_ context.Context, _, _ string, runID int64, perPage int, _ string) (actions.JobPage, error) {
	s.callCount++
	s.runID, s.perPage = runID, perPage
	return s.jobs, s.err
}
func (s *actionsServiceStub) GetRun(_ context.Context, _, _ string, runID int64, _ string) (actions.WorkflowRun, error) {
	s.callCount++
	s.runID = runID
	return s.run, s.err
}
func (s *actionsServiceStub) GetWorkflow(_ context.Context, _, _ string, workflowID int64, _ string) (actions.Workflow, error) {
	s.callCount++
	s.workflowID = workflowID
	return s.workflow, s.err
}
func (s *actionsServiceStub) OpenJobLog(_ context.Context, _, _ string, jobID int64, _ string) (actions.JobLog, error) {
	s.callCount++
	s.runID = jobID
	return s.log, s.err
}

func actionsRouter(service ActionsService) http.Handler {
	return NewRouter(Handlers{Actions: NewActionsHandler(service)})
}

func TestActionsRunsResponseAndQuery(t *testing.T) {
	stub := &actionsServiceStub{runs: actions.RunPage{TotalCount: 1, Runs: []actions.WorkflowRun{{ID: 101, DisplayTitle: "CI", Status: "completed", Conclusion: "failure", HeadSHA: "abc", HTMLURL: "https://git/runs/1"}}}}
	request := httptest.NewRequest(http.MethodGet, "/repos/o/r/actions/runs?head_sha=abc&per_page=100", nil)
	request.Header.Set("Authorization", "Bearer incoming")
	response := httptest.NewRecorder()
	actionsRouter(stub).ServeHTTP(response, request)
	want := `{"total_count":1,"workflow_runs":[{"id":101,"display_title":"CI","status":"completed","conclusion":"failure","head_sha":"abc","html_url":"https://git/runs/1"}]}`
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != want {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if stub.headSHA != "abc" || stub.perPage != 100 || stub.authorization != "Bearer incoming" {
		t.Fatalf("query/auth = %q/%d/%q", stub.headSHA, stub.perPage, stub.authorization)
	}
}

func TestActionsJobsKeepDuplicateNames(t *testing.T) {
	stub := &actionsServiceStub{jobs: actions.JobPage{TotalCount: 2, Jobs: []actions.WorkflowJob{
		{ID: 201, Name: "matrix", Status: "completed", Conclusion: "failure"},
		{ID: 202, Name: "matrix", Status: "completed", Conclusion: "cancelled"},
	}}}
	response := httptest.NewRecorder()
	actionsRouter(stub).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repos/o/r/actions/runs/101/jobs?per_page=100", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":201`) || !strings.Contains(response.Body.String(), `"id":202`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if stub.runID != 101 || stub.perPage != 100 {
		t.Fatalf("run/page = %d/%d", stub.runID, stub.perPage)
	}
}

func TestActionsRunAndWorkflowPreflight(t *testing.T) {
	stub := &actionsServiceStub{
		run:      actions.WorkflowRun{ID: 101, DisplayTitle: "CI", Status: "completed", Conclusion: "failure", HeadSHA: "abc", WorkflowID: 987},
		workflow: actions.Workflow{ID: 987, Name: "CI"},
	}
	router := actionsRouter(stub)
	runResponse := httptest.NewRecorder()
	router.ServeHTTP(runResponse, httptest.NewRequest(http.MethodGet, "/repos/o/r/actions/runs/101?exclude_pull_requests=true", nil))
	if runResponse.Code != http.StatusOK || !strings.Contains(runResponse.Body.String(), `"workflow_id":987`) {
		t.Fatalf("run response = %d %s", runResponse.Code, runResponse.Body.String())
	}
	workflowResponse := httptest.NewRecorder()
	router.ServeHTTP(workflowResponse, httptest.NewRequest(http.MethodGet, "/repos/o/r/actions/workflows/987", nil))
	if workflowResponse.Code != http.StatusOK || strings.TrimSpace(workflowResponse.Body.String()) != `{"id":987,"name":"CI"}` {
		t.Fatalf("workflow response = %d %s", workflowResponse.Code, workflowResponse.Body.String())
	}
}

func TestActionsJobLogIsPlainText(t *testing.T) {
	stub := &actionsServiceStub{log: actions.JobLog{Body: io.NopCloser(strings.NewReader("one\n\x00two\n")), ContentType: "text/plain; charset=utf-8", ContentDisposition: "attachment"}}
	response := httptest.NewRecorder()
	actionsRouter(stub).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repos/o/r/actions/jobs/201/logs", nil))
	if response.Code != http.StatusOK || response.Body.String() != "one\n\x00two\n" || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Header().Get("Content-Disposition") != "attachment" {
		t.Fatalf("response = %d %q headers=%v", response.Code, response.Body.String(), response.Header())
	}
}

func TestActionsErrors(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		body   string
	}{
		{actions.ErrNotFound, 404, "Not Found"},
		{actions.ErrUnauthorized, 401, "Requires authentication"},
		{actions.ErrForbidden, 403, "Resource not accessible"},
		{errors.New("Gitea internal secret"), 502, "upstream service"},
	} {
		stub := &actionsServiceStub{err: tt.err}
		response := httptest.NewRecorder()
		actionsRouter(stub).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repos/o/r/actions/runs", nil))
		if response.Code != tt.status || !strings.Contains(response.Body.String(), tt.body) || strings.Contains(response.Body.String(), "Gitea internal secret") {
			t.Fatalf("error %v response = %d %s", tt.err, response.Code, response.Body.String())
		}
	}
}

func TestUnsupportedRerunDoesNotCallService(t *testing.T) {
	stub := &actionsServiceStub{}
	response := httptest.NewRecorder()
	actionsRouter(stub).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/repos/o/r/actions/runs/101/rerun-failed-jobs", nil))
	want := `{"message":"Failed-only workflow rerun is not supported by this Gitea compatibility profile."}`
	if response.Code != http.StatusNotImplemented || strings.TrimSpace(response.Body.String()) != want || stub.callCount != 0 {
		t.Fatalf("response/calls = %d %s / %d", response.Code, response.Body.String(), stub.callCount)
	}
}

func TestActionsRejectInvalidParameters(t *testing.T) {
	stub := &actionsServiceStub{}
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/repos/o/r/actions/runs?per_page=101", nil),
		httptest.NewRequest(http.MethodGet, "/repos/o/r/actions/runs/not-an-id", nil),
	} {
		response := httptest.NewRecorder()
		actionsRouter(stub).ServeHTTP(response, request)
		if response.Code != http.StatusUnprocessableEntity && response.Code != http.StatusNotFound {
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
	}
	if stub.callCount != 0 {
		t.Fatalf("calls = %d", stub.callCount)
	}
}
