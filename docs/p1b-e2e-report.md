# P1-B Actions E2E Report

本报告记录 2026-09-16（Asia/Shanghai）对 P1-B GitHub Actions 兼容面的实现后验证。结论仅覆盖 Codex `babysit-pr` 当前使用的 monitoring profile 和 job-level 人工日志诊断，不代表通用 GitHub Actions 兼容。

## Versions

| Component | Version / commit |
| --- | --- |
| Codex `babysit-pr` watcher | `0265dd7b4547fd6a88c6458f359b3c20731421c4`，未修改脚本 |
| GitHub CLI | `2.100.0`（Actions 与 watcher E2E）；`2.95.0`（既有 P0/P1-A 回归） |
| Gitea | `1.25.5` |
| Gitea act_runner | `v0.2.10`，`gh-gateway-e2e:host` label |

## Implemented

- repository workflow runs：`head_sha` 直传，`per_page` 映射为 Gitea `page=1&limit=N`；
- workflow run jobs：保留每个数据库 job ID，包括 matrix 展开的同名 jobs；
- run lookup 与 workflow lookup：run ID 始终使用 Gitea 数据库 ID，workflow 文件名通过稳定正 `int64` 代理 ID 暴露给 gh；
- job-level logs：直接返回 Gitea 的 `200 text/plain` 原始内容；
- failed-only rerun route：明确返回 `501 Not Implemented`，没有 Gitea mutation provider 或 fallback。

未知、空或不一致的 Gitea status/conclusion 会产生 compatibility error，不会被提升为 `completed/success`。workflow 代理 ID 属于 **Emulated**；job logs 因没有复现 GitHub `302` transport，属于 **Approximate**。

## Unsupported

- run-level ZIP logs；
- failed-only workflow rerun；
- full rerun、Web UI rerun、artifacts、dispatch、cancel、delete 和 attempt API。

## Protocol E2E

协议 E2E 使用真实 gh 2.100.0、真实未修改 watcher 和真实 Gateway。P0/P1-A 请求仍转发到真实 Gitea；只有 Actions provider 数据由确定性 fixture 提供，并记录每一条上游请求。

`babysit-pr --once` exit code 为 0。snapshot 包含：

```text
failed_runs: run 101, failure
failed_jobs: job 201 failure, job 202 cancelled
actions: diagnose_ci_failure, retry_failed_checks
```

fixture 同时证明：completed success/skipped 和错误 SHA 不请求 jobs；non-completed run 104 会请求 jobs；`--once` 不请求 logs 或 rerun。

`babysit-pr --retry-failed-now` 的外部 trace：

```text
GET  /api/v3/repos/gateway/bar/actions/runs
GET  /api/v3/repos/gateway/bar/actions/runs/101/jobs
GET  /api/v3/repos/gateway/bar/actions/runs/104/jobs
GET  /api/v3/repos/gateway/bar/actions/runs/101?exclude_pull_requests=true
GET  /api/v3/repos/gateway/bar/actions/workflows/1557893587334474976
POST /api/v3/repos/gateway/bar/actions/runs/101/rerun-failed-jobs -> 501
```

Actions fixture 的完整上游日志只包含 GET；unsupported POST 没有穿透 Gateway。

独立真实 gh fixture 还验证：`201 application/json` 加 `{}` 成功，而 `201` 空 body 失败并显示 `unexpected end of JSON input`。该 fixture 只固化未来成功实现的 gh 协议要求，不表示生产 route 支持 rerun。

## Full real-Gitea Actions E2E

真实 Gitea fixture 在默认分支保存 workflow identity，并在 PR feature 分支触发两个 matrix 展开 jobs；两个 job 都输出唯一标记后确定性失败。验证结果：

- `gh api .../actions/runs` 返回真实 completed/failure run；
- `gh api .../actions/runs/1/jobs` 返回两个不同 job ID，均保留；
- `gh api .../actions/jobs/1/logs` 输出真实 Gitea plain-text 日志与标记；
- `babysit-pr --once` exit code 为 0，包含真实 failed run、两个 failed jobs 及两项 CI actions；
- `--once` 没有自动请求 logs 或 rerun。

真实 retry trace：

```text
GET  /api/v3/repos/gateway/bar/actions/runs
GET  /api/v3/repos/gateway/bar/actions/runs/1/jobs
GET  /api/v3/repos/gateway/bar/actions/runs/1?exclude_pull_requests=true
GET  /api/v3/repos/gateway/bar/actions/workflows/4940212140823242936
POST /api/v3/repos/gateway/bar/actions/runs/1/rerun-failed-jobs -> 501
```

POST 前后的真实 Gitea run status、conclusion、started/completed timestamps，以及全部 job ID/status/conclusion 完全一致，因此没有发生 full rerun、Web UI rerun或其他 workflow mutation。

## Validation

以下命令全部通过：

```text
go test ./...
go test -race ./...
go vet ./...
docker compose --profile test run --rm --build tests
docker compose up --build --abort-on-container-exit --exit-code-from e2e e2e
```

## Final verdict

```text
P1-B monitoring compatibility: PASS

P1-B failed-only retry compatibility: UNSUPPORTED
```
