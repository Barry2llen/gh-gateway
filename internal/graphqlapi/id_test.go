package graphqlapi

import "testing"

func TestPullRequestIDRoundTrip(t *testing.T) {
	t.Parallel()

	id := encodePullRequestID("foo", "bar", 12)
	owner, repo, number, err := decodePullRequestID(id)
	if err != nil || owner != "foo" || repo != "bar" || number != 12 {
		t.Fatalf("decodePullRequestID(%q) = %q/%q/%d, %v", id, owner, repo, number, err)
	}
}

func TestPullRequestIDRejectsMalformedOrWrongType(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"", "not-base64", encodeOpaqueID("Repository:foo/bar/12"), encodeOpaqueID("PullRequest:foo/bar/0")} {
		if _, _, _, err := decodePullRequestID(id); err == nil {
			t.Fatalf("decodePullRequestID(%q) error = nil", id)
		}
	}
}
