package graphqlapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"gh-gateway/internal/repository"
)

const maxRequestBytes = 1 << 20

type RepositoryService interface {
	Get(ctx context.Context, owner, name, authorization string) (repository.Repository, error)
}

type handler struct {
	service RepositoryService
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

func NewRouter(service RepositoryService) http.Handler {
	h := &handler{service: service}
	router := chi.NewRouter()
	router.Post("/api/graphql", h.repositoryInfo)
	return router
}

func (h *handler) repositoryInfo(w http.ResponseWriter, r *http.Request) {
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

	info, err := parseRepositoryInfo(request)
	if err != nil {
		writeJSON(w, http.StatusOK, graphQLResponse{
			Data:   nil,
			Errors: []graphQLError{{Type: "BAD_USER_INPUT", Message: err.Error()}},
		})
		return
	}

	result, err := h.service.Get(r.Context(), info.Owner, info.Name, r.Header.Get("Authorization"))
	if err != nil {
		h.writeServiceError(w, info, err)
		return
	}
	writeJSON(w, http.StatusOK, graphQLResponse{Data: repositoryData{Repository: &result}})
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
