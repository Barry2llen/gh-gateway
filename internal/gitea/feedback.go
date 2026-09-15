package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"gh-gateway/internal/feedback"
)

type issueCommentDTO struct {
	ID        int64     `json:"id"`
	User      *ownerDTO `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
}

type pullReviewDTO struct {
	ID          int64     `json:"id"`
	User        *ownerDTO `json:"user"`
	State       string    `json:"state"`
	Dismissed   bool      `json:"dismissed"`
	SubmittedAt time.Time `json:"submitted_at"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
}

type pullReviewCommentDTO struct {
	ID           int64     `json:"id"`
	ReviewID     int64     `json:"pull_request_review_id"`
	User         *ownerDTO `json:"user"`
	CreatedAt    time.Time `json:"created_at"`
	Body         string    `json:"body"`
	Path         string    `json:"path"`
	Line         uint64    `json:"position"`
	OriginalLine uint64    `json:"original_position"`
	HTMLURL      string    `json:"html_url"`
}

func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int64, authorization string) ([]feedback.ConversationComment, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "issues", strconv.FormatInt(number, 10), "comments")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Gitea issue comments request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	c.applyAuthorization(request, authorization)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Gitea issue comments: %w", err)
	}
	defer response.Body.Close()
	if err := feedbackResponseError(response); err != nil {
		return nil, err
	}
	var dtos []issueCommentDTO
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&dtos); err != nil {
		return nil, fmt.Errorf("decode Gitea issue comments response: %w", err)
	}
	result := make([]feedback.ConversationComment, 0, len(dtos))
	for _, dto := range dtos {
		if dto.User == nil || dto.User.Login == "" {
			return nil, errors.New("Gitea issue comment response is missing user")
		}
		result = append(result, feedback.ConversationComment{ID: dto.ID, Author: feedback.Actor{Login: dto.User.Login}, CreatedAt: dto.CreatedAt, Body: dto.Body, HTMLURL: dto.HTMLURL})
	}
	return result, nil
}

func (c *Client) IsCollaborator(ctx context.Context, owner, repo, login, authorization string) (bool, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "collaborators", login)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return false, fmt.Errorf("create Gitea collaborator request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	c.applyAuthorization(request, authorization)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return false, fmt.Errorf("request Gitea collaborator: %w", err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNoContent:
		return true, nil
	case http.StatusNotFound, http.StatusForbidden, http.StatusUnprocessableEntity:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return false, nil
	case http.StatusUnauthorized:
		return false, feedback.ErrUnauthorized
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return false, fmt.Errorf("Gitea collaborator request returned HTTP %d", response.StatusCode)
	}
}

func (c *Client) ListPullReviews(ctx context.Context, owner, repo string, number int64, page, limit int, authorization string) (feedback.ReviewPage, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "pulls", strconv.FormatInt(number, 10), "reviews")
	query := endpoint.Query()
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(limit))
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return feedback.ReviewPage{}, fmt.Errorf("create Gitea reviews request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	c.applyAuthorization(request, authorization)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return feedback.ReviewPage{}, fmt.Errorf("request Gitea reviews: %w", err)
	}
	defer response.Body.Close()
	if err := feedbackResponseError(response); err != nil {
		return feedback.ReviewPage{}, err
	}
	var dtos []pullReviewDTO
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&dtos); err != nil {
		return feedback.ReviewPage{}, fmt.Errorf("decode Gitea reviews response: %w", err)
	}
	reviews := make([]feedback.Review, 0, len(dtos))
	for _, dto := range dtos {
		if dto.User == nil || dto.User.Login == "" {
			return feedback.ReviewPage{}, errors.New("Gitea review response is missing user")
		}
		state, err := mapReviewState(dto.State, dto.Dismissed)
		if err != nil {
			return feedback.ReviewPage{}, err
		}
		reviews = append(reviews, feedback.Review{ID: dto.ID, Author: feedback.Actor{Login: dto.User.Login}, State: state, SubmittedAt: dto.SubmittedAt, CreatedAt: dto.SubmittedAt, Body: dto.Body, HTMLURL: dto.HTMLURL})
	}
	return feedback.ReviewPage{Reviews: reviews, HasNext: hasNextPage(response.Header.Get("X-Total-Count"), page, limit, len(dtos))}, nil
}

func mapReviewState(state string, dismissed bool) (feedback.ReviewState, error) {
	if dismissed {
		return feedback.ReviewDismissed, nil
	}
	switch state {
	case "APPROVED":
		return feedback.ReviewApproved, nil
	case "PENDING":
		return feedback.ReviewPending, nil
	case "COMMENT":
		return feedback.ReviewCommented, nil
	case "REQUEST_CHANGES":
		return feedback.ReviewChangesRequested, nil
	case "REQUEST_REVIEW":
		return feedback.ReviewRequestReview, nil
	default:
		return "", fmt.Errorf("Gitea review response has unsupported state %q", state)
	}
}

func (c *Client) ListPullReviewComments(ctx context.Context, owner, repo string, number, reviewID int64, authorization string) ([]feedback.InlineComment, error) {
	endpoint := c.baseURL.JoinPath("api", "v1", "repos", owner, repo, "pulls", strconv.FormatInt(number, 10), "reviews", strconv.FormatInt(reviewID, 10), "comments")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Gitea review comments request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	c.applyAuthorization(request, authorization)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Gitea review comments: %w", err)
	}
	defer response.Body.Close()
	if err := feedbackResponseError(response); err != nil {
		return nil, err
	}
	var dtos []pullReviewCommentDTO
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&dtos); err != nil {
		return nil, fmt.Errorf("decode Gitea review comments response: %w", err)
	}
	result := make([]feedback.InlineComment, 0, len(dtos))
	for _, dto := range dtos {
		if dto.User == nil || dto.User.Login == "" {
			return nil, errors.New("Gitea review comment response is missing user")
		}
		result = append(result, feedback.InlineComment{ID: dto.ID, ReviewID: dto.ReviewID, Author: feedback.Actor{Login: dto.User.Login}, CreatedAt: dto.CreatedAt, Body: dto.Body, Path: dto.Path, Line: dto.Line, OriginalLine: dto.OriginalLine, HTMLURL: dto.HTMLURL})
	}
	return result, nil
}

func feedbackResponseError(response *http.Response) error {
	switch response.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return feedback.ErrNotFound
	case http.StatusUnauthorized:
		return feedback.ErrUnauthorized
	case http.StatusForbidden:
		return feedback.ErrForbidden
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return fmt.Errorf("Gitea feedback request returned HTTP %d", response.StatusCode)
	}
}
