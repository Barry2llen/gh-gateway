package pullrequest

import (
	"context"
	"errors"
)

const MaxCandidates = 30

var (
	ErrNotFound  = errors.New("repository not found")
	ErrForbidden = errors.New("repository access forbidden")
)

type State string

const (
	StateOpen   State = "OPEN"
	StateClosed State = "CLOSED"
	StateMerged State = "MERGED"
)

type RepositoryOwner struct {
	ID    string `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

type PullRequest struct {
	Number              int64            `json:"number"`
	URL                 string           `json:"url"`
	State               State            `json:"state"`
	ID                  string           `json:"id"`
	BaseRefName         string           `json:"baseRefName"`
	HeadRefName         string           `json:"headRefName"`
	IsCrossRepository   bool             `json:"isCrossRepository"`
	HeadRepositoryOwner *RepositoryOwner `json:"headRepositoryOwner"`
}

type RepositoryMetadata struct {
	DefaultBranch string
}

type Page struct {
	PullRequests []PullRequest
	HasNext      bool
}

type Query struct {
	Owner         string
	Repo          string
	HeadRefName   string
	Authorization string
}

type Result struct {
	Nodes         []PullRequest
	DefaultBranch string
}

type Provider interface {
	GetRepositoryMetadata(ctx context.Context, owner, repo, authorization string) (RepositoryMetadata, error)
	ListPullRequests(ctx context.Context, owner, repo string, page, limit int, authorization string) (Page, error)
}

type Service struct {
	provider Provider
}

func NewService(provider Provider) *Service {
	return &Service{provider: provider}
}

func (s *Service) FindForBranch(ctx context.Context, query Query) (Result, error) {
	metadata, err := s.provider.GetRepositoryMetadata(ctx, query.Owner, query.Repo, query.Authorization)
	if err != nil {
		return Result{}, err
	}

	candidates := make([]PullRequest, 0, MaxCandidates)
	for pageNumber := 1; len(candidates) < MaxCandidates; pageNumber++ {
		page, err := s.provider.ListPullRequests(ctx, query.Owner, query.Repo, pageNumber, MaxCandidates, query.Authorization)
		if err != nil {
			return Result{}, err
		}
		for _, candidate := range page.PullRequests {
			if candidate.HeadRefName != query.HeadRefName {
				continue
			}
			candidates = append(candidates, candidate)
			if len(candidates) == MaxCandidates {
				break
			}
		}
		if !page.HasNext {
			break
		}
	}

	if query.HeadRefName == metadata.DefaultBranch {
		eligible := candidates[:0]
		for _, candidate := range candidates {
			if candidate.State == StateOpen || candidate.IsCrossRepository {
				eligible = append(eligible, candidate)
			}
		}
		candidates = eligible
	}

	ordered := make([]PullRequest, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.State == StateOpen {
			ordered = append(ordered, candidate)
		}
	}
	for _, candidate := range candidates {
		if candidate.State != StateOpen {
			ordered = append(ordered, candidate)
		}
	}

	return Result{Nodes: ordered, DefaultBranch: metadata.DefaultBranch}, nil
}
