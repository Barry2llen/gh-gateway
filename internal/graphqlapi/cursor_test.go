package graphqlapi

import "testing"

func TestStatusCursorRoundTripAndBinding(t *testing.T) {
	t.Parallel()
	id := encodePullRequestID("foo", "bar", 12)
	cursor := encodeStatusCursor(id, "head", "ci/test")
	after, err := decodeStatusCursor(cursor, id, "head")
	if err != nil || after != "ci/test" {
		t.Fatalf("decode = %q/%v", after, err)
	}
	if _, err := decodeStatusCursor(cursor, encodePullRequestID("foo", "bar", 13), "head"); err == nil {
		t.Fatal("wrong PR cursor accepted")
	}
	if _, err := decodeStatusCursor(cursor, id, "other"); err == nil {
		t.Fatal("wrong SHA cursor accepted")
	}
}

func TestStatusCursorRejectsMalformed(t *testing.T) {
	t.Parallel()
	if _, err := decodeStatusCursor("bad", "id", "sha"); err == nil {
		t.Fatal("malformed cursor accepted")
	}
}
