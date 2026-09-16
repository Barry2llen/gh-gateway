package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"gh-gateway/internal/actions"
	"gh-gateway/internal/feedback"
	"gh-gateway/internal/gateway"
	"gh-gateway/internal/gitea"
	"gh-gateway/internal/githubrest"
	"gh-gateway/internal/graphqlapi"
	"gh-gateway/internal/proxy"
	"gh-gateway/internal/pullrequest"
	"gh-gateway/internal/repository"
	"gh-gateway/internal/sshproxy"
	"gh-gateway/internal/statuscheck"
	"gh-gateway/internal/upstream"
	userdomain "gh-gateway/internal/user"
)

type Config struct {
	BaseURL, Address, Token, TransparentHost, UpstreamIP, TLSCertFile, TLSKeyFile string
	SSHProxy                                                                      bool
	SSHPort                                                                       int
}

func ConfigFromEnvironment() Config {
	address := os.Getenv("GATEWAY_ADDR")
	if address == "" {
		address = ":8080"
	}
	sshPort, _ := strconv.Atoi(os.Getenv("GATEWAY_SSH_PORT"))
	return Config{BaseURL: os.Getenv("GITEA_BASE_URL"), Address: address, Token: os.Getenv("GITEA_TOKEN"), TransparentHost: os.Getenv("GATEWAY_TRANSPARENT_HOST"), UpstreamIP: os.Getenv("GATEWAY_UPSTREAM_IP"), TLSCertFile: os.Getenv("GATEWAY_TLS_CERT"), TLSKeyFile: os.Getenv("GATEWAY_TLS_KEY"), SSHProxy: os.Getenv("GATEWAY_SSH_PROXY") == "1", SSHPort: sshPort}
}

func Run(ctx context.Context, cfg Config) error {
	if cfg.BaseURL == "" {
		return errors.New("GITEA_BASE_URL is required")
	}
	if cfg.Address == "" {
		cfg.Address = ":8080"
	}
	handler, err := NewHandler(cfg)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: cfg.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	errCh := make(chan error, 2)
	go func() {
		log.Printf("gh-gateway listening on %s", cfg.Address)
		var serveErr error
		if cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" {
			if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
				serveErr = errors.New("both GATEWAY_TLS_CERT and GATEWAY_TLS_KEY are required")
			} else {
				serveErr = httpServer.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
			}
		} else {
			serveErr = httpServer.ListenAndServe()
		}
		if !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()
	if cfg.SSHProxy {
		if cfg.UpstreamIP == "" || cfg.SSHPort < 1 || cfg.SSHPort > 65535 {
			_ = httpServer.Close()
			return errors.New("valid GATEWAY_UPSTREAM_IP and GATEWAY_SSH_PORT are required for SSH proxy")
		}
		ssh := sshproxy.Server{ListenAddress: net.JoinHostPort("", strconv.Itoa(cfg.SSHPort)), UpstreamAddress: net.JoinHostPort(cfg.UpstreamIP, strconv.Itoa(cfg.SSHPort))}
		go func() { errCh <- ssh.Serve(ctx) }()
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		_ = httpServer.Close()
		return err
	}
}

func NewHandler(cfg Config) (http.Handler, error) {
	var compatibilityTransport http.RoundTripper = http.DefaultTransport
	if cfg.TransparentHost != "" || cfg.UpstreamIP != "" {
		if cfg.TransparentHost == "" || net.ParseIP(cfg.UpstreamIP) == nil {
			return nil, errors.New("transparent mode requires GATEWAY_TRANSPARENT_HOST and a valid GATEWAY_UPSTREAM_IP")
		}
		compatibilityTransport = upstream.NewPinnedTransport(cfg.TransparentHost, cfg.UpstreamIP)
	}
	provider, err := gitea.NewClient(cfg.BaseURL, cfg.Token, &http.Client{Timeout: 10 * time.Second, Transport: compatibilityTransport})
	if err != nil {
		return nil, fmt.Errorf("configure Gitea client: %w", err)
	}
	repositoryService := repository.NewService(provider)
	pullRequestService := pullrequest.NewService(provider)
	userService := userdomain.NewService(provider)
	feedbackService := feedback.NewService(provider)
	statusCheckService := statuscheck.NewService(provider)
	actionsService := actions.NewService(provider)
	graphQL := graphqlapi.NewHandler(repositoryService, pullRequestService, statusCheckService)
	rest := githubrest.NewRouter(githubrest.Handlers{CommitPullRequests: githubrest.NewHandler(pullRequestService), AuthenticatedUser: githubrest.NewUserHandler(userService), ConversationComments: githubrest.NewConversationCommentsHandler(feedbackService), Reviews: githubrest.NewReviewsHandler(feedbackService), InlineComments: githubrest.NewInlineCommentsHandler(feedbackService), Actions: githubrest.NewActionsHandler(actionsService)})
	if cfg.TransparentHost == "" {
		return gateway.NewRouter(graphQL, rest), nil
	}
	fallback := proxy.New(cfg.TransparentHost, upstream.NewPinnedTransport(cfg.TransparentHost, cfg.UpstreamIP))
	return gateway.NewTransparentRouter(graphQL, rest, fallback, cfg.TransparentHost), nil
}
