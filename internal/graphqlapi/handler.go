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
)

const maxRequestBytes = 1 << 20

type RepositoryService interface {
	Get(ctx context.Context, owner, name, authorization string) (repository.Repository, error)
}

type PullRequestService interface {
	FindForBranch(ctx context.Context, query pullrequest.Query) (pullrequest.Result, error)
}

type handler struct {
	repositories RepositoryService
	pullRequests PullRequestService
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
	Nodes []pullrequest.PullRequest `json:"nodes"`
}

type defaultBranchRef struct {
	Name string `json:"name"`
}

func NewRouter(repositories RepositoryService, pullRequests PullRequestService) http.Handler {
	h := &handler{repositories: repositories, pullRequests: pullRequests}
	router := chi.NewRouter()
	router.Post("/api/graphql", h.graphQL)
	return router
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
		PullRequests:     pullRequestConnection{Nodes: result.Nodes},
		DefaultBranchRef: defaultBranchRef{Name: result.DefaultBranch},
	}}})
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
