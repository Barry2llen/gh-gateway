package feedback

import (
	"context"
	"fmt"
	"testing"
	"time"

	userdomain "gh-gateway/internal/user"
)

type providerStub struct {
	comments          []ConversationComment
	login             string
	collaborators     map[string]bool
	collaboratorCalls map[string]int
	seenAuth          []string
	reviewPages       map[int]ReviewPage
	reviewCalls       []int
	reviewComments    map[int64][]InlineComment
	reviewCommentErr  error
}

func (p *providerStub) ListPullReviewComments(_ context.Context, _, _ string, _ int64, reviewID int64, _ string) ([]InlineComment, error) {
	if p.reviewCommentErr != nil {
		return nil, p.reviewCommentErr
	}
	return p.reviewComments[reviewID], nil
}

func (p *providerStub) ListPullReviews(_ context.Context, _, _ string, _ int64, page, _ int, _ string) (ReviewPage, error) {
	p.reviewCalls = append(p.reviewCalls, page)
	return p.reviewPages[page], nil
}

func (p *providerStub) ListIssueComments(_ context.Context, _, _ string, _ int64, authorization string) ([]ConversationComment, error) {
	p.seenAuth = append(p.seenAuth, authorization)
	return p.comments, nil
}

func (p *providerStub) GetAuthenticatedUser(context.Context, string) (userdomain.AuthenticatedUser, error) {
	return userdomain.AuthenticatedUser{Login: p.login}, nil
}

func (p *providerStub) IsCollaborator(_ context.Context, _, _, login, _ string) (bool, error) {
	p.collaboratorCalls[login]++
	return p.collaborators[login], nil
}

func TestConversationCommentsStableLocalPagination(t *testing.T) {
	t.Parallel()

	comments := make([]ConversationComment, 101)
	for i := range comments {
		comments[100-i] = ConversationComment{ID: int64(i + 1), Author: Actor{Login: "foo"}, CreatedAt: time.Unix(int64(i), 0).UTC()}
	}
	provider := &providerStub{comments: comments, login: "foo", collaborators: map[string]bool{}, collaboratorCalls: map[string]int{}}
	service := NewService(provider)
	first, err := service.ListConversationComments(context.Background(), Query{Owner: "foo", Repo: "bar", Authorization: "token incoming", Page: Page{Number: 1, PerPage: 100}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ListConversationComments(context.Background(), Query{Owner: "foo", Repo: "bar", Page: Page{Number: 2, PerPage: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 100 || first[0].ID != 1 || first[99].ID != 100 || len(second) != 1 || second[0].ID != 101 {
		t.Fatalf("pages = %d [%d..%d], %d [%d]", len(first), first[0].ID, first[99].ID, len(second), second[0].ID)
	}
	if fmt.Sprint(provider.seenAuth) != "[token incoming ]" {
		t.Fatalf("authorizations = %v", provider.seenAuth)
	}
}

func TestConversationCommentAssociationsAreConservativeAndMemoized(t *testing.T) {
	t.Parallel()

	provider := &providerStub{
		comments: []ConversationComment{
			{ID: 1, Author: Actor{Login: "owner"}},
			{ID: 2, Author: Actor{Login: "current"}},
			{ID: 3, Author: Actor{Login: "reviewer"}},
			{ID: 4, Author: Actor{Login: "reviewer"}},
			{ID: 5, Author: Actor{Login: "stranger"}},
		},
		login: "current", collaborators: map[string]bool{"reviewer": true}, collaboratorCalls: map[string]int{},
	}
	got, err := NewService(provider).ListConversationComments(context.Background(), Query{Owner: "owner", Repo: "repo", Page: Page{Number: 1, PerPage: 100}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Association{AssociationOwner, AssociationNone, AssociationCollaborator, AssociationCollaborator, AssociationNone}
	for i := range want {
		if got[i].AuthorAssociation != want[i] {
			t.Fatalf("comment %d association = %q, want %q", i, got[i].AuthorAssociation, want[i])
		}
	}
	if provider.collaboratorCalls["reviewer"] != 1 || provider.collaboratorCalls["stranger"] != 1 || len(provider.collaboratorCalls) != 2 {
		t.Fatalf("collaborator calls = %#v", provider.collaboratorCalls)
	}
}

func TestReviewsFilterRequestsPreservePendingAndPaginateAfterFiltering(t *testing.T) {
	t.Parallel()

	first := make([]Review, 100)
	for i := range first {
		first[i] = Review{ID: int64(i + 1), Author: Actor{Login: "owner"}, State: ReviewCommented, SubmittedAt: time.Unix(int64(i), 0).UTC()}
	}
	first[50].State = ReviewRequestReview
	provider := &providerStub{login: "current", collaborators: map[string]bool{}, collaboratorCalls: map[string]int{}, reviewPages: map[int]ReviewPage{
		1: {Reviews: first, HasNext: true}, 2: {Reviews: []Review{{ID: 101, Author: Actor{Login: "owner"}, State: ReviewPending, SubmittedAt: time.Unix(101, 0).UTC()}}},
	}}
	got, err := NewService(provider).ListReviews(context.Background(), Query{Owner: "owner", Repo: "repo", Number: 12, Page: Page{Number: 1, PerPage: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 100 || got[99].ID != 101 || got[99].State != ReviewPending || fmt.Sprint(provider.reviewCalls) != "[1 2]" {
		t.Fatalf("reviews/calls = %#v/%v", got, provider.reviewCalls)
	}
}

func TestInlineCommentsAggregateDeduplicateAndPreserveReviewIDs(t *testing.T) {
	t.Parallel()
	provider := &providerStub{login: "current", collaborators: map[string]bool{"reviewer": true}, collaboratorCalls: map[string]int{}, reviewPages: map[int]ReviewPage{1: {Reviews: []Review{{ID: 10, State: ReviewPending}, {ID: 11, State: ReviewCommented}}}}, reviewComments: map[int64][]InlineComment{
		10: {{ID: 2, ReviewID: 10, Author: Actor{Login: "reviewer"}, CreatedAt: time.Unix(2, 0).UTC(), Line: 7}},
		11: {{ID: 1, ReviewID: 11, Author: Actor{Login: "reviewer"}, CreatedAt: time.Unix(1, 0).UTC(), OriginalLine: 3}, {ID: 2, ReviewID: 10, Author: Actor{Login: "reviewer"}, CreatedAt: time.Unix(2, 0).UTC(), Line: 7}},
	}}
	got, err := NewService(provider).ListInlineComments(context.Background(), Query{Owner: "owner", Repo: "repo", Number: 12, Page: Page{Number: 1, PerPage: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[0].ReviewID != 11 || got[0].OriginalLine != 3 || got[1].ReviewID != 10 || got[1].AuthorAssociation != AssociationCollaborator {
		t.Fatalf("comments = %#v", got)
	}
}

func TestInlineCommentsFailAtomically(t *testing.T) {
	t.Parallel()
	provider := &providerStub{login: "current", collaborators: map[string]bool{}, collaboratorCalls: map[string]int{}, reviewPages: map[int]ReviewPage{1: {Reviews: []Review{{ID: 10}}}}, reviewCommentErr: fmt.Errorf("upstream failed")}
	got, err := NewService(provider).ListInlineComments(context.Background(), Query{Owner: "owner", Repo: "repo", Number: 12, Page: Page{Number: 1, PerPage: 100}})
	if err == nil || got != nil {
		t.Fatalf("comments/error = %#v/%v", got, err)
	}
}
