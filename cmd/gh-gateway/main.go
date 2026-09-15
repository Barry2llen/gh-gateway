package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"gh-gateway/internal/gateway"
	"gh-gateway/internal/gitea"
	"gh-gateway/internal/githubrest"
	"gh-gateway/internal/graphqlapi"
	"gh-gateway/internal/pullrequest"
	"gh-gateway/internal/repository"
	userdomain "gh-gateway/internal/user"
)

func main() {
	baseURL := os.Getenv("GITEA_BASE_URL")
	if baseURL == "" {
		log.Fatal("GITEA_BASE_URL is required")
	}
	address := os.Getenv("GATEWAY_ADDR")
	if address == "" {
		address = ":8080"
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	provider, err := gitea.NewClient(baseURL, os.Getenv("GITEA_TOKEN"), httpClient)
	if err != nil {
		log.Fatalf("configure Gitea client: %v", err)
	}
	repositoryService := repository.NewService(provider)
	pullRequestService := pullrequest.NewService(provider)
	userService := userdomain.NewService(provider)
	server := &http.Server{
		Addr: address,
		Handler: gateway.NewRouter(
			graphqlapi.NewHandler(repositoryService, pullRequestService),
			githubrest.NewHandler(pullRequestService),
			githubrest.NewUserHandler(userService),
		),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("gh-gateway listening on %s", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
