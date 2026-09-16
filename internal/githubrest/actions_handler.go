package githubrest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"gh-gateway/internal/actions"
)

type ActionsService interface {
	ListRuns(context.Context, string, string, string, int, string) (actions.RunPage, error)
	ListJobs(context.Context, string, string, int64, int, string) (actions.JobPage, error)
	GetRun(context.Context, string, string, int64, string) (actions.WorkflowRun, error)
	GetWorkflow(context.Context, string, string, int64, string) (actions.Workflow, error)
	OpenJobLog(context.Context, string, string, int64, string) (actions.JobLog, error)
}

type actionsHandler struct{ service ActionsService }

type workflowRunResponse struct {
	ID           int64  `json:"id"`
	DisplayTitle string `json:"display_title,omitempty"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion,omitempty"`
	HeadSHA      string `json:"head_sha"`
	HTMLURL      string `json:"html_url,omitempty"`
	WorkflowID   int64  `json:"workflow_id,omitempty"`
}

type workflowRunsResponse struct {
	TotalCount   int64                 `json:"total_count"`
	WorkflowRuns []workflowRunResponse `json:"workflow_runs"`
}

type workflowJobResponse struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	HTMLURL    string `json:"html_url,omitempty"`
}

type workflowJobsResponse struct {
	TotalCount int64                 `json:"total_count"`
	Jobs       []workflowJobResponse `json:"jobs"`
}

type workflowResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func NewActionsHandler(service ActionsService) http.Handler {
	h := &actionsHandler{service: service}
	router := chi.NewRouter()
	router.Get("/runs", h.listRuns)
	router.Get("/runs/{run_id}/jobs", h.listJobs)
	router.Get("/runs/{run_id}", h.getRun)
	router.Get("/workflows/{workflow_id}", h.getWorkflow)
	router.Get("/jobs/{job_id}/logs", h.getJobLog)
	router.Post("/runs/{run_id}/rerun-failed-jobs", h.rerunFailedJobs)
	return router
}

func (h *actionsHandler) listRuns(w http.ResponseWriter, r *http.Request) {
	perPage, ok := parsePerPage(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListRuns(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), r.URL.Query().Get("head_sha"), perPage, r.Header.Get("Authorization"))
	if err != nil {
		writeActionsError(w, err)
		return
	}
	response := workflowRunsResponse{TotalCount: page.TotalCount, WorkflowRuns: make([]workflowRunResponse, 0, len(page.Runs))}
	for _, run := range page.Runs {
		response.WorkflowRuns = append(response.WorkflowRuns, presentRun(run))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *actionsHandler) listJobs(w http.ResponseWriter, r *http.Request) {
	runID, ok := parsePositiveID(w, chi.URLParam(r, "run_id"))
	if !ok {
		return
	}
	perPage, ok := parsePerPage(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListJobs(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), runID, perPage, r.Header.Get("Authorization"))
	if err != nil {
		writeActionsError(w, err)
		return
	}
	response := workflowJobsResponse{TotalCount: page.TotalCount, Jobs: make([]workflowJobResponse, 0, len(page.Jobs))}
	for _, job := range page.Jobs {
		response.Jobs = append(response.Jobs, workflowJobResponse{ID: job.ID, Name: job.Name, Status: job.Status, Conclusion: job.Conclusion, HTMLURL: job.HTMLURL})
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *actionsHandler) getRun(w http.ResponseWriter, r *http.Request) {
	runID, ok := parsePositiveID(w, chi.URLParam(r, "run_id"))
	if !ok {
		return
	}
	run, err := h.service.GetRun(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), runID, r.Header.Get("Authorization"))
	if err != nil {
		writeActionsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, presentRun(run))
}

func (h *actionsHandler) getWorkflow(w http.ResponseWriter, r *http.Request) {
	workflowID, ok := parsePositiveID(w, chi.URLParam(r, "workflow_id"))
	if !ok {
		return
	}
	workflow, err := h.service.GetWorkflow(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), workflowID, r.Header.Get("Authorization"))
	if err != nil {
		writeActionsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workflowResponse{ID: workflow.ID, Name: workflow.Name})
}

func (h *actionsHandler) getJobLog(w http.ResponseWriter, r *http.Request) {
	jobID, ok := parsePositiveID(w, chi.URLParam(r, "job_id"))
	if !ok {
		return
	}
	log, err := h.service.OpenJobLog(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), jobID, r.Header.Get("Authorization"))
	if err != nil {
		writeActionsError(w, err)
		return
	}
	defer log.Body.Close()
	contentType := log.ContentType
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	if log.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", log.ContentDisposition)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, log.Body)
}

func (h *actionsHandler) rerunFailedJobs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorResponse{Message: "Failed-only workflow rerun is not supported by this Gitea compatibility profile."})
}

func presentRun(run actions.WorkflowRun) workflowRunResponse {
	return workflowRunResponse{ID: run.ID, DisplayTitle: run.DisplayTitle, Status: run.Status, Conclusion: run.Conclusion, HeadSHA: run.HeadSHA, HTMLURL: run.HTMLURL, WorkflowID: run.WorkflowID}
}

func parsePerPage(w http.ResponseWriter, r *http.Request) (int, bool) {
	value := r.URL.Query().Get("per_page")
	if value == "" {
		return 30, true
	}
	perPage, err := strconv.Atoi(value)
	if err != nil || perPage < 1 || perPage > 100 {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Message: "Invalid per_page parameter."})
		return 0, false
	}
	return perPage, true
}

func parsePositiveID(w http.ResponseWriter, value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusNotFound, errorResponse{Message: "Not Found"})
		return 0, false
	}
	return id, true
}

func writeActionsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, actions.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Message: "Not Found"})
	case errors.Is(err, actions.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, errorResponse{Message: "Requires authentication."})
	case errors.Is(err, actions.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorResponse{Message: "Resource not accessible with the supplied credentials."})
	default:
		writeJSON(w, http.StatusBadGateway, errorResponse{Message: "The upstream service could not be reached."})
	}
}
