package statuscheck

import (
	"context"
	"sort"
	"time"

	"gh-gateway/internal/pullrequest"
)

type State string

const (
	StatePending State = "PENDING"
	StateSuccess State = "SUCCESS"
	StateError   State = "ERROR"
	StateFailure State = "FAILURE"
)

type Context struct {
	ID          int64
	Name        string
	State       State
	TargetURL   string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
type Page struct {
	Statuses []Context
	HasNext  bool
}
type Query struct {
	Owner, Repo   string
	Number        int64
	Authorization string
}
type Result struct {
	PullRequest pullrequest.PullRequest
	HeadSHA     string
	Contexts    []Context
}
type Provider interface {
	GetPullRequest(context.Context, string, string, int64, string) (pullrequest.PullRequest, error)
	ListCommitStatuses(context.Context, string, string, string, int, int, string) (Page, error)
}
type Service struct{ provider Provider }

func NewService(provider Provider) *Service { return &Service{provider: provider} }
func (s *Service) Get(ctx context.Context, query Query) (Result, error) {
	pr, err := s.provider.GetPullRequest(ctx, query.Owner, query.Repo, query.Number, query.Authorization)
	if err != nil {
		return Result{}, err
	}
	latest := map[string]Context{}
	for page := 1; ; page++ {
		result, err := s.provider.ListCommitStatuses(ctx, query.Owner, query.Repo, pr.HeadSHA, page, 100, query.Authorization)
		if err != nil {
			return Result{}, err
		}
		for _, candidate := range result.Statuses {
			current, ok := latest[candidate.Name]
			if !ok || newer(candidate, current) {
				latest[candidate.Name] = candidate
			}
		}
		if !result.HasNext {
			break
		}
	}
	contexts := make([]Context, 0, len(latest))
	for _, item := range latest {
		contexts = append(contexts, item)
	}
	sort.Slice(contexts, func(i, j int) bool { return contexts[i].Name < contexts[j].Name })
	return Result{PullRequest: pr, HeadSHA: pr.HeadSHA, Contexts: contexts}, nil
}
func newer(a, b Context) bool {
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.After(b.UpdatedAt)
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}
