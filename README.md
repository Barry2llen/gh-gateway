# gh-gateway

Minimal GitHub GraphQL compatibility gateway for two commands:

```text
gh repo view --json nameWithOwner,parent
gh pr view --json number,url,state
```

The gateway exposes only `POST /api/graphql` and dispatches the `RepositoryInfo` and `PullRequestForBranch` operations. It translates them to Gitea's repository and paginated pull-request REST endpoints.

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

The E2E runner verifies that the Enterprise-style request reaches `/api/graphql`, authentication is forwarded to Gitea, non-fork `parent` is null, fork parent IDs are JSON strings, `gh pr view` returns the expected number/state/URL, and `/graphql` remains unavailable.

## Manual `gh` verification

`gh` requires HTTPS for a custom Enterprise-style host. Configure trusted DNS and a TLS-terminating reverse proxy so that:

```text
https://git.example.com/api/graphql -> http://127.0.0.1:8080/api/graphql
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
gh repo view --json nameWithOwner,parent
```

Expected non-fork output:

```json
{"nameWithOwner":"foo/bar","parent":null}
```

No `/graphql`, GitHub REST, explicit PR selector, commit-to-PR fallback, checks, Actions, review, create, or merge compatibility is provided in this slice. `PullRequestForBranch` accepts only the exact `gh` 2.95.0 query shape with `states: null`.
