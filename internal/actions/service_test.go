package actions

import (
	"context"
	"io"
	"strings"
	"testing"
)

type providerStub struct {
	runs       RunPage
	jobs       JobPage
	run        WorkflowRun
	workflows  []Workflow
	workflow   Workflow
	log        JobLog
	err        error
	runPage    int
	runLimit   int
	jobPage    int
	jobLimit   int
	workflowID string
}

func (p *providerStub) ListWorkflowRuns(_ context.Context, _, _, _ string, page, limit int, _ string) (RunPage, error) {
	p.runPage, p.runLimit = page, limit
	return p.runs, p.err
}
func (p *providerStub) ListWorkflowJobs(_ context.Context, _, _ string, _ int64, page, limit int, _ string) (JobPage, error) {
	p.jobPage, p.jobLimit = page, limit
	return p.jobs, p.err
}
func (p *providerStub) GetWorkflowRun(context.Context, string, string, int64, string) (WorkflowRun, error) {
	return p.run, p.err
}
func (p *providerStub) ListWorkflows(context.Context, string, string, string) ([]Workflow, error) {
	return p.workflows, p.err
}
func (p *providerStub) GetWorkflow(_ context.Context, _, _, id, _ string) (Workflow, error) {
	p.workflowID = id
	return p.workflow, p.err
}
func (p *providerStub) OpenJobLog(context.Context, string, string, int64, string) (JobLog, error) {
	return p.log, p.err
}

func TestListRunsUsesFirstPageAndPreservesItems(t *testing.T) {
	p := &providerStub{runs: RunPage{TotalCount: 2, Runs: []WorkflowRun{
		{ID: 11, Status: "completed", Conclusion: "failure"},
		{ID: 12, Status: "completed", Conclusion: "skipped"},
	}}}
	got, err := NewService(p).ListRuns(context.Background(), "o", "r", "sha", 100, "token")
	if err != nil {
		t.Fatal(err)
	}
	if p.runPage != 1 || p.runLimit != 100 || got.TotalCount != 2 || len(got.Runs) != 2 {
		t.Fatalf("page/limit/result = %d/%d/%+v", p.runPage, p.runLimit, got)
	}
}

func TestListJobsKeepsDuplicateMatrixNames(t *testing.T) {
	p := &providerStub{jobs: JobPage{TotalCount: 2, Jobs: []WorkflowJob{
		{ID: 21, Name: "matrix", Status: "completed", Conclusion: "failure"},
		{ID: 22, Name: "matrix", Status: "completed", Conclusion: "cancelled"},
	}}}
	got, err := NewService(p).ListJobs(context.Background(), "o", "r", 11, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.jobPage != 1 || p.jobLimit != 100 || len(got.Jobs) != 2 || got.Jobs[0].ID == got.Jobs[1].ID {
		t.Fatalf("unexpected jobs result: %+v", got)
	}
}

func TestUnknownStatusIsRejected(t *testing.T) {
	for _, run := range []WorkflowRun{{Status: "", Conclusion: ""}, {Status: "completed", Conclusion: "warning"}, {Status: "queued", Conclusion: "success"}} {
		p := &providerStub{runs: RunPage{Runs: []WorkflowRun{run}}}
		if _, err := NewService(p).ListRuns(context.Background(), "o", "r", "", 30, ""); err == nil {
			t.Fatalf("status/conclusion %q/%q unexpectedly accepted", run.Status, run.Conclusion)
		}
	}
}

func TestUnknownJobStatusIsRejected(t *testing.T) {
	p := &providerStub{jobs: JobPage{Jobs: []WorkflowJob{{ID: 1, Name: "job", Status: "completed", Conclusion: "warning"}}}}
	if _, err := NewService(p).ListJobs(context.Background(), "o", "r", 1, 100, ""); err == nil {
		t.Fatal("warning job conclusion unexpectedly accepted")
	}
}

func TestRunAndWorkflowUseStableSurrogateID(t *testing.T) {
	wantID := WorkflowSurrogateID("o", "r", "ci.yml")
	p := &providerStub{
		run:       WorkflowRun{ID: 9, Status: "completed", Conclusion: "failure", ProviderWorkflowID: "ci.yml"},
		workflows: []Workflow{{ProviderID: "ci.yml", Name: "CI"}},
		workflow:  Workflow{ProviderID: "ci.yml", Name: "CI"},
	}
	s := NewService(p)
	run, err := s.GetRun(context.Background(), "o", "r", 9, "")
	if err != nil || run.WorkflowID != wantID {
		t.Fatalf("GetRun() = %+v, %v", run, err)
	}
	workflow, err := s.GetWorkflow(context.Background(), "o", "r", wantID, "")
	if err != nil || workflow.ID != wantID || p.workflowID != "ci.yml" {
		t.Fatalf("GetWorkflow() = %+v, %v, provider ID %q", workflow, err, p.workflowID)
	}
}

func TestWorkflowSurrogateCollisionIsRejected(t *testing.T) {
	id := WorkflowSurrogateID("o", "r", "ci.yml")
	p := &providerStub{workflows: []Workflow{{ProviderID: "ci.yml"}, {ProviderID: "ci.yml"}}}
	if _, err := NewService(p).GetWorkflow(context.Background(), "o", "r", id, ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenJobLogPassesThrough(t *testing.T) {
	p := &providerStub{log: JobLog{Body: io.NopCloser(strings.NewReader("hello")), ContentType: "text/plain"}}
	log, err := NewService(p).OpenJobLog(context.Background(), "o", "r", 1, "")
	if err != nil || log.ContentType != "text/plain" {
		t.Fatalf("OpenJobLog() = %+v, %v", log, err)
	}
	_ = log.Body.Close()
}
