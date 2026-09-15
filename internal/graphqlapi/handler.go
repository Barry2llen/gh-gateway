package graphqlapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/vektah/gqlparser/v2/ast"

	"gh-gateway/internal/pullrequest"
	"gh-gateway/internal/repository"
	"gh-gateway/internal/statuscheck"
)

const maxRequestBytes = 1 << 20

type RepositoryService interface {
	Get(ctx context.Context, owner, name, authorization string) (repository.Repository, error)
}

type PullRequestService interface {
	FindForBranch(ctx context.Context, query pullrequest.Query) (pullrequest.Result, error)
	FindByNumber(ctx context.Context, query pullrequest.NumberQuery) (pullrequest.PullRequest, error)
}

type StatusCheckService interface {
	Get(context.Context, statuscheck.Query) (statuscheck.Result, error)
}

type handler struct {
	repositories RepositoryService
	pullRequests PullRequestService
	statusChecks StatusCheckService
}

type graphQLResponse struct {
	Data   any            `json:"data"`
	Errors []graphQLError `json:"errors,omitempty"`
}

type graphQLError struct {
	Type    string   `json:"type"`
	Path    []string `json:"path,omitempty"`
	Message string   `json:"message"`
}

type repositoryData struct {
	Repository *repository.Repository `json:"repository"`
}

type pullRequestData struct {
	Repository *pullRequestRepository `json:"repository"`
}

type pullRequestRepository struct {
	PullRequests     pullRequestConnection `json:"pullRequests"`
	DefaultBranchRef defaultBranchRef      `json:"defaultBranchRef"`
}

type pullRequestConnection struct {
	Nodes []map[string]any `json:"nodes"`
}

type pullRequestByNumberData struct {
	Repository *pullRequestByNumberRepository `json:"repository"`
}

type pullRequestByNumberRepository struct {
	PullRequest map[string]any `json:"pullRequest"`
}

type graphQLPullRequest struct {
	Number              int64                   `json:"number"`
	URL                 string                  `json:"url"`
	State               pullrequest.State       `json:"state"`
	ID                  string                  `json:"id"`
	BaseRefName         string                  `json:"baseRefName"`
	HeadRefName         string                  `json:"headRefName"`
	IsCrossRepository   bool                    `json:"isCrossRepository"`
	HeadRepositoryOwner *graphQLRepositoryOwner `json:"headRepositoryOwner"`
}

type graphQLRepositoryOwner struct {
	ID    string `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

type defaultBranchRef struct {
	Name string `json:"name"`
}

func NewRouter(repositories RepositoryService, pullRequests PullRequestService, statusChecks ...StatusCheckService) http.Handler {
	router := chi.NewRouter()
	router.Post("/api/graphql", NewHandler(repositories, pullRequests, statusChecks...).ServeHTTP)
	return router
}

func NewHandler(repositories RepositoryService, pullRequests PullRequestService, statusChecks ...StatusCheckService) http.Handler {
	var checks StatusCheckService
	if len(statusChecks) > 0 {
		checks = statusChecks[0]
	}
	h := &handler{repositories: repositories, pullRequests: pullRequests, statusChecks: checks}
	return http.HandlerFunc(h.graphQL)
}

func (h *handler) graphQL(w http.ResponseWriter, r *http.Request) {
	var request graphQLRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, graphQLResponse{
			Data:   nil,
			Errors: []graphQLError{{Type: "BAD_REQUEST", Message: "Invalid JSON request body."}},
		})
		return
	}
	if err := ensureEOF(decoder); err != nil {
		writeJSON(w, http.StatusBadRequest, graphQLResponse{
			Data:   nil,
			Errors: []graphQLError{{Type: "BAD_REQUEST", Message: "Invalid JSON request body."}},
		})
		return
	}

	operation, err := parseOperation(request)
	if err != nil {
		writeJSON(w, http.StatusOK, graphQLResponse{
			Data:   nil,
			Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: err.Error()}},
		})
		return
	}
	switch operation.Name {
	case "RepositoryInfo":
		h.repositoryInfo(w, r, request, operation)
	case "PullRequestForBranch":
		h.pullRequestForBranch(w, r, request, operation)
	case "PullRequestByNumber":
		h.pullRequestByNumber(w, r, request, operation)
	case "PullRequest_fields", "PullRequest_fields2":
		h.pullRequestFeatureDetection(w, operation.Name)
	case "PullRequestStatusChecks":
		h.pullRequestStatusChecks(w, r, request, operation)
	default:
		writeJSON(w, http.StatusOK, graphQLResponse{
			Data:   nil,
			Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: "Unsupported GraphQL operation."}},
		})
	}
}

func (h *handler) repositoryInfo(w http.ResponseWriter, r *http.Request, request graphQLRequest, operation *ast.OperationDefinition) {
	info, err := parseRepositoryInfoOperation(request, operation)
	if err != nil {
		writeJSON(w, http.StatusOK, graphQLResponse{Data: nil, Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: err.Error()}}})
		return
	}

	result, err := h.repositories.Get(r.Context(), info.Owner, info.Name, r.Header.Get("Authorization"))
	if err != nil {
		h.writeServiceError(w, info, err)
		return
	}
	writeJSON(w, http.StatusOK, graphQLResponse{Data: repositoryData{Repository: &result}})
}

func (h *handler) pullRequestForBranch(w http.ResponseWriter, r *http.Request, request graphQLRequest, operation *ast.OperationDefinition) {
	info, err := parsePullRequestForBranchOperation(request, operation)
	if err != nil {
		writeJSON(w, http.StatusOK, graphQLResponse{Data: nil, Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: err.Error()}}})
		return
	}
	result, err := h.pullRequests.FindForBranch(r.Context(), pullrequest.Query{
		Owner:         info.Owner,
		Repo:          info.Repo,
		HeadRefName:   info.HeadRefName,
		Authorization: r.Header.Get("Authorization"),
	})
	if err != nil {
		h.writePullRequestServiceError(w, info, err)
		return
	}
	writeJSON(w, http.StatusOK, graphQLResponse{Data: pullRequestData{Repository: &pullRequestRepository{
		PullRequests:     pullRequestConnection{Nodes: presentPullRequests(result.Nodes, info.Owner, info.Repo, info.Fields)},
		DefaultBranchRef: defaultBranchRef{Name: result.DefaultBranch},
	}}})
}

func (h *handler) pullRequestByNumber(w http.ResponseWriter, r *http.Request, request graphQLRequest, operation *ast.OperationDefinition) {
	info, err := parsePullRequestByNumberOperation(request, operation)
	if err != nil {
		writeJSON(w, http.StatusOK, graphQLResponse{Data: nil, Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: err.Error()}}})
		return
	}
	result, err := h.pullRequests.FindByNumber(r.Context(), pullrequest.NumberQuery{
		Owner: info.Owner, Repo: info.Repo, Number: info.Number, Authorization: r.Header.Get("Authorization"),
	})
	if err != nil {
		errorType, message := "INTERNAL", "The pull request could not be retrieved."
		if errors.Is(err, pullrequest.ErrNotFound) {
			errorType, message = "NOT_FOUND", "Could not resolve to a PullRequest."
		} else if errors.Is(err, pullrequest.ErrForbidden) {
			errorType, message = "FORBIDDEN", "Resource not accessible with the supplied credentials."
		}
		writeJSON(w, http.StatusOK, graphQLResponse{
			Data:   pullRequestByNumberData{Repository: &pullRequestByNumberRepository{PullRequest: nil}},
			Errors: []graphQLError{{Type: errorType, Path: []string{"repository", "pullRequest"}, Message: message}},
		})
		return
	}
	writeJSON(w, http.StatusOK, graphQLResponse{Data: pullRequestByNumberData{Repository: &pullRequestByNumberRepository{
		PullRequest: presentPullRequest(result, info.Owner, info.Repo, info.Fields),
	}}})
}

func presentPullRequests(pullRequests []pullrequest.PullRequest, owner, repo string, selection pullRequestSelection) []map[string]any {
	presented := make([]map[string]any, 0, len(pullRequests))
	for _, pr := range pullRequests {
		presented = append(presented, presentPullRequest(pr, owner, repo, selection))
	}
	return presented
}

func presentPullRequest(pr pullrequest.PullRequest, owner, repo string, selection pullRequestSelection) map[string]any {
	result := make(map[string]any, len(selection.Fields))
	for field := range selection.Fields {
		switch field {
		case "number":
			result[field] = pr.Number
		case "url":
			result[field] = pr.URL
		case "state":
			result[field] = pr.State
		case "id":
			result[field] = encodePullRequestID(owner, repo, pr.Number)
		case "baseRefName":
			result[field] = pr.BaseRefName
		case "headRefName":
			result[field] = pr.HeadRefName
		case "headRefOid":
			result[field] = pr.HeadSHA
		case "mergedAt":
			result[field] = pr.MergedAt
		case "closedAt":
			result[field] = pr.ClosedAt
		case "isCrossRepository":
			result[field] = pr.IsCrossRepository
		case "mergeable":
			result[field] = pr.Mergeable
		case "mergeStateStatus":
			result[field] = pr.MergeStateStatus
		case "reviewDecision":
			result[field] = pr.ReviewDecision
		case "headRepository":
			if pr.HeadRepository == nil {
				result[field] = nil
				break
			}
			nested := map[string]any{}
			for selected := range selection.HeadRepository {
				switch selected {
				case "id":
					nested[selected] = pr.HeadRepository.ID
				case "name":
					nested[selected] = pr.HeadRepository.Name
				case "nameWithOwner":
					nested[selected] = pr.HeadRepository.NameWithOwner
				}
			}
			result[field] = nested
		case "headRepositoryOwner":
			if pr.HeadRepositoryOwner == nil {
				result[field] = nil
				break
			}
			nested := map[string]any{"id": pr.HeadRepositoryOwner.ID, "login": pr.HeadRepositoryOwner.Login, "name": pr.HeadRepositoryOwner.Name}
			result[field] = nested
		}
	}
	return result
}

func (h *handler) pullRequestFeatureDetection(w http.ResponseWriter, operation string) {
	fieldNames := func(names ...string) []map[string]string {
		result := make([]map[string]string, 0, len(names))
		for _, name := range names {
			result = append(result, map[string]string{"name": name})
		}
		return result
	}
	if operation == "PullRequest_fields2" {
		writeJSON(w, http.StatusOK, graphQLResponse{Data: map[string]any{"WorkflowRun": map[string]any{"fields": fieldNames("workflow")}}})
		return
	}
	writeJSON(w, http.StatusOK, graphQLResponse{Data: map[string]any{
		"PullRequest":                        map[string]any{"fields": fieldNames("id", "number", "headRefName", "commits")},
		"StatusCheckRollupContextConnection": map[string]any{"fields": fieldNames("nodes", "pageInfo")},
	}})
}

func (h *handler) pullRequestStatusChecks(w http.ResponseWriter, r *http.Request, request graphQLRequest, operation *ast.OperationDefinition) {
	info, err := parsePullRequestStatusChecksOperation(request, operation)
	if err != nil {
		writeJSON(w, http.StatusOK, graphQLResponse{Data: nil, Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: err.Error()}}})
		return
	}
	owner, repo, number, err := decodePullRequestID(info.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, graphQLResponse{Data: nil, Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: "Invalid PullRequest ID."}}})
		return
	}
	result, err := h.statusChecks.Get(r.Context(), statuscheck.Query{Owner: owner, Repo: repo, Number: number, Authorization: r.Header.Get("Authorization")})
	if err != nil {
		errorType, message := "INTERNAL", "The status checks could not be retrieved."
		if errors.Is(err, pullrequest.ErrNotFound) {
			errorType, message = "NOT_FOUND", "Could not resolve to a PullRequest."
		} else if errors.Is(err, pullrequest.ErrForbidden) {
			errorType, message = "FORBIDDEN", "Resource not accessible with the supplied credentials."
		}
		writeJSON(w, http.StatusOK, graphQLResponse{Data: map[string]any{"node": nil}, Errors: []graphQLError{{Type: errorType, Path: []string{"node"}, Message: message}}})
		return
	}
	start := 0
	if info.Cursor != "" {
		after, err := decodeStatusCursor(info.Cursor, info.ID, result.HeadSHA)
		if err != nil {
			writeJSON(w, http.StatusOK, graphQLResponse{Data: nil, Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: "Invalid status cursor."}}})
			return
		}
		found := false
		for i, item := range result.Contexts {
			if item.Name == after {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusOK, graphQLResponse{Data: nil, Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: "Invalid status cursor."}}})
			return
		}
	}
	end := start + 100
	if end > len(result.Contexts) {
		end = len(result.Contexts)
	}
	page := result.Contexts[start:end]
	nodes := make([]map[string]any, 0, len(page))
	for _, item := range page {
		nodes = append(nodes, map[string]any{"__typename": "StatusContext", "context": item.Name, "state": item.State, "targetUrl": item.TargetURL, "createdAt": item.CreatedAt, "description": item.Description, "isRequired": false})
	}
	hasNext := end < len(result.Contexts)
	var endCursor any = nil
	if hasNext && len(page) > 0 {
		endCursor = encodeStatusCursor(info.ID, result.HeadSHA, page[len(page)-1].Name)
	}
	contexts := map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": endCursor}}
	data := map[string]any{"node": map[string]any{"statusCheckRollup": map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": map[string]any{"contexts": contexts}}}}}}}
	writeJSON(w, http.StatusOK, graphQLResponse{Data: data})
}

func (h *handler) writeServiceError(w http.ResponseWriter, info repositoryInfo, err error) {
	errorType := "INTERNAL"
	message := "The repository could not be retrieved."
	if errors.Is(err, repository.ErrNotFound) {
		errorType = "NOT_FOUND"
		message = "Could not resolve to a Repository with the name '" + info.Owner + "/" + info.Name + "'."
	} else if errors.Is(err, repository.ErrForbidden) {
		errorType = "FORBIDDEN"
		message = "Resource not accessible with the supplied credentials."
	}
	writeJSON(w, http.StatusOK, graphQLResponse{
		Data:   repositoryData{Repository: nil},
		Errors: []graphQLError{{Type: errorType, Path: []string{"repository"}, Message: message}},
	})
}

func (h *handler) writePullRequestServiceError(w http.ResponseWriter, info pullRequestForBranch, err error) {
	errorType := "INTERNAL"
	message := "The pull requests could not be retrieved."
	if errors.Is(err, pullrequest.ErrNotFound) {
		errorType = "NOT_FOUND"
		message = "Could not resolve to a Repository with the name '" + info.Owner + "/" + info.Repo + "'."
	} else if errors.Is(err, pullrequest.ErrForbidden) {
		errorType = "FORBIDDEN"
		message = "Resource not accessible with the supplied credentials."
	}
	writeJSON(w, http.StatusOK, graphQLResponse{
		Data:   pullRequestData{Repository: nil},
		Errors: []graphQLError{{Type: errorType, Path: []string{"repository"}, Message: message}},
	})
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value graphQLResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
