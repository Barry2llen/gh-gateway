package feedback

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	userdomain "gh-gateway/internal/user"
)

var (
	ErrNotFound     = errors.New("feedback not found")
	ErrUnauthorized = errors.New("authentication required")
	ErrForbidden    = errors.New("feedback access forbidden")
)

type Association string

const (
	AssociationOwner        Association = "OWNER"
	AssociationCollaborator Association = "COLLABORATOR"
	AssociationNone         Association = "NONE"
)

type Actor struct{ Login string }

type ConversationComment struct {
	ID                int64
	Author            Actor
	AuthorAssociation Association
	CreatedAt         time.Time
	Body              string
	HTMLURL           string
}

type ReviewState string

const (
	ReviewApproved         ReviewState = "APPROVED"
	ReviewPending          ReviewState = "PENDING"
	ReviewCommented        ReviewState = "COMMENTED"
	ReviewChangesRequested ReviewState = "CHANGES_REQUESTED"
	ReviewDismissed        ReviewState = "DISMISSED"
	ReviewRequestReview    ReviewState = "REQUEST_REVIEW"
)

type Review struct {
	ID                int64
	Author            Actor
	AuthorAssociation Association
	State             ReviewState
	SubmittedAt       time.Time
	CreatedAt         time.Time
	Body              string
	HTMLURL           string
}

type ReviewPage struct {
	Reviews []Review
	HasNext bool
}

type InlineComment struct {
	ID                int64
	ReviewID          int64
	Author            Actor
	AuthorAssociation Association
	CreatedAt         time.Time
	Body              string
	Path              string
	Line              uint64
	OriginalLine      uint64
	HTMLURL           string
}

type Page struct{ Number, PerPage int }

type Query struct {
	Owner, Repo   string
	Number        int64
	Authorization string
	Page          Page
}

type Provider interface {
	ListIssueComments(ctx context.Context, owner, repo string, number int64, authorization string) ([]ConversationComment, error)
	ListPullReviews(ctx context.Context, owner, repo string, number int64, page, limit int, authorization string) (ReviewPage, error)
	ListPullReviewComments(ctx context.Context, owner, repo string, number, reviewID int64, authorization string) ([]InlineComment, error)
	GetAuthenticatedUser(ctx context.Context, authorization string) (userdomain.AuthenticatedUser, error)
	IsCollaborator(ctx context.Context, owner, repo, login, authorization string) (bool, error)
}

type Service struct{ provider Provider }

func NewService(provider Provider) *Service { return &Service{provider: provider} }

func (s *Service) ListConversationComments(ctx context.Context, query Query) ([]ConversationComment, error) {
	comments, err := s.provider.ListIssueComments(ctx, query.Owner, query.Repo, query.Number, query.Authorization)
	if err != nil {
		return nil, err
	}
	if len(comments) == 0 {
		return []ConversationComment{}, nil
	}
	logins := make([]string, len(comments))
	for i := range comments {
		logins[i] = comments[i].Author.Login
	}
	associations, err := s.resolveAssociations(ctx, query, logins)
	if err != nil {
		return nil, err
	}
	for i := range comments {
		comments[i].AuthorAssociation = associations[i]
	}
	sort.SliceStable(comments, func(i, j int) bool {
		if comments[i].CreatedAt.Equal(comments[j].CreatedAt) {
			return comments[i].ID < comments[j].ID
		}
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})
	return paginate(comments, query.Page), nil
}

func (s *Service) ListReviews(ctx context.Context, query Query) ([]Review, error) {
	all, err := s.loadReviews(ctx, query)
	if err != nil {
		return nil, err
	}
	reviews := make([]Review, 0, len(all))
	for _, review := range all {
		if review.State != ReviewRequestReview {
			reviews = append(reviews, review)
		}
	}
	if len(reviews) == 0 {
		return []Review{}, nil
	}
	logins := make([]string, len(reviews))
	for i := range reviews {
		logins[i] = reviews[i].Author.Login
	}
	associations, err := s.resolveAssociations(ctx, query, logins)
	if err != nil {
		return nil, err
	}
	for i := range reviews {
		reviews[i].AuthorAssociation = associations[i]
		reviews[i].CreatedAt = reviews[i].SubmittedAt
	}
	sort.SliceStable(reviews, func(i, j int) bool {
		if reviews[i].SubmittedAt.Equal(reviews[j].SubmittedAt) {
			return reviews[i].ID < reviews[j].ID
		}
		return reviews[i].SubmittedAt.Before(reviews[j].SubmittedAt)
	})
	return paginate(reviews, query.Page), nil
}

func (s *Service) ListInlineComments(ctx context.Context, query Query) ([]InlineComment, error) {
	reviews, err := s.loadReviews(ctx, query)
	if err != nil {
		return nil, err
	}
	byID := map[int64]InlineComment{}
	for _, review := range reviews {
		if review.State == ReviewRequestReview {
			continue
		}
		comments, err := s.provider.ListPullReviewComments(ctx, query.Owner, query.Repo, query.Number, review.ID, query.Authorization)
		if err != nil {
			return nil, err
		}
		for _, comment := range comments {
			if _, exists := byID[comment.ID]; !exists {
				byID[comment.ID] = comment
			}
		}
	}
	if len(byID) == 0 {
		return []InlineComment{}, nil
	}
	comments := make([]InlineComment, 0, len(byID))
	for _, comment := range byID {
		comments = append(comments, comment)
	}
	logins := make([]string, len(comments))
	for i := range comments {
		logins[i] = comments[i].Author.Login
	}
	associations, err := s.resolveAssociations(ctx, query, logins)
	if err != nil {
		return nil, err
	}
	for i := range comments {
		comments[i].AuthorAssociation = associations[i]
	}
	sort.SliceStable(comments, func(i, j int) bool {
		if comments[i].CreatedAt.Equal(comments[j].CreatedAt) {
			return comments[i].ID < comments[j].ID
		}
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})
	return paginate(comments, query.Page), nil
}

func (s *Service) loadReviews(ctx context.Context, query Query) ([]Review, error) {
	reviews := make([]Review, 0)
	for page := 1; ; page++ {
		result, err := s.provider.ListPullReviews(ctx, query.Owner, query.Repo, query.Number, page, 100, query.Authorization)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, result.Reviews...)
		if !result.HasNext {
			break
		}
	}
	return reviews, nil
}

func (s *Service) resolveAssociations(ctx context.Context, query Query, logins []string) ([]Association, error) {
	current, err := s.provider.GetAuthenticatedUser(ctx, query.Authorization)
	if err != nil {
		switch {
		case errors.Is(err, userdomain.ErrUnauthorized):
			return nil, ErrUnauthorized
		case errors.Is(err, userdomain.ErrForbidden):
			return nil, ErrForbidden
		default:
			return nil, err
		}
	}
	result := make([]Association, len(logins))
	memo := map[string]Association{}
	for i, login := range logins {
		key := strings.ToLower(login)
		switch {
		case strings.EqualFold(login, query.Owner):
			result[i] = AssociationOwner
		case strings.EqualFold(login, current.Login):
			result[i] = AssociationNone
		case memo[key] != "":
			result[i] = memo[key]
		default:
			collaborator, err := s.provider.IsCollaborator(ctx, query.Owner, query.Repo, login, query.Authorization)
			if err != nil {
				return nil, err
			}
			association := AssociationNone
			if collaborator {
				association = AssociationCollaborator
			}
			memo[key], result[i] = association, association
		}
	}
	return result, nil
}

func paginate[T any](items []T, page Page) []T {
	if page.Number-1 > len(items)/page.PerPage {
		return []T{}
	}
	start := (page.Number - 1) * page.PerPage
	if start >= len(items) {
		return []T{}
	}
	end := start + page.PerPage
	if end > len(items) {
		end = len(items)
	}
	return append([]T(nil), items[start:end]...)
}
