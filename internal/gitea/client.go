package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gh-gateway/internal/repository"
)

const maxResponseBytes = 1 << 20

type Client struct {
	baseURL         *url.URL
	configuredToken string
	httpClient      *http.Client
}

type repositoryDTO struct {
	FullName string     `json:"full_name"`
	Parent   *parentDTO `json:"parent"`
}

type parentDTO struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Owner ownerDTO `json:"owner"`
}

type ownerDTO struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

func NewClient(baseURL, configuredToken string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Gitea base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("Gitea base URL must be an absolute HTTP(S) URL")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:         parsed,
		configuredToken: strings.TrimSpace(configuredToken),
		httpClient:      httpClient,
	}, nil
}

func (c *Client) GetRepository(ctx context.Context, owner, name, authorization string) (repository.Repository, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, name)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return repository.Repository{}, fmt.Errorf("create Gitea repository request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if c.configuredToken != "" {
		request.Header.Set("Authorization", "token "+c.configuredToken)
	} else if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return repository.Repository{}, fmt.Errorf("request Gitea repository: %w", err)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		// Continue below.
	case http.StatusNotFound:
		return repository.Repository{}, repository.ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return repository.Repository{}, repository.ErrForbidden
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return repository.Repository{}, fmt.Errorf("Gitea repository request returned HTTP %d", response.StatusCode)
	}

	var dto repositoryDTO
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(&dto); err != nil {
		return repository.Repository{}, fmt.Errorf("decode Gitea repository response: %w", err)
	}
	if dto.FullName == "" {
		return repository.Repository{}, errors.New("Gitea repository response is missing full_name")
	}

	mapped := repository.Repository{NameWithOwner: dto.FullName}
	if dto.Parent != nil {
		mapped.Parent = &repository.Parent{
			ID:   strconv.FormatInt(dto.Parent.ID, 10),
			Name: dto.Parent.Name,
			Owner: repository.Owner{
				ID:    strconv.FormatInt(dto.Parent.Owner.ID, 10),
				Login: dto.Parent.Owner.Login,
			},
		}
	}
	return mapped, nil
}
