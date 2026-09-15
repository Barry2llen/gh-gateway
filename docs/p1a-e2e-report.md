# P1-A Monitoring Compatibility E2E Report

- 验证日期：2026-09-15
- P1-A API compatibility profile：**PASS**
- 当前 `babysit-pr --once` 完整运行：**PARTIAL**（按 scope 拒绝实现其强制进入的 P1-B Actions）

## Versions

| 组件 | 版本 / commit |
| --- | --- |
| Codex babysit-pr | `b13164d86f9a70adc48d22f4a5a07ed0c001a1d0` |
| gh baseline | `2.95.0`（2026-06-17） |
| gh P1-A matrix | `2.100.0`（2026-09-03） |
| Gitea | `1.25.5` |
| Caddy | `2.10.2` |

## Implemented slices

| Slice | 能力 | 结果 |
| --- | --- | --- |
| 1 | authenticated user | PASS |
| 2 | expanded PR metadata + `PullRequestByNumber` | PASS |
| 3 | conversation comments | PASS |
| 4 | reviews | PASS |
| 5 | inline review comments | PASS |
| 6 | gh PR checks protocol | PASS |

所有 Slice 使用 `GraphQL/REST presenter → domain service → Gitea provider`，未向 compatibility handler 暴露 Gitea DTO。

## Compatibility matrix

| 行为 | 兼容级别 | 说明 |
| --- | --- | --- |
| PR direct metadata、head SHA、repository/owner | Direct / Emulated | Gitea字段映射为 GitHub GraphQL shape，ID 为 opaque string。 |
| `mergeable` | Approximate | true → MERGEABLE；false → UNKNOWN。 |
| `mergeStateStatus` | Unsupported exact semantics | draft → DRAFT；其他 → UNKNOWN，始终保守。 |
| `reviewDecision` | Unsupported | GraphQL null；gh JSON exporter 显示空字符串。 |
| conversation comments | Emulated | Gitea全量结果稳定排序并本地分页。 |
| reviews | Emulated / Approximate | 状态重命名；REQUEST_REVIEW 过滤；PENDING 保留。 |
| inline review comments | Emulated / Approximate | 跨 review 聚合；position 映射为 line。 |
| `author_association` | Approximate | OWNER、直接 COLLABORATOR、NONE；不声称 MEMBER。 |
| commit statuses | Emulated / Approximate | 仅 StatusContext；同 context 取最新。 |
| required checks | Unsupported | 当前 profile 固定 `isRequired=false`。 |
| warning / skipped status | Approximate | 保守映射为 ERROR，不映射为成功。 |
| CheckRun / WorkflowRun | Unsupported | feature detection 不声明能力，响应不生成 CheckRun。 |

## Real gh E2E output

expanded PR（两版 gh 均成功）：

```json
{"closedAt":null,"headRefName":"feature","headRefOid":"b02ce6329d8cfcce398dbd4437e9f6251a843510","headRepository":{"id":"1","name":"bar","nameWithOwner":"gateway/bar"},"headRepositoryOwner":{"id":"1","login":"gateway"},"mergeStateStatus":"UNKNOWN","mergeable":"UNKNOWN","mergedAt":null,"number":1,"reviewDecision":"","state":"OPEN","url":"https://git.example.test/gateway/bar/pulls/1"}
```

Gitea 的异步 mergeability 计算使两次读取可能分别出现 `MERGEABLE` 或 `UNKNOWN`；两者均按保守规则接受，而 `mergeStateStatus` 始终为 `UNKNOWN`，不会产生乐观 ready 判定。

真实 feedback fixture：

```text
conversation: forker / COLLABORATOR
review:       forker / COMMENTED / COLLABORATOR
inline:       feature.txt:1 / review id 1 / COLLABORATOR
```

`gh pr checks` 2.95.0 与 2.100.0 均得到三个 StatusContext：

```text
ci/failure -> FAILURE -> fail
ci/pending -> PENDING -> pending
ci/success -> SUCCESS -> pass
```

gh 2.100.0 的 `GH_DEBUG=api` 验证了以下 operation：

```text
PullRequestByNumber
PullRequest_fields
PullRequest_fields2
PullRequestStatusChecks
```

## Gateway actual request sequence

最终 watcher 新增请求按顺序为：

```text
POST /api/graphql
GET  /api/v3/user
GET  /api/v3/repos/gateway/bar/issues/1/comments?per_page=100&page=1
GET  /api/v3/repos/gateway/bar/pulls/1/comments?per_page=100&page=1
GET  /api/v3/repos/gateway/bar/pulls/1/reviews?per_page=100&page=1
POST /api/graphql
POST /api/graphql
POST /api/graphql
GET  /api/v3/repos/gateway/bar/actions/runs?head_sha=...&per_page=100 -> 404
```

两个 checks feature-detection POST 并发，到达顺序不作为契约。Caddy 日志确认没有随后出现 `/jobs`、`/logs`、`/rerun` 或 mutation。

## babysit-pr --once result

使用未修改的 pinned watcher、真实 gh 2.100.0、真实 Gateway 与真实 Gitea。P1-A 数据全部读取成功后，watcher 无条件进入 Actions：

```text
gh_pr_watch.py error: GitHub CLI command failed:
gh api repos/gateway/bar/actions/runs -X GET -f head_sha=... -f per_page=100
stdout: 404 page not found
stderr: gh: HTTP 404
```

没有通过 mock、脚本修改或假 Actions 响应绕过该行为。

## Final verdict

`gh-gateway` 对当前调查确定的 P1-A API profile 判定为 **PASS**：PR metadata、用户、三类反馈和 checks 均由真实 gh 2.95.0/2.100.0 验证。

当前 upstream watcher 的完整 `--once` 判定为 **PARTIAL**，原因仅是它在完成 P1-A 后强制请求明确属于 P1-B 的 Actions runs。Gateway 正确保持 scope，没有实现 Actions、jobs、logs、rerun 或 mutation。本结论不是完整 GitHub compatibility 声明。
