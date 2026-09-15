# gh-gateway

Minimal GitHub GraphQL compatibility gateway for one command:

```text
gh repo view --json nameWithOwner,parent
```

The gateway exposes only `POST /api/graphql` and translates `RepositoryInfo` to Gitea's `GET /api/v1/repos/{owner}/{repo}` endpoint.

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

No `/graphql`, REST, pull request, checks, Actions, review, create, or merge compatibility is provided in this slice.
