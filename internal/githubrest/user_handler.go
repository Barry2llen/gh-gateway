package githubrest

import (
	"context"
	"errors"
	"net/http"

	userdomain "gh-gateway/internal/user"
)

type AuthenticatedUserService interface {
	Get(ctx context.Context, authorization string) (userdomain.AuthenticatedUser, error)
}

type userHandler struct {
	service AuthenticatedUserService
}

type authenticatedUserResponse struct {
	Login string `json:"login"`
}

func NewUserHandler(service AuthenticatedUserService) http.Handler {
	return &userHandler{service: service}
}

func (h *userHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Get(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, authenticatedUserResponse{Login: result.Login})
}

func (h *userHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, userdomain.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, errorResponse{Message: "Requires authentication."})
	case errors.Is(err, userdomain.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorResponse{Message: "Resource not accessible with the supplied credentials."})
	default:
		writeJSON(w, http.StatusBadGateway, errorResponse{Message: "The upstream service could not be reached."})
	}
}
