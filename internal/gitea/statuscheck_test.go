package gitea

import (
	"context"
	"gh-gateway/internal/statuscheck"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientListsCommitStatusesAndMapsConservatively(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/foo/bar/commits/head/statuses" || r.URL.Query().Get("page") != "1" || r.URL.Query().Get("limit") != "100" {
			t.Errorf("URL = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`[
{"id":1,"status":"pending","context":"a","target_url":"https://a","description":"pending","created_at":"2026-09-15T01:00:00Z","updated_at":"2026-09-15T01:00:00Z"},
{"id":2,"status":"success","context":"b","created_at":"2026-09-15T02:00:00Z","updated_at":"2026-09-15T02:00:00Z"},
{"id":3,"status":"warning","context":"c","created_at":"2026-09-15T03:00:00Z","updated_at":"2026-09-15T03:00:00Z"},
{"id":4,"status":"skipped","context":"d","created_at":"2026-09-15T04:00:00Z","updated_at":"2026-09-15T04:00:00Z"}]`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "", server.Client())
	page, err := client.ListCommitStatuses(context.Background(), "foo", "bar", "head", 1, 100, "token")
	if err != nil || len(page.Statuses) != 4 {
		t.Fatalf("page/error=%#v/%v", page, err)
	}
	want := []statuscheck.State{statuscheck.StatePending, statuscheck.StateSuccess, statuscheck.StateError, statuscheck.StateError}
	for i := range want {
		if page.Statuses[i].State != want[i] {
			t.Fatalf("status %d = %q", i, page.Statuses[i].State)
		}
	}
}
