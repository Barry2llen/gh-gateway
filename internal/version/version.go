package version

import "strings"

const runtimeImageRepository = "ghcr.io/barry2llen/gh-gateway"

// Version and Commit are set by release builds with -ldflags -X.
var (
	Version = "dev"
	Commit  = "unknown"
)

func DisplayVersion() string {
	if strings.TrimSpace(Version) == "" {
		return "dev"
	}
	return Version
}

func DisplayCommit() string {
	commit := strings.TrimSpace(Commit)
	if commit == "" {
		return "unknown"
	}
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func RuntimeImage() string {
	return runtimeImage(DisplayVersion())
}

func runtimeImage(buildVersion string) string {
	tag := buildVersion
	if tag == "" || tag == "dev" {
		tag = "latest"
	}
	return runtimeImageRepository + ":" + tag
}
