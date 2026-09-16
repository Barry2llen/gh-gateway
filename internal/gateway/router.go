package gateway

import (
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func NewRouter(graphQL, githubREST http.Handler) http.Handler {
	return NewTransparentRouter(graphQL, githubREST, http.NotFoundHandler(), "")
}

// NewTransparentRouter dispatches the GitHub compatibility paths and sends all
// other traffic to fallback. When expectedHost is non-empty, requests for any
// other host are rejected before reaching either handler.
func NewTransparentRouter(graphQL, githubREST, fallback http.Handler, expectedHost string) http.Handler {
	if fallback == nil {
		fallback = http.NotFoundHandler()
	}
	router := chi.NewRouter()
	router.Post("/api/graphql", graphQL.ServeHTTP)
	router.Mount("/api/v3", githubREST)
	router.NotFound(fallback.ServeHTTP)

	if expectedHost == "" {
		return router
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sameHost(r.Host, expectedHost) {
			http.Error(w, "request host does not match the configured Gitea host", http.StatusMisdirectedRequest)
			return
		}
		router.ServeHTTP(w, r)
	})
}

func sameHost(raw, expected string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "/@") {
		return false
	}
	host := raw
	if strings.Contains(raw, ":") {
		var err error
		var port string
		host, port, err = net.SplitHostPort(raw)
		if err != nil || port != "443" {
			return false
		}
	}
	return strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(expected, "."))
}
