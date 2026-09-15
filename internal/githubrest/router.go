package githubrest

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	CommitPullRequests   http.Handler
	AuthenticatedUser    http.Handler
	ConversationComments http.Handler
	Reviews              http.Handler
	InlineComments       http.Handler
}

func NewRouter(handlers Handlers) http.Handler {
	router := chi.NewRouter()
	if handlers.CommitPullRequests != nil {
		router.Get("/repos/{owner}/{repo}/commits/{sha}/pulls", handlers.CommitPullRequests.ServeHTTP)
	}
	if handlers.AuthenticatedUser != nil {
		router.Get("/user", handlers.AuthenticatedUser.ServeHTTP)
	}
	if handlers.ConversationComments != nil {
		router.Get("/repos/{owner}/{repo}/issues/{number}/comments", handlers.ConversationComments.ServeHTTP)
	}
	if handlers.Reviews != nil {
		router.Get("/repos/{owner}/{repo}/pulls/{number}/reviews", handlers.Reviews.ServeHTTP)
	}
	if handlers.InlineComments != nil {
		router.Get("/repos/{owner}/{repo}/pulls/{number}/comments", handlers.InlineComments.ServeHTTP)
	}
	return router
}
