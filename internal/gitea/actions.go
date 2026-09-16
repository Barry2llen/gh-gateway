package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"gh-gateway/internal/actions"
)

type workflowRunDTO struct {
	ID           int64  `json:"id"`
	DisplayTitle string `json:"display_title"`
	Path         string `json:"path"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	HeadSHA      string `json:"head_sha"`
	HTMLURL      string `json:"html_url"`
}

type workflowRunsDTO struct {
	TotalCount int64            `json:"total_count"`
	Runs       []workflowRunDTO `json:"workflow_runs"`
}

type workflowJobDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
}

type workflowJobsDTO struct {
	TotalCount int64            `json:"total_count"`
	Jobs       []workflowJobDTO `json:"jobs"`
}

type workflowDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type workflowsDTO struct {
	Workflows []workflowDTO `json:"workflows"`
}

func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo, headSHA string, pageNumber, limit int, authorization string) (actions.RunPage, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "actions", "runs")
	query := endpoint.Query()
	if headSHA != "" {
		query.Set("head_sha", headSHA)
	}
	query.Set("page", strconv.Itoa(pageNumber))
	query.Set("limit", strconv.Itoa(limit))
	endpoint.RawQuery = query.Encode()

	var response workflowRunsDTO
	if err := c.getActionsJSON(ctx, endpoint.String(), authorization, &response); err != nil {
		return actions.RunPage{}, err
	}
	runs := make([]actions.WorkflowRun, 0, len(response.Runs))
	for _, dto := range response.Runs {
		if dto.ID <= 0 || dto.HeadSHA == "" {
			return actions.RunPage{}, errors.New("Gitea workflow run response is missing id or head_sha")
		}
		runs = append(runs, mapWorkflowRun(dto))
	}
	return actions.RunPage{TotalCount: response.TotalCount, Runs: runs}, nil
}

func (c *Client) ListWorkflowJobs(ctx context.Context, owner, repo string, runID int64, pageNumber, limit int, authorization string) (actions.JobPage, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "actions", "runs", strconv.FormatInt(runID, 10), "jobs")
	query := endpoint.Query()
	query.Set("page", strconv.Itoa(pageNumber))
	query.Set("limit", strconv.Itoa(limit))
	endpoint.RawQuery = query.Encode()

	var response workflowJobsDTO
	if err := c.getActionsJSON(ctx, endpoint.String(), authorization, &response); err != nil {
		return actions.JobPage{}, err
	}
	jobs := make([]actions.WorkflowJob, 0, len(response.Jobs))
	for _, dto := range response.Jobs {
		if dto.ID <= 0 || dto.Name == "" {
			return actions.JobPage{}, errors.New("Gitea workflow job response is missing id or name")
		}
		jobs = append(jobs, actions.WorkflowJob{ID: dto.ID, Name: dto.Name, Status: dto.Status, Conclusion: dto.Conclusion, HTMLURL: dto.HTMLURL})
	}
	return actions.JobPage{TotalCount: response.TotalCount, Jobs: jobs}, nil
}

func (c *Client) GetWorkflowRun(ctx context.Context, owner, repo string, runID int64, authorization string) (actions.WorkflowRun, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "actions", "runs", strconv.FormatInt(runID, 10))
	var dto workflowRunDTO
	if err := c.getActionsJSON(ctx, endpoint.String(), authorization, &dto); err != nil {
		return actions.WorkflowRun{}, err
	}
	if dto.ID <= 0 || dto.ID != runID {
		return actions.WorkflowRun{}, errors.New("Gitea workflow run response has inconsistent id")
	}
	mapped := mapWorkflowRun(dto)
	workflowPath, _, ok := strings.Cut(dto.Path, "@refs/")
	if !ok || workflowPath == "" || path.Base(workflowPath) == "." {
		return actions.WorkflowRun{}, errors.New("Gitea workflow run response has unsupported path")
	}
	mapped.ProviderWorkflowID = path.Base(workflowPath)
	return mapped, nil
}

func (c *Client) ListWorkflows(ctx context.Context, owner, repo, authorization string) ([]actions.Workflow, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "actions", "workflows")
	var response workflowsDTO
	if err := c.getActionsJSON(ctx, endpoint.String(), authorization, &response); err != nil {
		return nil, err
	}
	workflows := make([]actions.Workflow, 0, len(response.Workflows))
	for _, dto := range response.Workflows {
		if dto.ID == "" || dto.Name == "" {
			return nil, errors.New("Gitea workflow response is missing id or name")
		}
		workflows = append(workflows, actions.Workflow{ProviderID: dto.ID, Name: dto.Name})
	}
	return workflows, nil
}

func (c *Client) GetWorkflow(ctx context.Context, owner, repo, workflowID, authorization string) (actions.Workflow, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "actions", "workflows", workflowID)
	var dto workflowDTO
	if err := c.getActionsJSON(ctx, endpoint.String(), authorization, &dto); err != nil {
		return actions.Workflow{}, err
	}
	if dto.ID == "" || dto.Name == "" {
		return actions.Workflow{}, errors.New("Gitea workflow response is missing id or name")
	}
	return actions.Workflow{ProviderID: dto.ID, Name: dto.Name}, nil
}

func (c *Client) OpenJobLog(ctx context.Context, owner, repo string, jobID int64, authorization string) (actions.JobLog, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "actions", "jobs", strconv.FormatInt(jobID, 10), "logs")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return actions.JobLog{}, fmt.Errorf("create Gitea job log request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	c.applyAuthorization(request, authorization)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return actions.JobLog{}, fmt.Errorf("request Gitea job log: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		return actions.JobLog{}, actionsStatusError(response)
	}
	return actions.JobLog{
		Body:               response.Body,
		ContentType:        response.Header.Get("Content-Type"),
		ContentDisposition: response.Header.Get("Content-Disposition"),
	}, nil
}

func (c *Client) getActionsJSON(ctx context.Context, endpoint, authorization string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Gitea Actions request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	c.applyAuthorization(request, authorization)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Gitea Actions: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return actionsStatusError(response)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(target); err != nil {
		return fmt.Errorf("decode Gitea Actions response: %w", err)
	}
	return nil
}

func actionsStatusError(response *http.Response) error {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
	switch response.StatusCode {
	case http.StatusNotFound:
		return actions.ErrNotFound
	case http.StatusUnauthorized:
		return actions.ErrUnauthorized
	case http.StatusForbidden:
		return actions.ErrForbidden
	default:
		return fmt.Errorf("Gitea Actions request returned HTTP %d", response.StatusCode)
	}
}

func mapWorkflowRun(dto workflowRunDTO) actions.WorkflowRun {
	return actions.WorkflowRun{
		ID:           dto.ID,
		DisplayTitle: dto.DisplayTitle,
		Status:       dto.Status,
		Conclusion:   dto.Conclusion,
		HeadSHA:      dto.HeadSHA,
		HTMLURL:      dto.HTMLURL,
	}
}
