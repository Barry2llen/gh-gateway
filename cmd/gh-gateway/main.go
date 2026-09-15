package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"gh-gateway/internal/gitea"
	"gh-gateway/internal/graphqlapi"
	"gh-gateway/internal/repository"
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
	service := repository.NewService(provider)
	server := &http.Server{
		Addr:              address,
		Handler:           graphqlapi.NewRouter(service),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("gh-gateway listening on %s", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
