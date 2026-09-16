package main

import (
	"bytes"
	"testing"

	"gh-gateway/internal/version"
)

func TestVersionCommand(t *testing.T) {
	originalVersion, originalCommit := version.Version, version.Commit
	t.Cleanup(func() { version.Version, version.Commit = originalVersion, originalCommit })

	tests := []struct {
		name, buildVersion, commit, want string
	}{
		{name: "development", buildVersion: "dev", commit: "unknown", want: "gh-gateway dev\ncommit: unknown\n"},
		{name: "release", buildVersion: "v0.1.0", commit: "abcdef1234567890", want: "gh-gateway v0.1.0\ncommit: abcdef1\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version.Version, version.Commit = test.buildVersion, test.commit
			command := newRootCommand()
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetArgs([]string{"version"})
			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got := output.String(); got != test.want {
				t.Fatalf("output = %q, want %q", got, test.want)
			}
		})
	}
}

func TestStartImageFlagDefaultsAndCanBeOverridden(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() { version.Version = originalVersion })
	version.Version = "v0.1.0"

	command := newRootCommand()
	start, _, err := command.Find([]string{"start"})
	if err != nil {
		t.Fatalf("Find(start) error = %v", err)
	}
	imageFlag := start.Flags().Lookup("image")
	if imageFlag == nil {
		t.Fatal("--image flag not found")
	}
	if got, want := imageFlag.DefValue, "ghcr.io/barry2llen/gh-gateway:v0.1.0"; got != want {
		t.Fatalf("default image = %q, want %q", got, want)
	}
	if err := start.Flags().Set("image", "registry.example.com/custom:tag"); err != nil {
		t.Fatalf("set --image: %v", err)
	}
	if got := imageFlag.Value.String(); got != "registry.example.com/custom:tag" {
		t.Fatalf("overridden image = %q", got)
	}
}
