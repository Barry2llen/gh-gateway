package localmode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostsManagerPreservesUnrelatedLinesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	original := "# user entry\r\n10.0.0.4 other.example.com # keep\r\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := fileHostsManager{path: path}
	if err := manager.Install("git.example.com", "instance-1"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Install("git.example.com", "instance-1"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), markerBegin("instance-1")) != 1 {
		t.Fatalf("duplicate marker:\n%s", data)
	}
	if !strings.HasPrefix(string(data), original) {
		t.Fatalf("unrelated content changed:\n%s", data)
	}
	if err := manager.Remove("instance-1"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Remove("instance-1"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != original {
		t.Fatalf("hosts after removal = %q", data)
	}
}

func TestHostsInspectionRejectsExistingEntry(t *testing.T) {
	inspection := inspectHosts("10.0.0.20 git.example.com\n", "git.example.com", "ours")
	if !inspection.Conflict || inspection.Own {
		t.Fatalf("inspection = %+v", inspection)
	}
	own := inspectHosts(markerBegin("ours")+"\n127.0.0.1 git.example.com\n"+markerEnd("ours")+"\n", "git.example.com", "ours")
	if !own.Own || own.Conflict {
		t.Fatalf("own inspection = %+v", own)
	}
}
