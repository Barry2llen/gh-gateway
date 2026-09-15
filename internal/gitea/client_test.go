package gitea

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientGetsAndMapsRepository(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		response   string
		wantParent bool
	}{
		{
			name:       "non-fork",
			response:   `{"full_name":"foo/bar","parent":null}`,
			wantParent: false,
		},
		{
			name: "fork",
			response: `{
              "full_name":"forker/bar",
              "parent":{
                "id":42,
                "name":"bar",
                "owner":{"id":7,"login":"foo"},
                "html_url":"https://gitea.invalid/foo/bar",
                "private":true
              }
            }`,
			wantParent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				if r.URL.Path != "/api/v1/repos/foo/bar" {
					t.Errorf("path = %s, want /api/v1/repos/foo/bar", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "", server.Client())
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			got, err := client.GetRepository(context.Background(), "foo", "bar", "token incoming")
			if err != nil {
				t.Fatalf("GetRepository() error = %v", err)
			}

			if !tt.wantParent {
				if got.NameWithOwner != "foo/bar" || got.Parent != nil {
					t.Fatalf("repository = %#v, want foo/bar with nil parent", got)
				}
				return
			}

			if got.NameWithOwner != "forker/bar" || got.Parent == nil {
				t.Fatalf("repository = %#v, want mapped parent", got)
			}
			if got.Parent.ID != "42" || got.Parent.Owner.ID != "7" {
				t.Fatalf("parent IDs = %q/%q, want string IDs 42/7", got.Parent.ID, got.Parent.Owner.ID)
			}
			if got.Parent.Name != "bar" || got.Parent.Owner.Login != "foo" {
				t.Fatalf("parent = %#v, want bar owned by foo", got.Parent)
			}

			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(encoded) != `{"nameWithOwner":"forker/bar","parent":{"id":"42","name":"bar","owner":{"id":"7","login":"foo"}}}` {
				t.Fatalf("encoded repository = %s", encoded)
			}
		})
	}
}

func TestClientAuthorizationPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured string
		incoming   string
		want       string
	}{
		{name: "forwards incoming authorization", incoming: "token incoming", want: "token incoming"},
		{name: "configured token takes precedence", configured: "configured", incoming: "token incoming", want: "token configured"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != tt.want {
					t.Errorf("Authorization = %q, want %q", got, tt.want)
				}
				_, _ = w.Write([]byte(`{"full_name":"foo/bar","parent":null}`))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, tt.configured, server.Client())
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if _, err := client.GetRepository(context.Background(), "foo", "bar", tt.incoming); err != nil {
				t.Fatalf("GetRepository() error = %v", err)
			}
		})
	}
}
