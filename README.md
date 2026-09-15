# gh-gateway

Minimal GitHub compatibility gateway for the three Codex P0 commands and the first P1-A authenticated-user lookup:

```text
gh repo view --json nameWithOwner,parent
gh pr view --json number,url,state
gh api -H "Accept: application/vnd.github+json" repos/OWNER/REPO/commits/HEAD_SHA/pulls
gh api user
```

The gateway exposes `POST /api/graphql` for the `RepositoryInfo` and `PullRequestForBranch` operations, `GET /api/v3/repos/{owner}/{repo}/commits/{sha}/pulls` for the commit-to-PR fallback, and `GET /api/v3/user` for the authenticated login. It translates them to Gitea's repository, paginated pull-request, and authenticated-user REST endpoints.

## Run

```powershell
$env:GITEA_BASE_URL = 'https://gitea.example.com'
$env:GATEWAY_ADDR = ':8080'
# Optional. When omitted, the incoming Authorization header is forwarded.
$env:GITEA_TOKEN = 'gitea-token'
go run ./cmd/gh-gateway
```

`GITEA_TOKEN` takes precedence over the incoming authorization value and is sent as `Authorization: token <GITEA_TOKEN>`.

## Test

```powershell
go test ./...
go test -race ./...
```

The complete containerized suite does not depend on a host C compiler. It runs regular and race-enabled Go tests, starts Gitea 1.25.5, terminates trusted test TLS with Caddy, and executes `gh` 2.95.0 against a normal repository, a real fork, and a real open pull request:

```powershell
docker compose --profile test run --rm --build tests
docker compose up --build --abort-on-container-exit --exit-code-from e2e e2e
docker compose down -v --remove-orphans
```

The E2E runner verifies both Enterprise-style API prefixes, authentication forwarding, authenticated-user lookup, repository/fork mapping, branch PR lookup, commit HEAD lookup, the unassociated-commit empty array, and that the github.com-style `/graphql`, `/user`, and `/repos/...` paths remain unavailable.

## Manual `gh` verification

`gh` requires HTTPS for a custom Enterprise-style host. Configure trusted DNS and a TLS-terminating reverse proxy so that:

```text
https://git.example.com/api/graphql -> http://127.0.0.1:8080/api/graphql
https://git.example.com/api/v3/*    -> http://127.0.0.1:8080/api/v3/*
```

Then use a temporary repository with only the gateway remote:

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

Expected non-fork output:

```json
{"nameWithOwner":"foo/bar","parent":null}
```

No `/graphql`, root `/user`, root `/repos/...`, explicit PR selector, comments, reviews, checks, Actions, create, merge, or mutation compatibility is provided. `PullRequestForBranch` accepts only the exact `gh` 2.95.0 query shape with `states: null`. Commit-to-PR matching is intentionally limited to exact PR head SHA and merged commit SHA; it does not implement GitHub's full historical commit association semantics.
