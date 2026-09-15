package gateway

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(graphQL, githubREST http.Handler) http.Handler {
	router := chi.NewRouter()
	router.Post("/api/graphql", graphQL.ServeHTTP)
	router.Mount("/api/v3", githubREST)
	return router
}
