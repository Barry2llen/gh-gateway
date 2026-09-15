package gateway

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(graphQL, commitPullRequests, authenticatedUser http.Handler) http.Handler {
	router := chi.NewRouter()
	router.Post("/api/graphql", graphQL.ServeHTTP)
	router.Get("/api/v3/repos/{owner}/{repo}/commits/{sha}/pulls", commitPullRequests.ServeHTTP)
	router.Get("/api/v3/user", authenticatedUser.ServeHTTP)
	return router
}
