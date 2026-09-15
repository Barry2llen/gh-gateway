package githubrest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"gh-gateway/internal/pullrequest"
)

type CommitPullRequestService interface {
	FindForCommit(ctx context.Context, query pullrequest.CommitQuery) ([]pullrequest.PullRequest, error)
}

type handler struct {
	service CommitPullRequestService
}

type pullRequestResponse struct {
	Number  int64  `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

type errorResponse struct {
	Message string `json:"message"`
}

func NewHandler(service CommitPullRequestService) http.Handler {
	return &handler{service: service}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.FindForCommit(r.Context(), pullrequest.CommitQuery{
		Owner:         chi.URLParam(r, "owner"),
		Repo:          chi.URLParam(r, "repo"),
		SHA:           chi.URLParam(r, "sha"),
		Authorization: r.Header.Get("Authorization"),
	})
	if err != nil {
		h.writeError(w, err)
		return
	}

	presented := make([]pullRequestResponse, 0, len(result))
	for _, pr := range result {
		state := "closed"
		if pr.State == pullrequest.StateOpen {
			state = "open"
		}
		presented = append(presented, pullRequestResponse{Number: pr.Number, HTMLURL: pr.URL, State: state})
	}
	writeJSON(w, http.StatusOK, presented)
}

func (h *handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pullrequest.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Message: "Not Found"})
	case errors.Is(err, pullrequest.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorResponse{Message: "Resource not accessible with the supplied credentials."})
	default:
		writeJSON(w, http.StatusBadGateway, errorResponse{Message: "The upstream service could not be reached."})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
