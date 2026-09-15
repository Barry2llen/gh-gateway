package gitea

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gh-gateway/internal/feedback"
)

func TestClientListsIssueCommentsWithoutUpstreamPagination(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/repos/foo/bar/issues/12/comments" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "token incoming" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`[{"id":7,"user":{"login":"reviewer"},"created_at":"2026-09-15T01:02:03Z","body":"hello","html_url":"https://git.example.test/foo/bar/issues/12#issuecomment-7"}]`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.ListIssueComments(context.Background(), "foo", "bar", 12, "token incoming")
	if err != nil || len(got) != 1 || got[0].ID != 7 || got[0].Author.Login != "reviewer" || got[0].Body != "hello" {
		t.Fatalf("comments/error = %#v/%v", got, err)
	}
}

func TestClientChecksCollaboratorMembership(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		status int
		want   bool
	}{{"collaborator", 204, true}, {"unknown", 404, false}, {"forbidden", 403, false}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/repos/foo/bar/collaborators/reviewer" {
					t.Errorf("path = %s", r.URL.Path)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"private detail"}`))
			}))
			defer server.Close()
			client, _ := NewClient(server.URL, "", server.Client())
			got, err := client.IsCollaborator(context.Background(), "foo", "bar", "reviewer", "")
			if err != nil || got != tt.want {
				t.Fatalf("IsCollaborator() = %v, %v", got, err)
			}
		})
	}
}

func TestClientListsAndMapsPullReviews(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/foo/bar/pulls/12/reviews" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("limit") != "100" {
			t.Errorf("URL = %s", r.URL.String())
		}
		w.Header().Set("X-Total-Count", "201")
		_, _ = w.Write([]byte(`[
		 {"id":1,"user":{"login":"a"},"state":"APPROVED","dismissed":true,"submitted_at":"2026-09-15T01:00:00Z","body":"a","html_url":"https://example/review/1"},
		 {"id":2,"user":{"login":"b"},"state":"PENDING","submitted_at":"2026-09-15T02:00:00Z"},
		 {"id":3,"user":{"login":"c"},"state":"COMMENT","submitted_at":"2026-09-15T03:00:00Z"},
		 {"id":4,"user":{"login":"d"},"state":"REQUEST_CHANGES","submitted_at":"2026-09-15T04:00:00Z"},
		 {"id":5,"user":{"login":"e"},"state":"REQUEST_REVIEW","submitted_at":"2026-09-15T05:00:00Z"}
		]`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "", server.Client())
	page, err := client.ListPullReviews(context.Background(), "foo", "bar", 12, 2, 100, "token")
	if err != nil || !page.HasNext || len(page.Reviews) != 5 {
		t.Fatalf("page/error = %#v/%v", page, err)
	}
	want := []feedback.ReviewState{feedback.ReviewDismissed, feedback.ReviewPending, feedback.ReviewCommented, feedback.ReviewChangesRequested, feedback.ReviewRequestReview}
	for i := range want {
		if page.Reviews[i].State != want[i] {
			t.Fatalf("review %d state = %q", i, page.Reviews[i].State)
		}
	}
}

func TestClientListsPullReviewComments(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/foo/bar/pulls/12/reviews/9/comments" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":4,"pull_request_review_id":9,"user":{"login":"reviewer"},"created_at":"2026-09-15T01:02:03Z","body":"inline","path":"x.go","position":7,"original_position":3,"html_url":"https://example/comment/4"}]`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "", server.Client())
	got, err := client.ListPullReviewComments(context.Background(), "foo", "bar", 12, 9, "token")
	if err != nil || len(got) != 1 || got[0].ReviewID != 9 || got[0].Line != 7 || got[0].OriginalLine != 3 || got[0].Path != "x.go" {
		t.Fatalf("comments/error = %#v/%v", got, err)
	}
}
