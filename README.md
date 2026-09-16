# gh-gateway

Minimal GitHub compatibility gateway for the Codex P0 commands, the P1-A PR monitoring profile, and the P1-B Actions monitoring profile:

```text
gh repo view --json nameWithOwner,parent
gh pr view --json number,url,state
gh api -H "Accept: application/vnd.github+json" repos/OWNER/REPO/commits/HEAD_SHA/pulls
gh api user
gh pr view 1 --json number,url,state,mergedAt,closedAt,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeable,mergeStateStatus,reviewDecision
gh api 'repos/OWNER/REPO/issues/PR/comments?per_page=100&page=1'
gh api 'repos/OWNER/REPO/pulls/PR/reviews?per_page=100&page=1'
gh api 'repos/OWNER/REPO/pulls/PR/comments?per_page=100&page=1'
gh pr checks PR --json name,state,bucket,link,workflow,event,startedAt,completedAt
gh api repos/OWNER/REPO/actions/runs -X GET -f head_sha=HEAD_SHA -f per_page=100
gh api repos/OWNER/REPO/actions/runs/RUN_ID/jobs -X GET -f per_page=100
gh api repos/OWNER/REPO/actions/jobs/JOB_ID/logs
```

The gateway exposes only the Enterprise-style `/api/graphql` and `/api/v3/...` paths required by these commands. GraphQL dispatch supports `RepositoryInfo`, expanded `PullRequestForBranch`, `PullRequestByNumber`, the two gh checks feature-detection queries, and `PullRequestStatusChecks`.

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
go vet ./...
```

The complete containerized suite does not depend on a host C compiler. It runs regular and race-enabled Go tests, starts Gitea 1.25.5 plus a pinned act_runner 0.2.10, terminates trusted test TLS with Caddy, and executes both `gh` 2.95.0 and 2.100.0. P1-B is covered twice: a deterministic Actions-provider protocol fixture and a real failing Gitea matrix workflow.

```powershell
docker compose --profile test run --rm --build tests
docker compose up --build --abort-on-container-exit --exit-code-from e2e e2e
docker compose down -v --remove-orphans
```

The E2E runner uses the unmodified watcher pinned at `0265dd7b4547fd6a88c6458f359b3c20731421c4`. It verifies that `--once` completes with failed runs/jobs without fetching logs or rerunning anything. It separately verifies that `--retry-failed-now` reaches the failed-only rerun route and receives the explicit unsupported response without any Gitea mutation.

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

Compatibility remains deliberately conservative:

- `mergeable=false` maps to `UNKNOWN`; `mergeStateStatus` is `DRAFT` or `UNKNOWN`; `reviewDecision` is null. Unknown data never makes a PR look ready to merge.
- `author_association` is approximate: repository owner and direct collaborator are recognized; organization `MEMBER` semantics are not claimed.
- Review line fields map from Gitea positions and are approximate.
- Gitea statuses are exposed only as `StatusContext`; warning/skipped map conservatively to `ERROR`. CheckRun, WorkflowRun event/timing, and required-check semantics are unsupported; `isRequired` is false.
- Commit-to-PR matching remains limited to exact PR head SHA and merged commit SHA.
- Workflow and job status mapping accepts only confirmed Gitea states. Unknown or inconsistent status/conclusion pairs fail with a compatibility error instead of being treated as successful.
- Workflow IDs are stable numeric surrogate IDs derived from the repository and Gitea workflow filename. This is an emulated identity boundary; workflows deleted from the default branch cannot be resolved for rerun preflight.
- Job-level logs are **Approximate** compatibility: raw Gitea `200 text/plain` is returned directly and GitHub's `302` temporary-download transport is not reproduced.
- Run-level ZIP logs and failed-only rerun are **Unsupported**. `POST .../rerun-failed-jobs` returns `501` and never calls Gitea full-run or Web UI rerun behavior.
- No root `/graphql`, `/user`, or `/repos/...`, create, merge, comment/review mutation, full Actions rerun, artifacts, dispatch, cancel, delete, attempt API, or other general GitHub compatibility is provided.
