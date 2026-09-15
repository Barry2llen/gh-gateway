package pullrequest

import (
	"context"
	"fmt"
	"testing"
)

type providerStub struct {
	metadata RepositoryMetadata
	pages    map[int]Page
	pull     PullRequest
	pullErr  error
	seen     []int
	seenAuth []string
}

func (p *providerStub) GetRepositoryMetadata(context.Context, string, string, string) (RepositoryMetadata, error) {
	return p.metadata, nil
}

func (p *providerStub) ListPullRequests(_ context.Context, _, _ string, page, _ int, authorization string) (Page, error) {
	p.seen = append(p.seen, page)
	p.seenAuth = append(p.seenAuth, authorization)
	result, ok := p.pages[page]
	if !ok {
		return Page{}, fmt.Errorf("unexpected page %d", page)
	}
	return result, nil
}

func (p *providerStub) GetPullRequest(_ context.Context, owner, repo string, number int64, authorization string) (PullRequest, error) {
	p.seenAuth = append(p.seenAuth, authorization)
	if owner != "foo" || repo != "bar" || number != 12 {
		return PullRequest{}, fmt.Errorf("unexpected lookup %s/%s#%d", owner, repo, number)
	}
	return p.pull, p.pullErr
}

func TestServiceFindByNumber(t *testing.T) {
	t.Parallel()

	provider := &providerStub{pull: PullRequest{Number: 12, HeadRefName: "feature"}}
	got, err := NewService(provider).FindByNumber(context.Background(), NumberQuery{Owner: "foo", Repo: "bar", Number: 12, Authorization: "token incoming"})
	if err != nil {
		t.Fatalf("FindByNumber() error = %v", err)
	}
	if got.Number != 12 || fmt.Sprint(provider.seenAuth) != "[token incoming]" {
		t.Fatalf("result/auth = %#v/%v", got, provider.seenAuth)
	}
}

func TestServiceFindForCommitMatchesHeadAndMergedCommit(t *testing.T) {
	t.Parallel()

	provider := &providerStub{pages: map[int]Page{1: {
		PullRequests: []PullRequest{
			{Number: 3, State: StateClosed, HeadSHA: "other"},
			{Number: 2, State: StateMerged, HeadSHA: "old", MergeCommitSHA: "wanted"},
			{Number: 1, State: StateOpen, HeadSHA: "wanted"},
		},
	}}}
	result, err := NewService(provider).FindForCommit(context.Background(), CommitQuery{
		Owner: "foo", Repo: "bar", SHA: "wanted", Authorization: "token incoming",
	})
	if err != nil {
		t.Fatalf("FindForCommit() error = %v", err)
	}
	if len(result) != 2 || result[0].Number != 2 || result[1].Number != 1 {
		t.Fatalf("result = %#v, want merged #2 and open #1 in provider order", result)
	}
	if fmt.Sprint(provider.seenAuth) != "[token incoming]" {
		t.Fatalf("authorizations = %v", provider.seenAuth)
	}
}

func TestServiceFindForCommitPaginatesAndDeduplicates(t *testing.T) {
	t.Parallel()

	provider := &providerStub{pages: map[int]Page{
		1: {PullRequests: []PullRequest{{Number: 2, State: StateOpen, HeadSHA: "other"}}, HasNext: true},
		2: {PullRequests: []PullRequest{{Number: 1, State: StateMerged, HeadSHA: "wanted", MergeCommitSHA: "wanted"}}},
	}}
	result, err := NewService(provider).FindForCommit(context.Background(), CommitQuery{Owner: "foo", Repo: "bar", SHA: "wanted"})
	if err != nil {
		t.Fatalf("FindForCommit() error = %v", err)
	}
	if len(result) != 1 || result[0].Number != 1 {
		t.Fatalf("result = %#v, want one copy of #1", result)
	}
	if fmt.Sprint(provider.seen) != "[1 2]" {
		t.Fatalf("pages = %v, want [1 2]", provider.seen)
	}
}

func TestServiceFindForCommitReturnsEmpty(t *testing.T) {
	t.Parallel()

	provider := &providerStub{pages: map[int]Page{1: {PullRequests: []PullRequest{{Number: 1, State: StateOpen, HeadSHA: "other"}}}}}
	result, err := NewService(provider).FindForCommit(context.Background(), CommitQuery{Owner: "foo", Repo: "bar", SHA: "missing"})
	if err != nil {
		t.Fatalf("FindForCommit() error = %v", err)
	}
	if result == nil || len(result) != 0 {
		t.Fatalf("result = %#v, want non-nil empty slice", result)
	}
}

func TestServiceFindForBranchFiltersAndPrioritizesOpen(t *testing.T) {
	t.Parallel()

	provider := &providerStub{
		metadata: RepositoryMetadata{DefaultBranch: "main"},
		pages: map[int]Page{1: {
			PullRequests: []PullRequest{
				{Number: 3, State: StateClosed, HeadRefName: "feature"},
				{Number: 2, State: StateOpen, HeadRefName: "other"},
				{Number: 1, State: StateOpen, HeadRefName: "feature"},
			},
		}},
	}
	result, err := NewService(provider).FindForBranch(context.Background(), Query{Owner: "foo", Repo: "bar", HeadRefName: "feature"})
	if err != nil {
		t.Fatalf("FindForBranch() error = %v", err)
	}
	if len(result.Nodes) != 2 || result.Nodes[0].Number != 1 || result.Nodes[1].Number != 3 {
		t.Fatalf("nodes = %#v, want open #1 before closed #3", result.Nodes)
	}
}

func TestServiceFindForBranchAppliesDefaultBranchRule(t *testing.T) {
	t.Parallel()

	provider := &providerStub{
		metadata: RepositoryMetadata{DefaultBranch: "main"},
		pages: map[int]Page{1: {
			PullRequests: []PullRequest{
				{Number: 2, State: StateMerged, HeadRefName: "main"},
				{Number: 1, State: StateOpen, HeadRefName: "main"},
				{Number: 3, State: StateClosed, HeadRefName: "main", IsCrossRepository: true},
			},
		}},
	}
	result, err := NewService(provider).FindForBranch(context.Background(), Query{Owner: "foo", Repo: "bar", HeadRefName: "main"})
	if err != nil {
		t.Fatalf("FindForBranch() error = %v", err)
	}
	if len(result.Nodes) != 2 || result.Nodes[0].Number != 1 || result.Nodes[1].Number != 3 {
		t.Fatalf("nodes = %#v, want open #1 and cross-repository #3", result.Nodes)
	}
}

func TestServiceFindForBranchPaginatesUntilMatch(t *testing.T) {
	t.Parallel()

	provider := &providerStub{
		metadata: RepositoryMetadata{DefaultBranch: "main"},
		pages: map[int]Page{
			1: {PullRequests: []PullRequest{{Number: 2, State: StateOpen, HeadRefName: "other"}}, HasNext: true},
			2: {PullRequests: []PullRequest{{Number: 1, State: StateOpen, HeadRefName: "feature"}}},
		},
	}
	result, err := NewService(provider).FindForBranch(context.Background(), Query{Owner: "foo", Repo: "bar", HeadRefName: "feature"})
	if err != nil {
		t.Fatalf("FindForBranch() error = %v", err)
	}
	if len(result.Nodes) != 1 || result.Nodes[0].Number != 1 {
		t.Fatalf("nodes = %#v, want #1 from page 2", result.Nodes)
	}
	if fmt.Sprint(provider.seen) != "[1 2]" {
		t.Fatalf("pages = %v, want [1 2]", provider.seen)
	}
}

func TestServiceFindForBranchStopsAtThirtyMatches(t *testing.T) {
	t.Parallel()

	firstPage := make([]PullRequest, 30)
	for i := range firstPage {
		firstPage[i] = PullRequest{Number: int64(30 - i), State: StateClosed, HeadRefName: "feature"}
	}
	provider := &providerStub{
		metadata: RepositoryMetadata{DefaultBranch: "main"},
		pages: map[int]Page{
			1: {PullRequests: firstPage, HasNext: true},
			2: {PullRequests: []PullRequest{{Number: 31, State: StateOpen, HeadRefName: "feature"}}},
		},
	}
	result, err := NewService(provider).FindForBranch(context.Background(), Query{Owner: "foo", Repo: "bar", HeadRefName: "feature"})
	if err != nil {
		t.Fatalf("FindForBranch() error = %v", err)
	}
	if len(result.Nodes) != 30 || fmt.Sprint(provider.seen) != "[1]" {
		t.Fatalf("got %d nodes from pages %v, want 30 nodes from page 1", len(result.Nodes), provider.seen)
	}
}

func TestServiceFindForBranchReturnsEmptyNodes(t *testing.T) {
	t.Parallel()

	provider := &providerStub{
		metadata: RepositoryMetadata{DefaultBranch: "main"},
		pages:    map[int]Page{1: {}},
	}
	result, err := NewService(provider).FindForBranch(context.Background(), Query{Owner: "foo", Repo: "bar", HeadRefName: "missing"})
	if err != nil {
		t.Fatalf("FindForBranch() error = %v", err)
	}
	if len(result.Nodes) != 0 || result.DefaultBranch != "main" {
		t.Fatalf("result = %#v, want empty nodes and main", result)
	}
}
