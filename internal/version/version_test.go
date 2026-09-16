package version

import "testing"

func TestRuntimeImageTracksBuildVersion(t *testing.T) {
	tests := []struct {
		name, buildVersion, want string
	}{
		{name: "development", buildVersion: "dev", want: "ghcr.io/barry2llen/gh-gateway:latest"},
		{name: "empty development metadata", buildVersion: "", want: "ghcr.io/barry2llen/gh-gateway:latest"},
		{name: "release", buildVersion: "v0.1.0", want: "ghcr.io/barry2llen/gh-gateway:v0.1.0"},
		{name: "prerelease", buildVersion: "v0.1.0-rc.1", want: "ghcr.io/barry2llen/gh-gateway:v0.1.0-rc.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := runtimeImage(test.buildVersion); got != test.want {
				t.Fatalf("runtimeImage(%q) = %q, want %q", test.buildVersion, got, test.want)
			}
		})
	}
}

func TestDisplayMetadata(t *testing.T) {
	originalVersion, originalCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = originalVersion, originalCommit })

	Version, Commit = "v0.1.0", "abcdef1234567890"
	if got := DisplayVersion(); got != "v0.1.0" {
		t.Fatalf("DisplayVersion() = %q", got)
	}
	if got := DisplayCommit(); got != "abcdef1" {
		t.Fatalf("DisplayCommit() = %q", got)
	}
}
