package localmode

import (
	"errors"
	"testing"
)

func TestFileStateStoreRoundTripAndReplacement(t *testing.T) {
	store := fileStateStore{baseDir: t.TempDir()}
	first := State{Version: 1, Host: "git.example.com", CAThumbprint: "ABC"}
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	first.Active = true
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Active || got.Host != first.Host || got.CAThumbprint != "ABC" {
		t.Fatalf("state = %+v", got)
	}
	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("load after delete = %v", err)
	}
}
