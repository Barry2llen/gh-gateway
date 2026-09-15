package gitea

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gh-gateway/internal/pullrequest"
)

func TestClientGetsRepositoryMetadataAndMapsPullRequests(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token incoming" {
			t.Errorf("Authorization = %q, want token incoming", got)
		}
		switch r.URL.Path {
		case "/api/v1/repos/foo/bar":
			_, _ = w.Write([]byte(`{"id":10,"full_name":"foo/bar","default_branch":"main","parent":null}`))
		case "/api/v1/repos/foo/bar/pulls":
			wantQuery := url.Values{"state": {"all"}, "page": {"1"}, "limit": {"30"}}
			if r.URL.Query().Encode() != wantQuery.Encode() {
				t.Errorf("query = %s, want %s", r.URL.RawQuery, wantQuery.Encode())
			}
			w.Header().Set("X-Total-Count", "31")
			_, _ = w.Write([]byte(`[
              {
                "id":101,"number":3,"html_url":"https://git.example.test/foo/bar/pulls/3",
                "state":"open","merged":false,"merge_commit_sha":null,
                "base":{"label":"main","repo_id":10,"repo":{"owner":{"id":1,"login":"foo","full_name":"Foo"}}},
                "head":{"label":"feature","sha":"head-open","repo_id":10,"repo":{"owner":{"id":1,"login":"foo","full_name":"Foo"}}}
              },
              {
                "id":100,"number":2,"html_url":"https://git.example.test/foo/bar/pulls/2",
                "state":"closed","merged":false,"merge_commit_sha":null,
                "base":{"label":"main","repo_id":10,"repo":{"owner":{"id":1,"login":"foo"}}},
                "head":{"label":"feature","sha":"head-closed","repo_id":20,"repo":{"owner":{"id":2,"login":"forker","full_name":"Fork User"}}}
              },
              {
                "id":99,"number":1,"html_url":"https://git.example.test/foo/bar/pulls/1",
                "state":"closed","merged":true,"merge_commit_sha":"merge-sha",
                "base":{"label":"main","repo_id":10,"repo":{"owner":{"id":1,"login":"foo"}}},
                "head":{"label":"old-feature","sha":"head-merged","repo_id":10,"repo":{"owner":{"id":1,"login":"foo"}}}
              }
            ]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	metadata, err := client.GetRepositoryMetadata(context.Background(), "foo", "bar", "token incoming")
	if err != nil {
		t.Fatalf("GetRepositoryMetadata() error = %v", err)
	}
	if metadata.DefaultBranch != "main" {
		t.Fatalf("default branch = %q, want main", metadata.DefaultBranch)
	}
	page, err := client.ListPullRequests(context.Background(), "foo", "bar", 1, 30, "token incoming")
	if err != nil {
		t.Fatalf("ListPullRequests() error = %v", err)
	}
	if !page.HasNext || len(page.PullRequests) != 3 {
		t.Fatalf("page = %#v, want 3 PRs and next page", page)
	}
	open, closed, merged := page.PullRequests[0], page.PullRequests[1], page.PullRequests[2]
	if open.ID != "101" || open.State != pullrequest.StateOpen || open.URL != "https://git.example.test/foo/bar/pulls/3" || open.HeadSHA != "head-open" {
		t.Fatalf("open PR = %#v", open)
	}
	if closed.State != pullrequest.StateClosed || !closed.IsCrossRepository || closed.HeadRepositoryOwner == nil || closed.HeadRepositoryOwner.ID != "2" || closed.HeadRepositoryOwner.Login != "forker" || closed.HeadRepositoryOwner.Name != "Fork User" {
		t.Fatalf("closed cross-repository PR = %#v", closed)
	}
	if merged.State != pullrequest.StateMerged || merged.HeadRefName != "old-feature" || merged.BaseRefName != "main" || merged.HeadSHA != "head-merged" || merged.MergeCommitSHA != "merge-sha" {
		t.Fatalf("merged PR = %#v", merged)
	}
}

func TestClientPullRequestErrorsDoNotExposeResponseBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		wantTarget error
	}{
		{name: "not found", status: http.StatusNotFound, wantTarget: pullrequest.ErrNotFound},
		{name: "forbidden", status: http.StatusForbidden, wantTarget: pullrequest.ErrForbidden},
		{name: "server error", status: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"gitea secret detail"}`))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "", server.Client())
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			_, err = client.ListPullRequests(context.Background(), "foo", "bar", 1, 30, "")
			if err == nil {
				t.Fatal("ListPullRequests() error = nil")
			}
			if tt.wantTarget != nil && !errors.Is(err, tt.wantTarget) {
				t.Fatalf("error = %v, want %v", err, tt.wantTarget)
			}
			if strings.Contains(err.Error(), "gitea secret") {
				t.Fatalf("error leaked response body: %v", err)
			}
		})
	}
}
