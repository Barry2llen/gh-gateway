package statuscheck

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gh-gateway/internal/pullrequest"
)

type providerStub struct {
	pr    pullrequest.PullRequest
	pages map[int]Page
	calls []int
}

func (p *providerStub) GetPullRequest(context.Context, string, string, int64, string) (pullrequest.PullRequest, error) {
	return p.pr, nil
}
func (p *providerStub) ListCommitStatuses(_ context.Context, _, _, _ string, page, _ int, _ string) (Page, error) {
	p.calls = append(p.calls, page)
	return p.pages[page], nil
}

func TestServicePaginatesAndKeepsLatestStatusPerContext(t *testing.T) {
	t.Parallel()
	old := time.Unix(1, 0).UTC()
	newer := time.Unix(2, 0).UTC()
	provider := &providerStub{pr: pullrequest.PullRequest{Number: 12, HeadSHA: "head"}, pages: map[int]Page{1: {Statuses: []Context{{ID: 1, Name: "ci", State: StateFailure, UpdatedAt: old}}, HasNext: true}, 2: {Statuses: []Context{{ID: 2, Name: "ci", State: StateSuccess, UpdatedAt: newer}, {ID: 3, Name: "lint", State: StatePending, UpdatedAt: newer}}}}}
	got, err := NewService(provider).Get(context.Background(), Query{Owner: "foo", Repo: "bar", Number: 12, Authorization: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if got.HeadSHA != "head" || len(got.Contexts) != 2 || got.Contexts[0].Name != "ci" || got.Contexts[0].State != StateSuccess || got.Contexts[1].Name != "lint" || fmt.Sprint(provider.calls) != "[1 2]" {
		t.Fatalf("result/calls = %#v/%v", got, provider.calls)
	}
}
