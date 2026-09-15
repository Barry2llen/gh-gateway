# P1-B Actions Compatibility Mapping

本文记录 Codex `babysit-pr` 在 CI / GitHub Actions 场景中实际需要的兼容面，以及它与 Gitea 1.25.5 Actions API 的差异。

本文是只读调查报告，不实现 Gateway Actions 路由，不修改 Codex 或 watcher，也不扩大到 PR create、merge、comment/review mutation 或通用 GitHub Actions 兼容性。

## 1. Evidence baseline

### 1.1 调查快照

调查日期为 2026-09-15（Asia/Shanghai）。调查开始时重新解析 upstream `openai/codex` `main`，得到 watcher commit `0265dd7b4547fd6a88c6458f359b3c20731421c4`：

- [当前 `gh_pr_watch.py`](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/scripts/gh_pr_watch.py)
- [当前 `babysit-pr/SKILL.md`](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/SKILL.md)

| 组件 | 本次调查版本 / commit | 证据与说明 |
| --- | --- | --- |
| Codex `babysit-pr` | upstream `main`，`0265dd7b4547fd6a88c6458f359b3c20731421c4` | 当前 upstream 源码；本报告以此为权威。 |
| 现有 Docker watcher pin | `b13164d86f9a70adc48d22f4a5a07ed0c001a1d0` | [`docker-compose.yml`](../docker-compose.yml) 与 [`docker/e2e/Dockerfile`](../docker/e2e/Dockerfile) 中的历史 E2E pin；不替代当前 upstream 证据。 |
| 旧 P1-A 文档快照 | `b0af519c39766c173191fc39b341808619b51c74` | [`docs/p1a-api-mapping.md`](p1a-api-mapping.md) 的历史调查 commit；用于解释基线差异。 |
| GitHub CLI | `v2.100.0`，commit `45437bc7eeeb3359bbfddd1742f79de7652fd3e2` | 本次动态 Actions trace 使用的真实 binary；[CLI source](https://github.com/cli/cli/tree/45437bc7eeeb3359bbfddd1742f79de7652fd3e2)。 |
| GitHub CLI | `v2.95.0`，commit `70bb306bd25eb407f90eabefd98824aed62cf519` | Windows host binary 与现有 P0/P1-A E2E 固定版本；[CLI source](https://github.com/cli/cli/tree/70bb306bd25eb407f90eabefd98824aed62cf519)。 |
| Gitea | `v1.25.5`，annotated tag object `366d371828ad0316517799ccba4fefc707168929`，peeled commit `f913d90ab664c1deccdfbe8c563abb13e897d62b` | 本报告引用的 Gitea 源码均固定在 [peeled commit](https://github.com/go-gitea/gitea/tree/f913d90ab664c1deccdfbe8c563abb13e897d62b)。 |
| Gateway 工作树 | `main`，`951cecf`，调查前干净；本地领先 `origin/main` 1 个提交 | 本阶段只新增本文档。 |

主机上的 `gh --version` 为 2.95.0；动态 fixture 在现有 `gh-gateway-e2e-e2e` 镜像中使用 `/opt/gh-2.100.0/bin/gh`。现有 Docker 参数见 [`docker-compose.yml#L67-L69`](../docker-compose.yml#L67-L69)。

### 1.2 Compatibility 标签

| 标签 | 本文定义 |
| --- | --- |
| **Direct** | Gitea 有同等字段和语义；只需路径、认证或等价字段转发。 |
| **Emulated** | Gitea 数据足够，但 Gateway 必须组合请求、改 envelope、做分页或聚合。 |
| **Approximate** | 能提供对 watcher 有用的近似值，但不能声称复现 GitHub 语义。 |
| **Unsupported** | Gitea 1.25.5 公共 API 没有足够信息，不能诚实地提供等价值。 |
| **Unknown** | 当前源码、官方文档和动态 trace 都不足以确认；不以猜测填充。 |

## 2. Actual babysit-pr CI flow

### 2.1 当前 watcher 的调用链

`gh_text` 是唯一的 subprocess 边界：它构造 `gh` argv 并调用 `subprocess.run(..., capture_output=True, text=True)`；`gh_json` 再解析 stdout 为 JSON。来源：[watcher `gh_text`](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L113-L139)。

```text
resolve_pr()
   ↓
gh api user
   ↓
issue comments / review comments / reviews（P1-A，手工分页）
   ↓
gh pr checks PR_NUMBER
   ↓
gh api repos/OWNER/REPO/actions/runs -X GET -f head_sha=... -f per_page=100
   ↓
按 head_sha + failed conclusion 筛选 workflow runs
   ↓
对匹配且未完成或已失败的 run：
gh api repos/OWNER/REPO/actions/runs/RUN_ID/jobs -X GET -f per_page=100
   ↓
按 job.conclusion 筛选 failed_jobs
   ↓
生成 snapshot / actions

--once：返回 snapshot，不请求 logs，不请求 rerun

--retry-failed-now：
  先完整 collect_snapshot
  ↓
  检查 PR open、checks failed、failed_runs 非空、checks terminal、retry budget
  ↓
  对每个 failed run：gh -R OWNER/REPO run rerun RUN_ID --failed
      ↓
      gh 内部 GET run?exclude_pull_requests=true
      gh 内部 GET actions/workflows/WORKFLOW_ID
      gh 内部 POST actions/runs/RUN_ID/rerun-failed-jobs
```

`collect_snapshot` 的顺序和 Actions 进入点见 [watcher `collect_snapshot`](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L739-L797)。

### 2.2 日志命令的边界

当前 watcher 源码没有执行 `gh api .../actions/jobs/{job_id}/logs`，也没有执行 `gh run view`。它只在 `failed_jobs` 输出中构造 `logs_endpoint` 字符串。`babysit-pr/SKILL.md` 才要求 agent 在 `diagnose_ci_failure` 时手工执行 logs 诊断命令：[SKILL CI failure classification](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/SKILL.md#L68-L84)。

因此：

- logs 是后续人工诊断的兼容面，不是 `babysit-pr --once` 继续运行所需的 watcher subprocess；
- run-level ZIP logs 不能被列入当前 watcher 的最小必需面；
- 不能因为 SKILL 文档提到 `gh run view --log-failed`，就声称 watcher 会自动请求它。

## 3. Command / Endpoint Matrix

| Trigger | 真实 gh argv / 内部请求 | GitHub REST endpoint | watcher 实际消费 | Gitea 1.25.5 endpoint | Compatibility |
| --- | --- | --- | --- | --- | --- |
| 每次 snapshot | `gh api repos/OWNER/REPO/actions/runs -X GET -f head_sha=HEAD_SHA -f per_page=100` | `GET /api/v3/repos/{owner}/{repo}/actions/runs?head_sha=...&per_page=100` | `workflow_runs[]`；每项 `id`、`name`/`display_title`、`status`、`conclusion`、`html_url`、`head_sha` | `GET /api/v1/repos/{owner}/{repo}/actions/runs?head_sha=...&page=1&limit=100` | **Emulated**：`per_page`→`limit`；Gitea envelope 可直接复用，但字段语义有缺口。 |
| 每个待检查 run | `gh api repos/OWNER/REPO/actions/runs/RUN_ID/jobs -X GET -f per_page=100` | `GET /api/v3/repos/{owner}/{repo}/actions/runs/{run_id}/jobs?per_page=100` | `jobs[]`；每项 `id`、`name`、`status`、`conclusion`、`html_url` | `GET /api/v1/repos/{owner}/{repo}/actions/runs/{run}/jobs?page=1&limit=100` | **Emulated / Approximate**：参数名、分页和 URL/状态语义不同。 |
| `--retry-failed-now` 的 gh preflight | `gh -R OWNER/REPO run rerun RUN_ID --failed` 内部 `GET .../runs/RUN_ID?exclude_pull_requests=true` | `GET /api/v3/repos/{owner}/{repo}/actions/runs/{run_id}?exclude_pull_requests=true` | gh 需要 `id`、`workflow_id`；标准 run 字段应保持完整 | `GET /api/v1/repos/{owner}/{repo}/actions/runs/{run}` | **Emulated**：Gitea path 可提供 run，但外部 ID 与 run number 需保持一致。 |
| `--retry-failed-now` 的 gh preflight | 同一命令内部 `GET .../actions/workflows/WORKFLOW_ID` | `GET /api/v3/repos/{owner}/{repo}/actions/workflows/{workflow_id}` | gh 需要 workflow object，至少 `id`、`name` | `GET /api/v1/repos/{owner}/{repo}/actions/workflows/{workflow_id}` | **Direct / Emulated**：Gitea 有 endpoint；Gateway 仍需保持 GitHub envelope。 |
| `--retry-failed-now` | `gh -R OWNER/REPO run rerun RUN_ID --failed` | `POST /api/v3/repos/{owner}/{repo}/actions/runs/{run_id}/rerun-failed-jobs` | 无 response JSON 字段；request body 未启用 debug 时为空 | Gitea 1.25.5 没有对应 REST route；只有 Web UI rerun route | **Unsupported** failed-only direct semantics。 |
| SKILL 人工诊断（watcher 不执行） | `gh api repos/OWNER/REPO/actions/jobs/JOB_ID/logs` | `GET /api/v3/repos/{owner}/{repo}/actions/jobs/{job_id}/logs` → `302` → plain text | `gh api` 输出最终 raw bytes | `GET /api/v1/repos/{owner}/{repo}/actions/jobs/{job_id}/logs` → `200 text/plain` attachment | **Approximate / Emulated**：状态码、redirect 和 header 不同。 |
| SKILL 人工诊断（watcher 不执行） | `gh run view RUN_ID --log-failed` | run GET、workflow GET、jobs GET、`GET /api/v3/repos/{owner}/{repo}/actions/runs/{run_id}/logs` → `302` → ZIP | gh 读取并校验 ZIP；缺少 job entry 时回退 job logs | Gitea 没有对应 run-level REST logs endpoint | **Unsupported**，除非未来显式聚合并重新打 ZIP。 |

GitHub 官方文档列出的 runs 参数包括 `event`、`branch`、`status`、`actor`、`head_sha`、`page`、`per_page`；本 watcher 只发送 `head_sha` 和 `per_page`，没有发送其余参数，也没有 `--paginate`。[GitHub workflow runs API](https://docs.github.com/en/rest/actions/workflow-runs#list-workflow-runs-for-a-repository)

## 4. Workflow Runs

### 4.1 watcher 的真实筛选逻辑

请求构造见 [ `get_workflow_runs_for_sha`](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L316-L336)：

```bash
gh api repos/OWNER/REPO/actions/runs \
  -X GET \
  -f head_sha=HEAD_SHA \
  -f per_page=100
```

watcher 只要求顶层 object 和 `workflow_runs` list；`total_count` 不读取，且没有 `page`、`--paginate` 或 Link-header fallback。因此超过 100 条时，当前 profile 只看到 gh 返回的首屏。

`failed_runs_from_workflow_runs` 的行为见 [源码](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L339-L364)：

1. 非 object 项跳过；
2. `head_sha` 必须与 PR head SHA 完全相等；
3. `conclusion` 必须属于 `failure`、`timed_out`、`cancelled`、`action_required`、`startup_failure`、`stale`；
4. 输出 `run_id`、`workflow_name`（优先 `name`，再 fallback 到 `display_title`）、`status`、`conclusion`、`html_url`；
5. 按 workflow name、run ID 的字符串表示排序。

watcher 不按 `event`、workflow name、`created_at`、`updated_at` 或 `run_attempt` 选择 latest attempt，也不读取 `total_count`。

这些 Actions 请求没有 watcher-level retry 或 fallback；`gh` 非零退出、非 object envelope 或缺少 list 都会直接抛出 `GhCommandError`，本轮 snapshot 不会改用另一条 Actions endpoint。

### 4.2 Gitea runs API

Gitea 1.25.5 的路由注册为：

- `GET /api/v1/repos/{owner}/{repo}/actions/runs`
- query：`event`、`branch`、`status`、`actor`、`head_sha`、`page`、`limit`
- response：`{"total_count": ..., "workflow_runs": [...]}`

证据：[Gitea API route registration](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/api.go#L1188-L1214)、[run handler](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/repo/action.go#L697-L759)、[shared list implementation](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/shared/action.go#L113-L186)。

`ListRuns` 使用 `page` / `limit` 生成数据库 `ListOptions`，按 `head_sha` 写入 `CommitSHA` filter。Gitea `GetListOptions` 只读取 `page` 和 `limit`，不会读取 GitHub 的 `per_page`：[pagination helper](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/utils/page.go#L12-L17)。

Gitea response struct 明确使用 `workflow_runs` 与 `total_count`：[ActionWorkflowRunsResponse](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/modules/structs/repo_actions.go#L106-L135)。

### 4.3 Run 字段 mapping

| GitHub run field | watcher 是否消费 | Gitea 1.25.5 字段 / 生成方式 | Compatibility |
| --- | --- | --- | --- |
| `id` | 是，用于 jobs 和 rerun | `ActionRun.ID`，数据库主键，`int64` | **Direct**，需 JSON number/string 约定稳定。 |
| `name` | 是，workflow name fallback 的第一优先级 | `ActionWorkflowRun` 没有 `name` 字段 | **Unsupported** exact field；watcher 可 fallback 到 `display_title`。 |
| `display_title` | 是，在没有 `name` 时作为 workflow name | `run.Title` | **Approximate**：Gitea title 不保证等于 GitHub workflow name。 |
| `status` | 是，传入 failed-job 判断 | `ToActionsStatus` 输出 `queued`、`waiting`、`in_progress` 或 `completed` | **Emulated**；Gitea `waiting` 来自 blocked，未知 status 会变成空字符串。 |
| `conclusion` | 是，决定 failed run | Gitea 输出 `success`、`failure`、`cancelled`、`skipped`；非终态为空 | **Approximate**；缺少 `neutral`、`timed_out`、`action_required`、`stale`、`startup_failure`。 |
| `event` | 否 | `string(run.Event)` | **Direct** 字段，但 watcher 不消费。 |
| `head_sha` | 是，精确匹配 PR head SHA | `run.CommitSHA` | **Direct**。 |
| `head_branch` | 否 | 从 `run.Ref` 推导 branch | **Approximate**，deleted ref 等边界未被 watcher 使用。 |
| `run_attempt` | 否 | struct 有字段，但 `ToActionWorkflowRun` 没有赋值；默认 JSON 值为 0 | **Unsupported** exact attempt semantics。 |
| `run_number` | 否 | `run.Index` | **Direct** sequence-like field；不能与数据库 `id` 混用。 |
| `url` | 否 | `repo.APIURL()/actions/runs/{ID}` | **Emulated**：Gateway 必须对外改为 `/api/v3` host/path。 |
| `html_url` | 是，snapshot 输出 | `repo.HTMLURL()/actions/runs/{Index}` | **Approximate**：URL 使用 run number/index，而不是数据库 ID。 |
| `created_at` / `updated_at` | 否 | `ActionWorkflowRun` 不输出这两个字段 | **Unsupported** exact timestamps。 |
| `started_at` / `completed_at` | 否 | `run.Started` / `run.Stopped` | **Approximate**；rerun 会重置 started/stopped，并保留 previous duration。 |
| `workflow_id` / `path` | 否 | `WorkflowID` 进入 `Path`（`workflowID@ref`）；`workflow_id` 不直接输出 | **Approximate / Unsupported** exact GitHub workflow identity。 |
| `total_count` | 否 | `FindAndCount` 结果 | **Direct** envelope field，但 watcher 忽略。 |

转换证据：[Gitea `ToActionWorkflowRun`](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/services/convert/convert.go#L250-L275)；状态转换见 [`ToActionsStatus`](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/services/convert/convert.go#L277-L316)。

## 5. Jobs

### 5.1 watcher 的真实请求和失败判定

请求构造见 [`get_jobs_for_run`](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L367-L375)：

```bash
gh api repos/OWNER/REPO/actions/runs/RUN_ID/jobs \
  -X GET \
  -f per_page=100
```

`failed_jobs_from_workflow_runs` 的真实逻辑见 [源码](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L378-L427)：

```text
for run in workflow_runs:
  head_sha 不匹配 → skip
  run.id 缺失 → skip
  若 run.status == "completed" 且 run.conclusion 不在 failed set → skip
  否则 GET /actions/runs/{run.id}/jobs
  for job in jobs:
    job.conclusion 不在 failed set → skip
    否则输出 job.id/name/status/conclusion/html_url 和 logs_endpoint
```

这意味着：

- completed success/skipped/neutral run 不请求 jobs；
- 非 completed run 即使 run conclusion 为空，也会请求 jobs；
- failed job 的判定只看 `conclusion`，不看 `status`、`steps` 或 step conclusion；
- matrix job 可以自然地逐项输出；代码没有假设 job name 唯一；
- `total_count`、`run_id`、`started_at`、`completed_at`、`steps` 等字段不被 watcher 读取；
- 不使用 `filter=latest`，也不使用 `page` 或 `--paginate`。

GitHub jobs API 的 envelope 和字段示例见 [官方 workflow jobs 文档](https://docs.github.com/en/rest/actions/workflow-jobs#list-jobs-for-a-workflow-run)：顶层为 `total_count` / `jobs`，job 至少包含 `id`、`run_id`、`status`、`conclusion`、`name`、`html_url` 和 `steps`。

### 5.2 Gitea jobs API

Gitea 1.25.5 注册：

```text
GET /api/v1/repos/{owner}/{repo}/actions/runs/{run}/jobs
```

query 为 `status`、`page`、`limit`；`run` 由路由解析为正 `int64`，`run <= 0` 返回 400。response 为 `{"total_count": ..., "jobs": [...]}`。

证据：[route](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/api.go#L1289-L1305)、[handler](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/repo/action.go#L1154-L1210)、[shared list](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/shared/action.go#L23-L83)。

### 5.3 Job 字段 mapping

| GitHub job field | watcher 是否消费 | Gitea 1.25.5 字段 / 生成方式 | Compatibility |
| --- | --- | --- | --- |
| `id` | 是，用于 failed job identity 和 logs endpoint | `ActionRunJob.ID` | **Direct**。 |
| `run_id` | 否 | `ActionRunJob.RunID` | **Direct**，但 watcher 已从外层 run 保存关联。 |
| `name` | 是，输出和排序 | `ActionRunJob.Name` | **Direct**。 |
| `status` | 是，输出；不参与失败判定 | `ToActionsStatus(job.Status)` | **Emulated / Approximate**，未知状态为空。 |
| `conclusion` | 是，唯一的 failed-job 判定字段 | `ToActionsStatus(job.Status)` 的 conclusion | **Approximate**，枚举比 GitHub 窄。 |
| `html_url` | 是，输出 | `repo.HTMLURL()/actions/runs/{run.Index}/jobs/{jobIndex}` | **Approximate**：使用 job index，而不是 job ID。 |
| `url` | 否 | `repo.APIURL()/actions/jobs/{job.ID}` | **Emulated**，需对外改 host/path。 |
| `run_url` | 否 | `repo.APIURL()/actions/runs/{job.RunID}` | **Emulated**。 |
| `head_sha` / `head_branch` | 否 | run 的 `CommitSHA` / `Ref` | **Direct / Approximate**。 |
| `run_attempt` | 否 | `ActionRunJob.Attempt` | **Direct** 字段，但 watcher 不消费。 |
| `steps` | 否 | 从 task steps 生成；每个 step 使用 job 总体 status/conclusion | **Approximate**，不能当作 GitHub step 状态；当前 watcher 不读取。 |
| `started_at` / `completed_at` | 否 | `job.Started` / `job.Stopped` | **Approximate**，当前 watcher 不读取。 |
| `total_count` | 否 | Gitea `FindAndCount` | **Direct** envelope field，但 watcher 忽略。 |

证据：[Gitea job structs](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/modules/structs/repo_actions.go#L137-L184)、[job converter](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/services/convert/convert.go#L319-L390)。

## 6. Logs

### 6.1 watcher 与 SKILL 的区别

当前 `babysit-pr --once` / `--watch` watcher 不下载日志；失败 job 只产生如下数据：

```json
{
  "job_id": 201,
  "logs_endpoint": "repos/OWNER/REPO/actions/jobs/201/logs"
}
```

真正的 logs 读取来自 SKILL 的人工诊断流程：

```bash
gh api repos/OWNER/REPO/actions/jobs/JOB_ID/logs > /tmp/codex-gh-job-JOB_ID-logs.zip
gh run view RUN_ID --log-failed
```

它们必须在 compatibility 文档中标为“人工诊断”，不能写成 watcher 的自动 subprocess。

### 6.2 `gh api` job logs

GitHub 官方 API 对 `GET /actions/jobs/{job_id}/logs` 定义为 `302 Found`，`Location` 指向短期有效的 plain-text 下载 URL：[workflow jobs logs 文档](https://docs.github.com/en/rest/actions/workflow-jobs#download-job-logs-for-a-workflow-run)。

`gh api` 的 transport：

- `-X GET` 强制 GET；
- `-f` 在 GET 下形成 query；
- watcher 没有使用 `-F`（typed field）或 `--paginate`；这两个 gh 通用选项不属于当前 compatibility profile；
- 不传自定义 header 时，v2.100.0 的 HTTP 层默认 Accept 为 `*/*`；
- 普通 HTTP client 会跟随 redirect；
- 非 JSON body 不做 JSON decode，pipe 场景下将 response bytes 原样写入 stdout；
- 2xx 以外最终转为 gh error。

源码证据：[API request construction](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/pkg/cmd/api/http.go#L16-L92)、[response processing](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/pkg/cmd/api/api.go#L477-L566)。redirect-follow 与认证 header 的行为也由 [CLI HTTP client](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/api/http_client.go#L35-L89) 和 [client tests](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/api/client_test.go#L355-L432) 覆盖。

### 6.3 `gh run view --log-failed`

`gh run view` 不是 watcher subprocess，但它是 SKILL 指定的 fallback。v2.100.0 的真实调用链为：

1. `GET /actions/runs/{run_id}?exclude_pull_requests=true`；
2. `GET /actions/workflows/{workflow_id}`，补齐 workflow name；
3. `GET /actions/runs/{run_id}/jobs?per_page=100`；
4. `GET /actions/runs/{run_id}/logs`，要求 200 后的 body 是有效 ZIP；
5. 读取 ZIP 中 job/step entries；
6. 如果 ZIP 没有对应 job entry，回退到 `GET /actions/jobs/{job_id}/logs`，最多分配 25 个 API log fetcher。

当 agent 显式传入 `--attempt N` 时，gh 会把 jobs/logs 改为 `/actions/runs/{run_id}/attempts/{N}/jobs` 和 `/actions/runs/{run_id}/attempts/{N}/logs`；当前 babysit-pr watcher 从不传 `--attempt`，Gitea 1.25.5 也没有对应的 attempt REST route。

源码：[run view flow](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/pkg/cmd/run/view/view.go#L207-L347)、[run ZIP fetch](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/pkg/cmd/run/view/view.go#L471-L535)、[job-log fallback](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/pkg/cmd/run/view/logs.go#L41-L155)。

GitHub run-level logs endpoint同样由官方文档定义为 `302` 后的 ZIP：[workflow run logs](https://docs.github.com/en/rest/actions/workflow-runs#download-workflow-run-logs)。

### 6.4 Gitea 1.25.5 logs

Gitea 1.25.5 只有 job-level REST logs：

```text
GET /api/v1/repos/{owner}/{repo}/actions/jobs/{job_id}/logs
```

路由和 handler 见 [Gitea API logs route](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/api.go#L1211-L1214) 与 [handler](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/repo/actions_run.go#L15-L64)。最终由 [`DownloadActionsRunJobLogs`](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/common/actions.go#L25-L66) 返回：

- `200 OK`；
- `Content-Type: text/plain; charset=utf-8`；
- `Content-Disposition: attachment`；
- body 为 plain text，不是 ZIP，也没有 GitHub 风格的 `302`；
- 日志行由 Gitea `FormatLog` 生成时间戳和内容，单行内容上限 64 KiB；内部 `.zst` 存储在 HTTP 层被解压。[log formatter](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/modules/actions/log.go#L178-L223)

Gitea 没有 `/api/v1/repos/{owner}/{repo}/actions/runs/{run_id}/logs` REST 路由。Web UI 的 `/owner/repo/actions/runs/{run}/jobs/{job}/logs` 是单个 job index 的页面/日志路径，不是 GitHub run ZIP：[web routes](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/web/web.go#L1447-L1476)。

| 日志能力 | GitHub transport | Gitea transport | Compatibility |
| --- | --- | --- | --- |
| 单 job logs | `302` → plain text | `200` plain text attachment | **Approximate**；若 Gateway 合成 302 可标 **Emulated**。 |
| 整个 run logs | `302` → ZIP | 无公共 REST 等价 endpoint | **Unsupported**；需要按 jobs 读取并重新打 GitHub ZIP。 |
| step logs | ZIP entry；缺失时只能 fallback 到 job logs | REST 只有 job logs，Web UI 按 job index | **Unsupported / Approximate**，不能假装逐 step Direct。 |

## 7. Rerun

### 7.1 watcher 触发条件

`retry_failed_now` 先调用完整 `collect_snapshot`，然后依次检查：[源码](https://github.com/openai/codex/blob/0265dd7b4547fd6a88c6458f359b3c20731421c4/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L800-L852)

1. PR 没有 closed/merged；
2. `checks.failed_count > 0`；
3. `failed_runs` 非空；
4. checks 已 terminal；
5. 当前 SHA 的 retry count 小于 `max_flaky_retries`。

满足后，对每个有 `run_id` 的 failed run 执行：

```bash
gh -R OWNER/REPO run rerun RUN_ID --failed
```

没有备用 rerun 命令。`--once` 和普通 `--watch` snapshot 不发送 rerun；`--watch` 只输出 `retry_failed_checks` action。

### 7.2 gh v2.100.0 / v2.95.0 的实际 REST 行为

`gh run rerun` 的源码将 `--failed` 映射为 `rerun-failed-jobs`，并且不是只发一个 POST：

1. `GET repos/{owner}/{repo}/actions/runs/{run_id}?exclude_pull_requests=true`；
2. `GET repos/{owner}/{repo}/actions/workflows/{workflow_id}`；
3. `POST repos/{owner}/{repo}/actions/runs/{run_id}/rerun-failed-jobs`。

gh 还支持完整 run rerun（`/actions/runs/{run_id}/rerun`）和指定 job rerun（`/actions/jobs/{job_id}/rerun`），但当前 watcher 没有传入 `--job`，也没有调用完整 rerun 命令。

源码：[v2.100.0 rerun implementation](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/pkg/cmd/run/rerun/rerun.go#L97-L214)、[run preflight](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/pkg/cmd/run/shared/shared.go#L559-L596)。

request body：

- watcher 没有 `--debug`，因此 `requestBody(false)` 返回 `nil`，POST body 为空；
- gh 的通用能力在 `--debug` 时发送 `{"enable_debug_logging":true}`；这不是 watcher profile。

GitHub 官方 API 将 full rerun 与 failed-only rerun 都定义为 `POST`、成功状态 `201`，failed-only 的语义是“失败 jobs 及其 dependent jobs”：[rerun workflow](https://docs.github.com/en/rest/actions/workflow-runs#re-run-a-workflow)、[rerun failed jobs](https://docs.github.com/en/rest/actions/workflow-runs#re-run-failed-jobs-from-a-workflow-run)。

本次动态 fixture 还有一个重要的 gh 兼容观察：

- 返回 `201` 且完全空 body 时，真实 gh 2.100.0 报 `unexpected end of JSON input`；
- 返回 `201` 且 body 为 `{}` 时，`gh run rerun ... --failed` 成功；
- CLI v2.100.0 单元测试也用 `{}` 作为成功 POST stub：[rerun tests](https://github.com/cli/cli/blob/45437bc7eeeb3359bbfddd1742f79de7652fd3e2/pkg/cmd/run/rerun/rerun_test.go#L202-L224)。

因此，后续 Gateway 实现若要支持当前 gh profile，应优先返回 `201` 加合法 JSON `{}`；不要只根据 GitHub 文档“无 response schema”返回空 body。该 body 结论是 gh 2.100.0 兼容观察，不扩展为所有未来 gh 版本的永久保证。

v2.95.0 与 v2.100.0 的源码 diff 只显示 safe URL/API client 重构，run rerun path、method、failed-only verb 和 debug body 语义相同：[v2.95.0 rerun source](https://github.com/cli/cli/blob/70bb306bd25eb407f90eabefd98824aed62cf519/pkg/cmd/run/rerun/rerun.go#L188-L249)。本次 Actions 动态 trace 使用 v2.100.0；没有为 v2.95.0 再建立独立 Actions fixture trace。

### 7.3 Gitea rerun 语义

Gitea 1.25.5 API route registration 在 `/actions/runs/{run}` 下只有 GET、DELETE、jobs、artifacts，没有 `POST rerun` 或 `POST rerun-failed-jobs`：[API routes](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/api.go#L1289-L1305)。全仓库 API 路由中也没有对应 REST rerun endpoint。

Gitea Web UI 另有：

- `POST /{username}/{reponame}/actions/runs/{run}/rerun`：重置 run 并 rerun 全部 jobs；
- `POST /{username}/{reponame}/actions/runs/{run}/jobs/{job}/rerun`：按 job index 选择 job，并通过 `GetAllRerunJobs` 加上依赖 jobs。

证据：[web route](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/web/web.go#L1454-L1472)、[Web rerun implementation](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/web/repo/actions/view.go#L399-L501)、[dependency expansion](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/services/actions/rerun.go#L11-L38)。

| GitHub/gh operation | Gitea 1.25.5 现状 | Compatibility |
| --- | --- | --- |
| full workflow rerun `/runs/{id}/rerun` | 只有 Web UI 路径；它使用 run index，且需要 session/CSRF | **Unsupported** REST；不能直接暴露为 GitHub API。 |
| failed-only `/runs/{id}/rerun-failed-jobs` | 没有 REST endpoint | **Unsupported**。 |
| single job `/jobs/{id}/rerun` | 只有 Web UI 的 job index route，并会扩展 dependencies | **Approximate / Unsupported**，不能冒充 watcher 的 failed-only 操作。 |

## 8. Status Mapping

### 8.1 watcher 的失败集合

当前 watcher 的 `FAILED_RUN_CONCLUSIONS` 为：

```text
failure, timed_out, cancelled, action_required, startup_failure, stale
```

`neutral`、`skipped`、`success` 和空/未知 conclusion 不会进入 `failed_runs`。`PENDING_CHECK_STATES`（`QUEUED`、`IN_PROGRESS`、`PENDING`、`WAITING`、`REQUESTED`）只用于 `gh pr checks` 汇总，不应与 workflow run conclusion 混淆。

### 8.2 GitHub 与 Gitea 输出

| GitHub run status / conclusion | watcher effect | Gitea 1.25.5 output | Compatibility / safety |
| --- | --- | --- | --- |
| `status=queued` | run 本身非 failed；jobs 阶段会因非 completed 而继续检查 | `StatusWaiting` → `status=queued` | **Direct** 状态近似。 |
| `status=waiting` / `requested` / `pending` | 非 completed；若 jobs 中有 failed conclusion，仍可生成 failed job | `StatusBlocked` → `status=waiting` | **Approximate**；Gitea 没有独立 requested/pending output。 |
| `status=in_progress` | 非 completed；会请求 jobs | `StatusRunning` → `status=in_progress` | **Direct**。 |
| `status=completed`, `conclusion=success` | 不生成 failed run；checks 仍由 `gh pr checks` 决定 | `StatusSuccess` → `completed/success` | **Direct**。 |
| `status=completed`, `conclusion=failure` | 生成 failed run | `StatusFailure` → `completed/failure` | **Direct**。 |
| `status=completed`, `conclusion=cancelled` | 生成 failed run | `StatusCancelled` → `completed/cancelled` | **Direct**。 |
| `status=completed`, `conclusion=skipped` | 不生成 failed run | `StatusSkipped` → `completed/skipped` | **Direct**，但不是成功。 |
| `status=completed`, `conclusion=neutral` | 不生成 failed run | Gitea 无 neutral；query filter 将 neutral 归为 skipped | **Unsupported / Approximate**。不能映射成 success。 |
| `status=completed`, `conclusion=timed_out` | 生成 failed run | Gitea 无 timed_out；query filter 可将 timed_out 归入 cancelled，但 output 不保留 timed_out | **Approximate**，需保留失败属性。 |
| `status=completed`, `conclusion=action_required` | 生成 failed run | Gitea 无 action_required；内部 query filter 将其归入 blocked | **Unsupported** exact conclusion；不能映射 success。 |
| `status=completed`, `conclusion=stale` | 生成 failed run | Gitea 无 stale | **Unsupported** exact conclusion；必须保守为非绿色。 |
| `status=completed`, `conclusion=startup_failure` | 生成 failed run | Gitea 无 startup_failure | **Unsupported** exact conclusion；必须保守为非绿色。 |
| 空或未知 status/conclusion | 当前 watcher 可能不把它计为 failed | `StatusUnknown` 的转换结果为空字符串 | **Unknown / 风险项**：绝不能把未知值转成 success、completed/success 或其他更健康状态。 |

Gitea 内部状态枚举只有 `unknown`、`success`、`failure`、`cancelled`、`skipped`、`waiting`、`running`、`blocked`：[Gitea status enum](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/models/actions/status.go#L14-L109)。API query status 的转换也明确将 `neutral`→`skipped`、`timed_out`→`cancelled`、`action_required`→`blocked`：[query conversion](https://github.com/go-gitea/gitea/blob/f913d90ab664c1deccdfbe8c563abb13e897d62b/routers/api/v1/shared/action.go#L85-L110)。这些是 Gitea 查询过滤语义，不能倒推为完整的 GitHub output conclusion mapping。

## 9. Dynamic Trace

### 9.1 Fixture 边界

动态 trace 使用一次性 HTTPS fixture responder，运行在现有 `gh-gateway-e2e-e2e` 镜像内部：

- real `gh 2.100.0`；
- 从 commit `0265dd7...` 下载的未修改 watcher；
- `GH_HOST=127.0.0.1:8443`、真实 Enterprise token 传递；
- fixture 只提供让 watcher 越过 P1-A 的最小 GraphQL/REST response，并记录 method/path/query/body/status/content type/字节数；
- fixture、证书和 access log 均为容器内临时文件，没有写入仓库。

fixture 构造了三个 runs：

- run 101：head SHA 匹配、`completed/failure`；
- run 102：head SHA 匹配、`completed/success`；
- run 103：head SHA 不匹配、`completed/failure`。

run 101 有两个 jobs：201 `failure`、202 `success`。

### 9.2 `--once`

watcher 退出状态为 0，Actions 部分的 access log 顺序为：

```text
GET /api/v3/repos/fixture/repo/actions/runs
    ?head_sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&per_page=100  -> 200
GET /api/v3/repos/fixture/repo/actions/runs/101/jobs
    ?per_page=100                                                   -> 200
```

没有请求：

```text
/actions/runs/102/jobs
/actions/runs/103/jobs
/actions/jobs/201/logs
/actions/runs/101/rerun-failed-jobs
```

snapshot 关键结果：

```json
{
  "actions": ["diagnose_ci_failure", "retry_failed_checks"],
  "checks": {"all_terminal": true, "failed_count": 1, "pending_count": 0},
  "failed_runs": [{"run_id": 101, "conclusion": "failure", "workflow_name": "CI"}],
  "failed_jobs": [{"job_id": 201, "job_name": "build", "conclusion": "failure"}]
}
```

这验证了源码中的 head SHA、completed nonfailed run 和 failed job filters，并验证 `--once` 不会自动下载 logs 或执行 rerun。

### 9.3 `--retry-failed-now`

watcher 退出状态为 0，结果为 `reason=rerun_triggered`、`rerun_count=1`。除同样的 runs/jobs snapshot 外，真实 gh access log 为：

```text
GET  /api/v3/repos/fixture/repo/actions/runs/101
     ?exclude_pull_requests=true                                  -> 200
GET  /api/v3/repos/fixture/repo/actions/workflows/1               -> 200
POST /api/v3/repos/fixture/repo/actions/runs/101/rerun-failed-jobs
     body bytes=0                                                  -> 201
```

POST 的成功 fixture body 为 `{}`；将同一响应改为 `201` 空 body 时，gh 2.100.0 返回 `unexpected end of JSON input`，与第 7 节的 CLI test/source 证据一致。

### 9.4 job logs redirect

直接执行 SKILL 诊断命令：

```bash
gh api repos/fixture/repo/actions/jobs/201/logs
```

fixture/access log：

```text
GET /api/v3/repos/fixture/repo/actions/jobs/201/logs -> 302
GET /download/job-201.txt                         -> 200 text/plain
```

最终 stdout 为 38 bytes 的 plain text；gh 自动跟随 redirect，没有输出 JSON envelope。

### 9.5 run-level ZIP 和 job fallback

`gh run view -R fixture/repo 101 --log-failed` 在 run ZIP 有 `0_build.txt` 时：

```text
GET /actions/runs/101?exclude_pull_requests=true -> 200
GET /actions/workflows/1                       -> 200
GET /actions/runs/101/jobs?per_page=100         -> 200
GET /actions/runs/101/logs                      -> 302
GET /download/run-101.zip                       -> 200 application/zip
```

gh 解压并输出带 job/step 前缀的文本；没有再请求 job logs。

第二个 fixture 返回合法但空的 ZIP，观察到：

```text
GET /actions/runs/101/logs                      -> 302
GET /download/empty.zip                          -> 200 application/zip
GET /actions/jobs/201/logs                       -> 302
GET /download/job.txt                            -> 200 text/plain
```

最终输出为 `build\tUNKNOWN STEP\tfallback job log`，证明缺少 ZIP job entry 时才触发 job-level logs fallback。

### 9.6 gh 2.95.0 说明

本次动态 Actions trace 使用 2.100.0。v2.95.0 与 v2.100.0 的 `rerun.go`、`view.go`、`logs.go`、`shared.go` diff 未发现目标 endpoint、method、failed-only verb 或 ZIP/job fallback 语义差异；差异主要是 safe URL 和 API client 重构。现有项目 E2E 已使用 2.95.0 验证 P1-A，但没有声称它已验证 Actions。

## 10. Known Semantic Gaps

| Gap | 事实 | 影响 |
| --- | --- | --- |
| watcher 不分页 runs/jobs | `gh api` 没有 `--paginate`；watcher 忽略 `total_count` | Gateway 只需保证首屏 profile，但不能声称全量 Actions 分页。 |
| query 名不同 | GitHub 使用 `per_page`；Gitea 使用 `limit` / `page` | Gateway 必须转换，不能直接透传。 |
| run `name` 缺失 | Gitea 只输出 `display_title`，且来源为 run title | workflow name 只能 Approximate。 |
| run ID 与 run number 分离 | Gitea `id` 是数据库主键，HTML URL 使用 `Index` | rerun、jobs 和 URL 必须避免把两者混用。 |
| `run_attempt` 不完整 | Gitea converter 未设置 run attempt | latest attempt 语义 Unsupported；当前 watcher 不读取。 |
| status/conclusion 枚举窄 | Gitea 缺少 neutral、timed_out、action_required、stale、startup_failure | 失败不能被映射为 success；未知值必须保守。 |
| job URL 不同 | Gitea HTML URL 使用 job index；API URL使用 job ID | 输出链接可用但不等价。 |
| step 语义不同 | Gitea step status/conclusion 从 job 总体状态生成 | 不能支持精确 step failure 诊断。 |
| job logs transport 不同 | GitHub `302`→plain text；Gitea `200` attachment | 可做路径/redirect emulation，但不能标 Direct。 |
| run logs 缺失 | Gitea 没有 run-level ZIP REST endpoint | `gh run view --log-failed` 需要聚合/重打 ZIP，否则 Unsupported。 |
| failed-only rerun 缺失 | Gitea REST 没有 rerun；Web UI 只能全 run 或指定 job+dependencies | 当前 `--retry-failed-now` 无诚实 Direct mapping。 |
| gh rerun preflight | gh 在 POST 前读取 run 和 workflow | 只实现 POST 而不提供两个 GET，真实 gh 仍会失败。 |
| 201 body 兼容性 | 动态 gh 2.100.0 对空 body 报 JSON EOF；`{}` 成功 | Gateway response 应优先使用 `201` + `{}`。 |
| 错误传播 | watcher 的 `gh_json` 对非 2xx 直接抛 `GhCommandError`；没有 Actions fallback | runs/jobs/rerun 的错误必须保持可诊断，不伪造空成功。 |

## 11. Recommended TDD Slices

### Slice 1：Actions runs read profile

- 新增外部 `/api/v3/repos/{owner}/{repo}/actions/runs` GET 的 handler/client/service 测试；上游调用 `/api/v1/.../actions/runs`。
- 覆盖 `head_sha`、`per_page`→`limit`、`page=1`、`workflow_runs` envelope、首屏 `total_count`。
- 覆盖匹配 SHA、错误 SHA、success/skipped/unknown conclusion；未知信息不得变成 success。

### Slice 2：workflow run jobs

- 实现 `/actions/runs/{run_id}/jobs` GET 的 `per_page`→`limit` 映射。
- 覆盖 completed nonfailed run 不触发上游 jobs、unfinished run 会触发、failed job conclusion、matrix job、job ID/name/status/html_url。
- 明确 `steps`、`run_attempt` 和 job index URL 不参与当前 watcher 判定。

### Slice 3：job logs（人工诊断 profile）

- 单独测试 `/actions/jobs/{job_id}/logs` 的 plain-text body、Content-Disposition、缺失任务 404。
- 若要满足 GitHub transport，再测试 Gateway 是否合成 302；不要把 200 text 直接标为 Direct。
- 不在此 slice 偷换成 run ZIP；run-level logs 另列可选 slice。

### Slice 4：rerun failed-only

- 先测试 gh 预读 run/workflow，再测试 POST `/actions/runs/{id}/rerun-failed-jobs` 的 201 + `{}`。
- 将 Gitea 无 REST failed-only endpoint 作为明确阻塞；只有实现了可验证 failed/dependency 语义后才允许从 Unsupported 升级。
- 测试 PR closed、no failed checks、pending checks、retry budget exhausted、多个 failed runs 和缺失 run ID。

### Slice 5：babysit-pr Actions E2E

- 使用当前 upstream watcher SHA 和真实 gh 2.100.0；保留现有 P1-A fixture/Gateway。
- `--once`：验证 runs→必要 jobs，且无 logs/rerun。
- `--retry-failed-now`：验证 run/workflow preflight→failed-only POST→状态文件 retry count。
- 增加 unknown conclusion/status、非匹配 SHA、matrix job 和 2xx/4xx 错误场景。
- 如果后续决定支持人工 `gh run view --log-failed`，再增加 run ZIP 和 job-log fallback E2E；这不是当前 watcher minimum。

## 12. Minimum P1-B Gateway Surface

下面只列当前 `babysit-pr` watcher 实际要求的 Actions surface。日志诊断 endpoint 单独列为 optional，不把 SKILL 人工命令误算成 `--once` 必需面。

| 外部 Gateway path / method | 外部 query / body | 最小 response | 上游 Gitea path | status/error 要求 |
| --- | --- | --- | --- | --- |
| `GET /api/v3/repos/{owner}/{repo}/actions/runs` | `head_sha`、`per_page`；watcher 不发送 `page`/`event`/`status`/`branch` | `200 {"total_count": number, "workflow_runs": [...]}`；每项至少 `id`、`head_sha`、`status`、`conclusion`、`name` 或 `display_title`、`html_url` | `GET /api/v1/repos/{owner}/{repo}/actions/runs?head_sha=...&page=1&limit=...` | 非 2xx 直接导致 watcher snapshot 失败；不能把未知/缺失 run 当 success。 |
| `GET /api/v3/repos/{owner}/{repo}/actions/runs/{run_id}/jobs` | `per_page`；watcher 不发送 `page`/`filter` | `200 {"total_count": number, "jobs": [...]}`；每项至少 `id`、`name`、`status`、`conclusion`、`html_url` | `GET /api/v1/repos/{owner}/{repo}/actions/runs/{run}/jobs?page=1&limit=...` | 非 2xx 直接导致 watcher snapshot 失败；run ID 必须与 runs response 的 `id` 一致。 |
| `GET /api/v3/repos/{owner}/{repo}/actions/runs/{run_id}` | gh rerun 内部追加 `exclude_pull_requests=true` | 至少合法 run object：`id`、`workflow_id`、`status`、`conclusion`；建议返回完整标准 run 字段 | `GET /api/v1/repos/{owner}/{repo}/actions/runs/{run}` | gh preflight 失败则不会 POST rerun；404/403 应保持 GitHub-compatible error。 |
| `GET /api/v3/repos/{owner}/{repo}/actions/workflows/{workflow_id}` | 无 | 至少合法 workflow object：`id`、`name` | `GET /api/v1/repos/{owner}/{repo}/actions/workflows/{workflow_id}` | gh rerun preflight 必须成功；不要只实现 rerun POST。 |
| `POST /api/v3/repos/{owner}/{repo}/actions/runs/{run_id}/rerun-failed-jobs` | watcher body 为空；debug body 不属于当前 profile | `201`；为满足 gh 2.100.0，建议 body 为 `{}` | Gitea 1.25.5 无等价 REST endpoint | 只有确认 failed-only 与依赖语义后才可实现；否则必须保持 Unsupported，而不是调用全 run rerun。 |

### Optional：人工日志诊断，不属于当前 watcher minimum

| 外部 path | 事实要求 | 当前结论 |
| --- | --- | --- |
| `GET /api/v3/repos/{owner}/{repo}/actions/jobs/{job_id}/logs` | GitHub 302→plain text；gh api 会 follow redirect；Gitea 只有 200 plain text job log | **Approximate / Emulated**。 |
| `GET /api/v3/repos/{owner}/{repo}/actions/runs/{run_id}/logs` | GitHub 302→ZIP；`gh run view` 校验 ZIP，并在缺 entry 时 fallback job logs | Gitea 无公共 REST 等价；**Unsupported**，除非另行实现聚合和 ZIP 重打包。 |

本阶段没有新增 Go public API、interface 或正式 Gateway route；正式实现仍应保持 P0/P1-A scope 不变。
