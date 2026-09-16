# gh-gateway

[English](README.md) | [简体中文](README.zh-CN.md)

> A focused GitHub API compatibility gateway that lets Codex and selected `gh` workflows work with Gitea.

`gh-gateway` translates the small, explicitly supported GitHub GraphQL and REST surface into Gitea API calls. On Windows 11 it can run transparently in front of an existing Gitea host, so repositories, browsers, and other Gitea traffic keep using the original hostname while compatible `gh` commands are handled by the gateway.

It is intentionally **not** a general GitHub API implementation. Unsupported or uncertain data fails conservatively instead of making a pull request or workflow appear successful.

## Contents

- [What it supports](#what-it-supports)
- [How it works](#how-it-works)
- [Quick start: Windows transparent mode](#quick-start-windows-transparent-mode)
- [Server mode](#server-mode)
- [Development and testing](#development-and-testing)
- [Compatibility boundaries](#compatibility-boundaries)

## What it supports

The compatibility layer is built for the Codex P0 command set, P1-A pull-request monitoring, and P1-B Actions monitoring.

| Area | Supported workflows |
| --- | --- |
| Repository and identity | `gh repo view`, `gh api user` |
| Pull requests | `gh pr view`, commit-to-PR lookup, conversation comments, reviews, and inline comments |
| Checks | `gh pr checks` and its GraphQL feature-detection/status-check queries |
| Actions | List workflow runs, list jobs, and fetch job logs |

Representative commands:

```text
gh repo view --json nameWithOwner,parent
gh api user
gh pr view --json number,url,state
gh api -H "Accept: application/vnd.github+json" repos/OWNER/REPO/commits/HEAD_SHA/pulls
gh pr view 1 --json number,url,state,mergedAt,closedAt,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeable,mergeStateStatus,reviewDecision
gh api 'repos/OWNER/REPO/issues/PR/comments?per_page=100&page=1'
gh api 'repos/OWNER/REPO/pulls/PR/reviews?per_page=100&page=1'
gh api 'repos/OWNER/REPO/pulls/PR/comments?per_page=100&page=1'
gh pr checks PR --json name,state,bucket,link,workflow,event,startedAt,completedAt
gh api repos/OWNER/REPO/actions/runs -X GET -f head_sha=HEAD_SHA -f per_page=100
gh api repos/OWNER/REPO/actions/runs/RUN_ID/jobs -X GET -f per_page=100
gh api repos/OWNER/REPO/actions/jobs/JOB_ID/logs
```

## How it works

```text
Codex / gh
    │  GitHub-compatible requests
    ▼
gh-gateway
    ├─ /api/graphql ───────► supported GraphQL operations ─┐
    ├─ /api/v3/... ────────► supported REST endpoints ─────┤─► Gitea API
    └─ every other path ───► transparent pass-through ─────┘   (local mode only)
```

The gateway forwards the incoming `Authorization` header by default. It does not persist tokens or pass them to Docker. In server mode, setting `GITEA_TOKEN` replaces the incoming authorization value with `Authorization: token <GITEA_TOKEN>`.

Transparent mode records the host's original IPv4 address before adding its marked hosts-file entry. The container connects to that address while preserving the original HTTP Host and TLS SNI, avoiding a routing loop without disabling upstream certificate verification.

## Quick start: Windows transparent mode

### Requirements

- Windows 11 and an elevated PowerShell terminal
- Docker Desktop using Linux containers
- An IPv4 Gitea hostname with a valid upstream HTTPS certificate
- Local port `443`, plus port `22` unless SSH pass-through is disabled
- A Gitea personal access token

### Install and start

Download the matching executable from [GitHub Releases](https://github.com/Barry2llen/gh-gateway/releases):

- `gh-gateway-windows-amd64.exe`
- `gh-gateway-windows-arm64.exe`

Then run:

```powershell
Rename-Item gh-gateway-windows-amd64.exe gh-gateway.exe
.\gh-gateway.exe start git.example.com

$env:GH_HOST = 'git.example.com'
$env:GH_ENTERPRISE_TOKEN = '<Gitea PAT>'
codex
```

A released CLI automatically selects its matching runtime image from `ghcr.io/barry2llen/gh-gateway`. Use `--image` only when you need an explicit override.

### Operate and remove

Run local-mode commands from an elevated PowerShell terminal:

```powershell
.\gh-gateway.exe status
.\gh-gateway.exe doctor
.\gh-gateway.exe stop
.\gh-gateway.exe uninstall
```

- `stop` removes the container and the hosts block owned by `gh-gateway`, but keeps the Current User local CA for reuse.
- `uninstall` also removes that CA by its recorded thumbprint and deletes `%LOCALAPPDATA%\gh-gateway`.
- Add `--no-ssh-proxy` to `start` if SSH pass-through is unnecessary. SSH remotes using the Gitea hostname will be unavailable until local mode is stopped.

Local mode currently supports one host, IPv4 upstream resolution, local HTTPS on port `443`, and optional same-port SSH pass-through. It does not provide automatic UAC elevation, Linux/macOS host orchestration, multi-host management, a background service, custom DNS, WSL-specific networking, Kubernetes deployment, or an installer.

## Server mode

Run the gateway as a regular HTTP service when you manage DNS and TLS separately:

```powershell
$env:GITEA_BASE_URL = 'https://gitea.example.com'
$env:GATEWAY_ADDR = ':8080'

# Optional: otherwise the incoming Authorization header is forwarded.
$env:GITEA_TOKEN = 'gitea-token'

go run ./cmd/gh-gateway serve
```

Running `gh-gateway` without a subcommand is equivalent to `gh-gateway serve`.

Because `gh` requires HTTPS for a custom Enterprise-style host, configure trusted DNS and a TLS-terminating reverse proxy:

```text
https://git.example.com/api/graphql -> http://127.0.0.1:8080/api/graphql
https://git.example.com/api/v3/*    -> http://127.0.0.1:8080/api/v3/*
```

Unlike local transparent mode, server mode only serves compatibility routes; other paths return `404` unless your surrounding proxy routes them elsewhere.

### Manual smoke test

Use a temporary repository with only the gateway remote:

```powershell
New-Item -ItemType Directory gh-gateway-e2e
Set-Location gh-gateway-e2e
git init
git remote add origin https://git.example.com/foo/bar.git

$env:GH_HOST = 'git.example.com'
$env:GH_ENTERPRISE_TOKEN = 'gitea-token'
$env:GH_PROMPT_DISABLED = '1'

gh api user
gh repo view --json nameWithOwner,parent
gh pr view --json number,url,state
$headSHA = git rev-parse HEAD
gh api -H "Accept: application/vnd.github+json" "repos/foo/bar/commits/$headSHA/pulls"
```

For a repository that is not a fork, `gh repo view` should include:

```json
{"nameWithOwner":"foo/bar","parent":null}
```

## Development and testing

This project uses Go 1.27. For local transparent-mode development, build both the runtime image and Windows CLI:

```powershell
docker build --target runtime -t gh-gateway:local .
go build -o gh-gateway.exe ./cmd/gh-gateway
.\gh-gateway.exe start git.example.com --image gh-gateway:local
```

Run the Go checks:

```powershell
go test ./...
go test -race ./...
go vet ./...
```

The containerized suite runs regular and race-enabled tests without requiring a host C compiler. Its end-to-end environment starts Gitea 1.25.5, act_runner 0.2.10, and trusted TLS through Caddy, then exercises `gh` 2.95.0 and 2.100.0.

```powershell
docker compose --profile test run --rm --build tests
docker compose up --build --abort-on-container-exit --exit-code-from e2e e2e
docker compose down -v --remove-orphans
```

An opt-in elevated Windows integration test is also available:

```powershell
$token = Read-Host 'Gitea PAT' -AsSecureString
.\scripts\windows-local-e2e.ps1 -HostName git.example.com -Image gh-gateway:local -Token $token -Repository owner/repo -PullRequest 1
```

The E2E runner uses the unmodified watcher pinned at `0265dd7b4547fd6a88c6458f359b3c20731421c4`. It verifies that `--once` completes when runs or jobs fail without fetching logs or rerunning anything. It also verifies that `--retry-failed-now` reaches the failed-only rerun endpoint, receives the explicit unsupported response, and causes no Gitea mutation.

More detailed API mappings and validation reports are available in [`docs/`](docs/).

## Compatibility boundaries

The implementation deliberately favors safe, narrow behavior:

- `mergeable=false` maps to `UNKNOWN`; `mergeStateStatus` is `DRAFT` or `UNKNOWN`; `reviewDecision` is `null`. Unknown data never makes a PR appear ready to merge.
- `author_association` is approximate: repository owners and direct collaborators are recognized, but organization `MEMBER` semantics are not claimed.
- Review line fields are approximated from Gitea positions.
- Gitea statuses are exposed only as `StatusContext`; warning and skipped states map conservatively to `ERROR`. `CheckRun`, workflow-run event/timing data, and required-check semantics are unsupported; `isRequired` is always `false`.
- Commit-to-PR matching is limited to the exact PR head SHA and merged commit SHA.
- Workflow and job mapping accepts only confirmed Gitea states. Unknown or inconsistent status/conclusion pairs return a compatibility error instead of being treated as successful.
- Workflow IDs are stable numeric surrogates derived from the repository and Gitea workflow filename. Workflows removed from the default branch cannot be resolved during rerun preflight.
- Job logs provide **approximate** compatibility: raw Gitea `200 text/plain` is returned directly rather than GitHub's `302` temporary-download flow.
- Run-level ZIP logs and failed-only reruns are **unsupported**. `POST .../rerun-failed-jobs` returns `501` and never invokes Gitea's full-run or Web UI rerun behavior.
- The gateway does not implement root `/graphql`, `/user`, or `/repos/...` routes; create, merge, comment/review mutations; full Actions reruns; artifacts; dispatch; cancel; delete; attempt APIs; or general GitHub compatibility. In local mode, non-compatibility paths pass through to Gitea unchanged.

For the exact endpoint and field mappings, see the [P0](docs/p0-api-mapping.md), [P1-A](docs/p1a-api-mapping.md), and [P1-B](docs/p1b-api-mapping.md) references.
