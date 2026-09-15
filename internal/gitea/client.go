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

	"gh-gateway/internal/pullrequest"
	"gh-gateway/internal/repository"
)

const maxResponseBytes = 1 << 20

type Client struct {
	baseURL         *url.URL
	configuredToken string
	httpClient      *http.Client
}

type repositoryDTO struct {
	ID            int64      `json:"id"`
	FullName      string     `json:"full_name"`
	DefaultBranch string     `json:"default_branch"`
	Parent        *parentDTO `json:"parent"`
}

type parentDTO struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Owner ownerDTO `json:"owner"`
}

type ownerDTO struct {
	ID       int64  `json:"id"`
	Login    string `json:"login"`
	FullName string `json:"full_name"`
}

type pullRequestDTO struct {
	ID      int64      `json:"id"`
	Number  int64      `json:"number"`
	HTMLURL string     `json:"html_url"`
	State   string     `json:"state"`
	Merged  bool       `json:"merged"`
	Base    *branchDTO `json:"base"`
	Head    *branchDTO `json:"head"`
}

type branchDTO struct {
	Label  string               `json:"label"`
	RepoID int64                `json:"repo_id"`
	Repo   *branchRepositoryDTO `json:"repo"`
}

type branchRepositoryDTO struct {
	Owner *ownerDTO `json:"owner"`
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
	c.applyAuthorization(request, authorization)

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

func (c *Client) GetRepositoryMetadata(ctx context.Context, owner, name, authorization string) (pullrequest.RepositoryMetadata, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, name)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return pullrequest.RepositoryMetadata{}, fmt.Errorf("create Gitea repository metadata request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	c.applyAuthorization(request, authorization)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return pullrequest.RepositoryMetadata{}, fmt.Errorf("request Gitea repository metadata: %w", err)
	}
	defer response.Body.Close()
	if err := pullRequestStatusError(response); err != nil {
		return pullrequest.RepositoryMetadata{}, err
	}
	var dto repositoryDTO
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&dto); err != nil {
		return pullrequest.RepositoryMetadata{}, fmt.Errorf("decode Gitea repository metadata response: %w", err)
	}
	if dto.DefaultBranch == "" {
		return pullrequest.RepositoryMetadata{}, errors.New("Gitea repository response is missing default_branch")
	}
	return pullrequest.RepositoryMetadata{DefaultBranch: dto.DefaultBranch}, nil
}

func (c *Client) ListPullRequests(ctx context.Context, owner, name string, page, limit int, authorization string) (pullrequest.Page, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, name, "pulls")
	query := endpoint.Query()
	query.Set("state", "all")
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(limit))
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return pullrequest.Page{}, fmt.Errorf("create Gitea pull request list request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	c.applyAuthorization(request, authorization)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return pullrequest.Page{}, fmt.Errorf("request Gitea pull request list: %w", err)
	}
	defer response.Body.Close()
	if err := pullRequestStatusError(response); err != nil {
		return pullrequest.Page{}, err
	}
	var dtos []pullRequestDTO
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&dtos); err != nil {
		return pullrequest.Page{}, fmt.Errorf("decode Gitea pull request list response: %w", err)
	}
	mapped := make([]pullrequest.PullRequest, 0, len(dtos))
	for _, dto := range dtos {
		pr, err := mapPullRequest(dto)
		if err != nil {
			return pullrequest.Page{}, err
		}
		mapped = append(mapped, pr)
	}
	return pullrequest.Page{
		PullRequests: mapped,
		HasNext:      hasNextPage(response.Header.Get("X-Total-Count"), page, limit, len(dtos)),
	}, nil
}

func (c *Client) applyAuthorization(request *http.Request, incoming string) {
	if c.configuredToken != "" {
		request.Header.Set("Authorization", "token "+c.configuredToken)
	} else if incoming != "" {
		request.Header.Set("Authorization", incoming)
	}
}

func pullRequestStatusError(response *http.Response) error {
	switch response.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return pullrequest.ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return pullrequest.ErrForbidden
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return fmt.Errorf("Gitea pull request request returned HTTP %d", response.StatusCode)
	}
}

func mapPullRequest(dto pullRequestDTO) (pullrequest.PullRequest, error) {
	if dto.Base == nil || dto.Head == nil {
		return pullrequest.PullRequest{}, errors.New("Gitea pull request response is missing base or head")
	}
	state := pullrequest.StateClosed
	switch {
	case dto.State == "open":
		state = pullrequest.StateOpen
	case dto.State == "closed" && dto.Merged:
		state = pullrequest.StateMerged
	case dto.State != "closed":
		return pullrequest.PullRequest{}, fmt.Errorf("Gitea pull request response has unsupported state %q", dto.State)
	}
	var headOwner *pullrequest.RepositoryOwner
	if dto.Head.Repo != nil && dto.Head.Repo.Owner != nil {
		headOwner = &pullrequest.RepositoryOwner{
			ID:    strconv.FormatInt(dto.Head.Repo.Owner.ID, 10),
			Login: dto.Head.Repo.Owner.Login,
			Name:  dto.Head.Repo.Owner.FullName,
		}
	}
	return pullrequest.PullRequest{
		Number:              dto.Number,
		URL:                 dto.HTMLURL,
		State:               state,
		ID:                  strconv.FormatInt(dto.ID, 10),
		BaseRefName:         dto.Base.Label,
		HeadRefName:         dto.Head.Label,
		IsCrossRepository:   dto.Base.RepoID != dto.Head.RepoID,
		HeadRepositoryOwner: headOwner,
	}, nil
}

func hasNextPage(totalHeader string, page, limit, count int) bool {
	if total, err := strconv.Atoi(totalHeader); err == nil && total >= 0 {
		return page*limit < total
	}
	return count == limit
}
