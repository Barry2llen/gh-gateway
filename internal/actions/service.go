package actions

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var (
	ErrNotFound     = errors.New("actions resource not found")
	ErrUnauthorized = errors.New("actions authentication required")
	ErrForbidden    = errors.New("actions access forbidden")
)

type WorkflowRun struct {
	ID                 int64
	DisplayTitle       string
	Status             string
	Conclusion         string
	HeadSHA            string
	HTMLURL            string
	ProviderWorkflowID string
	WorkflowID         int64
}

type WorkflowJob struct {
	ID         int64
	Name       string
	Status     string
	Conclusion string
	HTMLURL    string
}

type Workflow struct {
	ID         int64
	ProviderID string
	Name       string
}

type RunPage struct {
	TotalCount int64
	Runs       []WorkflowRun
}

type JobPage struct {
	TotalCount int64
	Jobs       []WorkflowJob
}

type JobLog struct {
	Body               io.ReadCloser
	ContentType        string
	ContentDisposition string
}

type Provider interface {
	ListWorkflowRuns(context.Context, string, string, string, int, int, string) (RunPage, error)
	ListWorkflowJobs(context.Context, string, string, int64, int, int, string) (JobPage, error)
	GetWorkflowRun(context.Context, string, string, int64, string) (WorkflowRun, error)
	ListWorkflows(context.Context, string, string, string) ([]Workflow, error)
	GetWorkflow(context.Context, string, string, string, string) (Workflow, error)
	OpenJobLog(context.Context, string, string, int64, string) (JobLog, error)
}

type Service struct{ provider Provider }

func NewService(provider Provider) *Service { return &Service{provider: provider} }

func (s *Service) ListRuns(ctx context.Context, owner, repo, headSHA string, perPage int, authorization string) (RunPage, error) {
	page, err := s.provider.ListWorkflowRuns(ctx, owner, repo, headSHA, 1, perPage, authorization)
	if err != nil {
		return RunPage{}, err
	}
	for i := range page.Runs {
		if err := validateStatus(page.Runs[i].Status, page.Runs[i].Conclusion); err != nil {
			return RunPage{}, err
		}
	}
	return page, nil
}

func (s *Service) ListJobs(ctx context.Context, owner, repo string, runID int64, perPage int, authorization string) (JobPage, error) {
	page, err := s.provider.ListWorkflowJobs(ctx, owner, repo, runID, 1, perPage, authorization)
	if err != nil {
		return JobPage{}, err
	}
	for i := range page.Jobs {
		if err := validateStatus(page.Jobs[i].Status, page.Jobs[i].Conclusion); err != nil {
			return JobPage{}, err
		}
	}
	return page, nil
}

func (s *Service) GetRun(ctx context.Context, owner, repo string, runID int64, authorization string) (WorkflowRun, error) {
	run, err := s.provider.GetWorkflowRun(ctx, owner, repo, runID, authorization)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err := validateStatus(run.Status, run.Conclusion); err != nil {
		return WorkflowRun{}, err
	}
	if run.ProviderWorkflowID == "" {
		return WorkflowRun{}, errors.New("Gitea workflow run is missing workflow identity")
	}
	run.WorkflowID = WorkflowSurrogateID(owner, repo, run.ProviderWorkflowID)
	return run, nil
}

func (s *Service) GetWorkflow(ctx context.Context, owner, repo string, workflowID int64, authorization string) (Workflow, error) {
	workflows, err := s.provider.ListWorkflows(ctx, owner, repo, authorization)
	if err != nil {
		return Workflow{}, err
	}
	var match *Workflow
	for i := range workflows {
		candidate := workflows[i]
		if WorkflowSurrogateID(owner, repo, candidate.ProviderID) != workflowID {
			continue
		}
		if match != nil {
			return Workflow{}, fmt.Errorf("workflow surrogate ID %d is ambiguous", workflowID)
		}
		match = &candidate
	}
	if match == nil {
		return Workflow{}, ErrNotFound
	}
	workflow, err := s.provider.GetWorkflow(ctx, owner, repo, match.ProviderID, authorization)
	if err != nil {
		return Workflow{}, err
	}
	if workflow.ProviderID != match.ProviderID || workflow.Name == "" {
		return Workflow{}, errors.New("Gitea workflow response is inconsistent")
	}
	workflow.ID = workflowID
	return workflow, nil
}

func (s *Service) OpenJobLog(ctx context.Context, owner, repo string, jobID int64, authorization string) (JobLog, error) {
	return s.provider.OpenJobLog(ctx, owner, repo, jobID, authorization)
}

func WorkflowSurrogateID(owner, repo, providerID string) int64 {
	sum := sha256.Sum256([]byte(owner + "\x00" + repo + "\x00" + providerID))
	id := int64(binary.BigEndian.Uint64(sum[:8]) & uint64(^uint64(0)>>1))
	if id == 0 {
		return 1
	}
	return id
}

func validateStatus(status, conclusion string) error {
	switch status {
	case "queued", "waiting", "in_progress":
		if conclusion == "" {
			return nil
		}
	case "completed":
		switch conclusion {
		case "success", "failure", "cancelled", "skipped":
			return nil
		}
	}
	return fmt.Errorf("Gitea Actions returned unsupported status/conclusion %q/%q", status, conclusion)
}
