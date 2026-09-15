package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gh-gateway/internal/statuscheck"
	"io"
	"net/http"
	"strconv"
	"time"
)

type commitStatusDTO struct {
	ID          int64     `json:"id"`
	State       string    `json:"status"`
	TargetURL   string    `json:"target_url"`
	Description string    `json:"description"`
	Context     string    `json:"context"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (c *Client) ListCommitStatuses(ctx context.Context, owner, repo, sha string, page, limit int, authorization string) (statuscheck.Page, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "commits", sha, "statuses")
	query := endpoint.Query()
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(limit))
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return statuscheck.Page{}, fmt.Errorf("create Gitea statuses request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	c.applyAuthorization(request, authorization)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return statuscheck.Page{}, fmt.Errorf("request Gitea statuses: %w", err)
	}
	defer response.Body.Close()
	if err := pullRequestStatusError(response); err != nil {
		return statuscheck.Page{}, err
	}
	var dtos []commitStatusDTO
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&dtos); err != nil {
		return statuscheck.Page{}, fmt.Errorf("decode Gitea statuses response: %w", err)
	}
	items := make([]statuscheck.Context, 0, len(dtos))
	for _, dto := range dtos {
		if dto.Context == "" {
			return statuscheck.Page{}, errors.New("Gitea status response is missing context")
		}
		state, err := mapStatusState(dto.State)
		if err != nil {
			return statuscheck.Page{}, err
		}
		items = append(items, statuscheck.Context{ID: dto.ID, Name: dto.Context, State: state, TargetURL: dto.TargetURL, Description: dto.Description, CreatedAt: dto.CreatedAt, UpdatedAt: dto.UpdatedAt})
	}
	return statuscheck.Page{Statuses: items, HasNext: hasNextPage(response.Header.Get("X-Total-Count"), page, limit, len(dtos))}, nil
}
func mapStatusState(state string) (statuscheck.State, error) {
	switch state {
	case "pending":
		return statuscheck.StatePending, nil
	case "success":
		return statuscheck.StateSuccess, nil
	case "error":
		return statuscheck.StateError, nil
	case "failure":
		return statuscheck.StateFailure, nil
	case "warning", "skipped":
		return statuscheck.StateError, nil
	default:
		return "", fmt.Errorf("Gitea status response has unsupported state %q", state)
	}
}
