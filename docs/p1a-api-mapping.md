# P1-A API Mapping：Codex `babysit-pr` monitoring

本文只调查 Codex `babysit-pr` 在 PR monitoring 场景中读取的以下六类能力：

- PR metadata
- PR checks
- PR conversation comments
- PR inline review comments
- PR reviews
- current authenticated user

本文不设计或实现 Gateway 代码，也不覆盖 Actions runs、Actions jobs、job logs、`gh run rerun`、PR create、merge、review mutation 或 comment mutation。这些属于 P1-B 或后续范围。

## 1. 证据基线与结论口径

### 1.1 源码快照

| 组件 | 调查版本 | commit | 说明 |
| --- | --- | --- | --- |
| Codex `babysit-pr` | upstream `main`（2026-09-15） | [`b0af519c39766c173191fc39b341808619b51c74`](https://github.com/openai/codex/tree/b0af519c39766c173191fc39b341808619b51c74) | 当前 watcher 实现来自 [`gh_pr_watch.py`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py)。该文件不在此前 P0 E2E 使用的 `rust-v0.154.0` tag 中，因此 P1-A 是面向当前 upstream skill 的新兼容目标。 |
| GitHub CLI | `v2.100.0` | [`45437bc7eeeb3359bbfddd1742f79de7652fd3e2`](https://github.com/cli/cli/tree/45437bc7eeeb3359bbfddd1742f79de7652fd3e2) | 截至调查日的最新 release。与现有 Docker 固定的 `v2.95.0` 对比后，本文涉及的 PR finder、checks operation、feature detection 和 checks 聚合协议没有行为变化。 |
| 已有 Docker gh 基线 | `v2.95.0` | [`70bb306bd25eb407f90eabefd98824aed62cf519`](https://github.com/cli/cli/tree/70bb306bd25eb407f90eabefd98824aed62cf519) | 后续实现应继续回归该固定版本，并新增或升级测试时显式说明版本。 |
| Gitea | `v1.25.5` | [`f913d90ab664c1deccdfbe8c563abb13e897d62b`](https://github.com/go-gitea/gitea/tree/f913d90ab664c1deccdfbe8c563abb13e897d62b) | `v1.25.5` 是 annotated tag；这里记录其 peeled commit。 |

GitHub CLI 2.100.0 对 Enterprise 风格 host 仍使用：

```text
GraphQL: https://HOST/api/graphql
REST:    https://HOST/api/v3/...
```

来源：[`internal/ghinstance/host.go`](https://github.com/cli/cli/blob/v2.100.0/internal/ghinstance/host.go) 和 [`pkg/cmd/api/http.go`](https://github.com/cli/cli/blob/v2.100.0/pkg/cmd/api/http.go)。

### 1.2 Compatibility 标签

| 标签 | 本文定义 |
| --- | --- |
| **Direct** | Gitea 有同等读取 API，Gateway 只需改路径、参数名或字段名，不需要推断业务语义。 |
| **Emulated** | Gitea 提供了足够的基础数据，但 Gateway 必须组合多个请求、实现 GraphQL envelope、分页或聚合。 |
| **Approximate** | 可以提供对 watcher 有用的近似值，但 Gitea 公共 API 不足以完整复现 GitHub 语义。 |
| **Unsupported** | Gitea 1.25 公共 API 没有足够信息，不能诚实地给出等价值。可返回兼容的空值或保守值，但不能声称语义等价。 |

### 1.3 Watcher 的真实调用顺序

一次 snapshot 的 P1-A 子集按以下顺序执行：

1. `gh pr view ... --json ...`
2. `gh api user`
3. 分页读取 issue comments、inline review comments、reviews
4. `gh pr checks <resolved-number> --json ...`

来源：[`collect_snapshot`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L739-L767)。后续 Actions 查询不在本文范围。

`gh_text` 只对非 `api` 命令插入 `-R OWNER/REPO`；所有 `gh api` 调用的 repo 已编码在 endpoint 中。来源：[`gh_text`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L113-L139)。

## 2. 总览表

| Codex/gh behavior | GitHub API | Gitea API | Compatibility | Main gap |
| --- | --- | --- | --- | --- |
| `gh pr view`，auto/current branch | `POST /api/graphql`，`PullRequestForBranch` | `GET /api/v1/repos/{owner}/{repo}` + `GET /api/v1/repos/{owner}/{repo}/pulls?state=all&page=N&limit=...` | **Emulated**，部分字段 **Approximate/Unsupported** | `mergeStateStatus`、`reviewDecision` 无直接等价；`mergeable=false` 不能区分冲突与尚未算完。 |
| `gh pr view PR_NUMBER_OR_URL` | `POST /api/graphql`，`PullRequestByNumber` | `GET /api/v1/repos/{owner}/{repo}/pulls/{index}` | **Emulated**，部分字段 **Approximate/Unsupported** | 需要支持按 number dispatch；metadata 语义缺口同上。 |
| `gh pr checks PR_NUMBER` 的 PR lookup | `POST /api/graphql`，`PullRequestByNumber` | `GET /api/v1/repos/{owner}/{repo}/pulls/{index}` | **Emulated** | 必须返回可在随后 `node(id:)` 中重新解析的 PR GraphQL ID。 |
| `gh pr checks` feature detection | 两个并发 `POST /api/graphql`：`PullRequest_fields`、`PullRequest_fields2` | 无；Gateway 自己声明兼容 schema capabilities | **Emulated** | 若不支持 introspection，真实 gh 在主 checks query 之前即失败。 |
| `gh pr checks` check rollup | `POST /api/graphql`，`PullRequestStatusChecks`，contexts cursor 分页 | `GET /api/v1/repos/{owner}/{repo}/commits/{headSha}/statuses?page=N&limit=...` | **Approximate** | Gitea commit status 不是 GitHub CheckRun；缺少 workflow、event、独立 started/completed、丰富 conclusion 和精确 required-check 语义。 |
| `gh api user` | `GET /api/v3/user` | `GET /api/v1/user` | **Direct** | watcher 只消费 `login`；认证与错误 envelope 仍需 GitHub-compatible。 |
| conversation comments | `GET /api/v3/repos/{owner}/{repo}/issues/{n}/comments?per_page=100&page=P` | `GET /api/v1/repos/{owner}/{repo}/issues/{n}/comments` | **Emulated**，`author_association` **Approximate** | Gitea 1.25 handler 不应用 `page/limit`；Gateway 必须本地切页，且 DTO 没有 `author_association`。 |
| inline review comments | `GET /api/v3/repos/{owner}/{repo}/pulls/{n}/comments?per_page=100&page=P` | 先列 reviews，再逐个 `GET /api/v1/repos/{owner}/{repo}/pulls/{n}/reviews/{reviewId}/comments` | **Emulated/Approximate** | Gitea 没有 PR 级聚合 endpoint；line 字段命名/语义不同，且没有 `author_association`。 |
| reviews | `GET /api/v3/repos/{owner}/{repo}/pulls/{n}/reviews?per_page=100&page=P` | `GET /api/v1/repos/{owner}/{repo}/pulls/{n}/reviews?page=P&limit=100` | **Emulated/Approximate** | review state 名称不同；`author_association` 缺失；GitHub review decision 规则不能由列表完全还原。 |

## 3. PR metadata：`gh pr view`

### 3.1 Watcher command 与字段消费

当前 watcher 构造：

```bash
gh [-R OWNER/REPO] pr view [PR] --json \
  number,url,state,mergedAt,closedAt,headRefName,headRefOid,\
headRepository,headRepositoryOwner,mergeable,mergeStateStatus,reviewDecision
```

字段表定义在 [`pr_view_fields`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L153-L170)，实际消费如下：

| 字段 | watcher 用途 |
| --- | --- |
| `number` | 必需；转为整数，后续 comments、reviews、checks 都使用它。 |
| `url` | snapshot 输出；优先从 URL 解析 `OWNER/REPO`。 |
| `state` | 判断 CLOSED，并进入 snapshot/change key。 |
| `mergedAt` | 非空即认为 merged。 |
| `closedAt` | 非空即认为 closed。 |
| `headRefName` | snapshot 中的 head branch。 |
| `headRefOid` | 必需的 head SHA；用于后续 CI 查询、retry state 和 change detection。 |
| `headRepository` | 当 URL 不能确定 repo 时，从 `name` 或 `nameWithOwner` fallback。 |
| `headRepositoryOwner` | repo fallback 使用 `login` 或 `name`。 |
| `mergeable` | 只有精确字符串 `MERGEABLE` 才可能判定 ready。 |
| `mergeStateStatus` | `BLOCKED`、`DIRTY`、`DRAFT`、`UNKNOWN` 会阻止 ready。 |
| `reviewDecision` | `REVIEW_REQUIRED`、`CHANGES_REQUESTED` 会阻止 ready。 |

消费逻辑见 [`resolve_pr`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L164-L210) 与 [`is_pr_ready_to_merge`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L680-L695)。

### 3.2 GraphQL operation 与 variables

#### auto/current branch

未给 PR selector 时，gh finder 使用：

```graphql
query PullRequestForBranch(
  $owner: String!
  $repo: String!
  $headRefName: String!
  $states: [PullRequestState!]
) {
  repository(owner: $owner, name: $repo) {
    pullRequests(
      headRefName: $headRefName
      states: $states
      first: 30
      orderBy: {field: CREATED_AT, direction: DESC}
    ) {
      nodes {
        number
        url
        state
        mergedAt
        closedAt
        headRefName
        headRefOid
        headRepository { id name nameWithOwner }
        headRepositoryOwner { id login ... on User { name } }
        mergeable
        mergeStateStatus
        reviewDecision
        id
        baseRefName
        isCrossRepository
      }
    }
    defaultBranchRef { name }
  }
}
```

字段顺序由 set/query builder 决定，不应作为协议条件；字段集合才是兼容要求。finder 额外要求 `id`、`baseRefName`、`isCrossRepository`、`headRepositoryOwner` 和 `defaultBranchRef.name`。来源：[`findForRefs`](https://github.com/cli/cli/blob/v2.100.0/pkg/cmd/pr/shared/finder.go#L386-L441) 与 [`PullRequestGraphQL`](https://github.com/cli/cli/blob/v2.100.0/api/query_builder.go#L384-L470)。

variables：

```json
{
  "owner": "OWNER",
  "repo": "REPO",
  "headRefName": "feature",
  "states": null
}
```

#### explicit PR number or PR URL

数字 selector 或可解析的 PR URL 使用：

```graphql
query PullRequestByNumber($owner: String!, $repo: String!, $pr_number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $pr_number) {
      number
      url
      state
      mergedAt
      closedAt
      headRefName
      headRefOid
      headRepository { id name nameWithOwner }
      headRepositoryOwner { id login ... on User { name } }
      mergeable
      mergeStateStatus
      reviewDecision
      id
    }
  }
}
```

variables：

```json
{"owner":"OWNER","repo":"REPO","pr_number":123}
```

来源：[`findByNumber`](https://github.com/cli/cli/blob/v2.100.0/pkg/cmd/pr/shared/finder.go#L356-L384)。URL 先被解析成 repo + number；branch selector 仍走 `PullRequestForBranch`。

### 3.3 Gitea 1.25 映射

number lookup 的首选上游：

```text
GET /api/v1/repos/{owner}/{repo}/pulls/{index}
```

branch lookup 可继续复用 P0 的 repository + paged PR list：

```text
GET /api/v1/repos/{owner}/{repo}
GET /api/v1/repos/{owner}/{repo}/pulls?state=all&page=N&limit=30
```

Gitea 的 [Get pull request](https://docs.gitea.com/api/1.25/operations/repo-get-pull-request/) DTO 和 [`modules/structs/pull.go`](https://github.com/go-gitea/gitea/blob/v1.25.5/modules/structs/pull.go) 可提供：

| GitHub GraphQL | Gitea | 结论 |
| --- | --- | --- |
| `number` | `number` | Direct field mapping。 |
| `url` | `html_url` | Direct field mapping；不能用 Gitea `url` API URL。 |
| `state` | `state` + `merged` | Emulated：`open -> OPEN`，`closed && merged -> MERGED`，否则 `CLOSED`。 |
| `mergedAt` | `merged_at` | Direct；未 merged 时为 null。 |
| `closedAt` | `closed_at` | Direct。 |
| `headRefName` | `head.label` | Direct for normal PR；deleted head repo/branch 是边界情况。 |
| `headRefOid` | `head.sha` | Direct。 |
| `headRepository` | `head.repo` | Emulated GraphQL shape；deleted fork 时可能为 null。ID 必须字符串化。 |
| `headRepositoryOwner` | `head.repo.owner` | Emulated GraphQL shape；user/org fragment 需区分，ID 必须字符串化。 |
| `mergeable` | `mergeable` bool | Approximate：`true -> MERGEABLE`；`false` 无法可靠区分 `CONFLICTING` 与 GitHub 的 `UNKNOWN`。Gitea GET 还会异步触发 PR mergeability check。 |
| `mergeStateStatus` | 无等价字段 | Unsupported exact semantics。可用 draft、mergeable、branch protection、statuses 做保守近似，但不能完整区分 `BEHIND/BLOCKED/CLEAN/DIRTY/HAS_HOOKS/UNSTABLE/UNKNOWN`。 |
| `reviewDecision` | 无等价字段 | Approximate：可聚合 reviews 与 protection policy，但 GitHub 的 required-review、dismissal、stale approval、CODEOWNERS 等规则无法完整复现。 |

对 watcher 来说，不能随意把未知值伪装成 `CLEAN` 或无 review requirement，否则会产生错误的 `ready_to_merge`。实现阶段应选择并测试保守策略。

## 4. PR checks：`gh pr checks`

### 4.1 Watcher command 与实际消费

watcher 在 `gh pr view` 已解析出 number 后固定执行：

```bash
gh -R OWNER/REPO pr checks PR_NUMBER --json \
  name,state,bucket,link,workflow,event,startedAt,completedAt
```

来源：[`get_pr_checks`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L276-L287) 和 [`collect_snapshot`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L759-L762)。

虽然命令请求八个 JSON 字段，watcher 当前只用：

- `bucket`：`pass` / `fail` / `pending` 分类与计数；
- `state`：当 bucket 不是 pending 时，再检查 `QUEUED/IN_PROGRESS/PENDING/WAITING/REQUESTED`。

`name`、`link`、`workflow`、`event`、`startedAt`、`completedAt` 被 gh 解析并输出，但 watcher 随后丢弃单项列表，只保留 passed/failed/pending counts。来源：[`summarize_checks`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L290-L313) 与 snapshot 构造。

### 4.2 一条命令实际触发的 GraphQL 请求

#### A. PR finder

因为 watcher 总是把解析出的 PR number 显式传给 `gh pr checks`，第一步是：

```graphql
query PullRequestByNumber($owner: String!, $repo: String!, $pr_number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $pr_number) {
      id
      number
      headRefName
    }
  }
}
```

variables：

```json
{"owner":"OWNER","repo":"REPO","pr_number":123}
```

#### B. feature detection（两个并发 query）

gh 2.100.0 在主查询前调用 `PullRequestFeatures()`，并发发送两个无 variables 的 introspection query：

```graphql
query PullRequest_fields {
  PullRequest: __type(name: "PullRequest") {
    fields(includeDeprecated: true) { name }
  }
  StatusCheckRollupContextConnection: __type(
    name: "StatusCheckRollupContextConnection"
  ) {
    fields(includeDeprecated: true) { name }
  }
}
```

```graphql
query PullRequest_fields2 {
  WorkflowRun: __type(name: "WorkflowRun") {
    fields(includeDeprecated: true) { name }
  }
}
```

gh 检查：

- `PullRequest.isInMergeQueue`；本命令结果不依赖它；
- context connection 是否有 `checkRunCount`；本命令的详细 contexts query 不依赖 count 字段；
- `WorkflowRun.event`；存在时主 query 才选择 `event`。

来源：[`PullRequestFeatures`](https://github.com/cli/cli/blob/v2.100.0/internal/featuredetection/feature_detection.go#L201-L267)。两个请求并发，Gateway 测试不能依赖它们的到达顺序。

#### C. checks rollup 与 cursor 分页

```graphql
query PullRequestStatusChecks($id: ID!, $endCursor: String) {
  node(id: $id) {
    ... on PullRequest {
      statusCheckRollup: commits(last: 1) {
        nodes {
          commit {
            statusCheckRollup {
              contexts(first: 100, after: $endCursor) {
                nodes {
                  __typename
                  ... on StatusContext {
                    context
                    state
                    targetUrl
                    createdAt
                    description
                    isRequired(pullRequestId: $id)
                  }
                  ... on CheckRun {
                    name
                    checkSuite {
                      workflowRun {
                        event
                        workflow { name }
                      }
                    }
                    status
                    conclusion
                    startedAt
                    completedAt
                    detailsUrl
                    isRequired(pullRequestId: $id)
                  }
                }
                pageInfo { hasNextPage endCursor }
              }
            }
          }
        }
      }
    }
  }
}
```

首个请求 variables：

```json
{"id":"GATEWAY_PR_ID"}
```

后续页增加：

```json
{"id":"GATEWAY_PR_ID","endCursor":"CURSOR"}
```

`contexts(first:100)`、operation 和分页逻辑来自 [`checks.go`](https://github.com/cli/cli/blob/v2.100.0/pkg/cmd/pr/checks/checks.go#L257-L310) 与 [`RequiredStatusCheckRollupGraphQL`](https://github.com/cli/cli/blob/v2.100.0/api/query_builder.go#L270-L312)。

### 4.3 gh 如何生成 JSON fields

Gateway 不直接返回 `bucket`。gh 对 union nodes 聚合后生成它：

- StatusContext：`context -> name`、`state -> state`、`targetUrl -> link`；
- CheckRun：完成时使用 `conclusion`，未完成时使用 `status`；`detailsUrl -> link`；
- `SUCCESS -> pass`；
- `SKIPPED/NEUTRAL -> skipping`；
- `ERROR/FAILURE/TIMED_OUT/ACTION_REQUIRED -> fail`；
- `CANCELLED -> cancel`；
- 其他状态 -> pending。

gh 还会按 context 或 `name/workflow/event` 去重。来源：[`aggregate.go`](https://github.com/cli/cli/blob/v2.100.0/pkg/cmd/pr/checks/aggregate.go)。

### 4.4 Gitea 1.25 映射与限制

可用上游：

```text
GET /api/v1/repos/{owner}/{repo}/pulls/{index}
GET /api/v1/repos/{owner}/{repo}/commits/{headSha}/statuses?page=N&limit=100
```

Gitea [commit statuses API](https://docs.gitea.com/api/1.25/operations/repo-list-statuses-by-ref/) 提供 `status`、`context`、`target_url`、`description`、`created_at`、`updated_at`。建议将普通 Gitea status 表示为 GraphQL `StatusContext`，并在 Gateway 侧先按 context 取最新记录，避免把同一 context 的历史状态全部暴露给 gh。

可直接/近似映射：

| GraphQL check field | Gitea status | 兼容性 |
| --- | --- | --- |
| `__typename` | 无 | Emulated，返回 `StatusContext`。 |
| `context` | `context` | Direct。 |
| `state` | `status` | Emulated：pending/success/error/failure 可转大写；Gitea `warning` 没有合法的 GitHub StatusState 等价，必须定义并测试保守策略。 |
| `targetUrl` | `target_url` | Direct。 |
| `createdAt` | `created_at` | Direct。 |
| `description` | `description` | Direct。 |
| `isRequired` | 无直接逐 status 字段 | Approximate；本次没有 `--required`，watcher 也不消费该值，可先为 false，但不能声称支持 required-check 语义。 |
| CheckRun `workflow/event` | 无 | Unsupported。作为 StatusContext 输出时 gh 会得到空字符串。 |
| CheckRun `startedAt/completedAt` | 只有 status created/updated | Approximate/Unsupported exact semantics。 |
| CheckRun rich status/conclusion | Gitea status 状态集更窄 | Approximate。 |

空 statuses 时仍应返回一个 commit node 和空 contexts，使 gh 生成其原生 “no checks reported” 行为；不要伪造成功 check。

## 5. Current authenticated user：`gh api user`

### 5.1 GitHub request 与消费字段

```http
GET /api/v3/user
```

无 query params。watcher 只要求响应是 object 且 `login` 非空，然后把它转为字符串。来源：[`get_authenticated_login`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L430-L436)。

### 5.2 Gitea mapping

```text
GET /api/v1/user
```

Gitea 1.25 [Get authenticated user](https://docs.gitea.com/api/1.25/operations/user-get-current/) 原生响应包含 `login`，因此该读取行为是 **Direct**。Gateway 仍需沿用固定 token 优先/否则转发 Authorization 的策略，并把 Gitea 401/403/5xx 转为稳定、不泄漏上游 body 的 GitHub REST error。

## 6. PR conversation comments

### 6.1 GitHub request

watcher 手工分页：

```http
GET /api/v3/repos/{owner}/{repo}/issues/{pr_number}/comments?per_page=100&page=PAGE
```

它不读取 `Link` header，而是当响应数组长度小于 100 时停止；因此 Gateway 必须严格返回请求页，不能在每一页重复全量结果。来源：[`gh_api_list_paginated`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L447-L462)。

watcher 消费：

```text
id
user.login
author_association
created_at
body
html_url
```

并规范化成 `issue_comment`。来源：[`normalize_issue_comments`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L465-L483)。

### 6.2 Gitea mapping

```text
GET /api/v1/repos/{owner}/{repo}/issues/{index}/comments
```

Gitea [issue comments API](https://docs.gitea.com/api/1.25/operations/issue-get-comments/) 提供 `id`、`user.login`、`created_at`、`body`、`html_url`，这些字段可直接映射。

两个关键差异：

1. Gitea 1.25 的 `ListIssueComments` handler 没有应用 `page/limit`，而是返回全部普通 comments。源码见 [`issue_comment.go`](https://github.com/go-gitea/gitea/blob/v1.25.5/routers/api/v1/repo/issue_comment.go#L26-L120)。Gateway 应获取全量、稳定排序，然后按 GitHub `page/per_page` 本地切片。
2. Gitea Comment DTO 没有 `author_association`。watcher 会忽略既不是允许的 bot、也不是当前认证用户、且 association 不在 `OWNER/MEMBER/COLLABORATOR` 的人类评论。若 Gateway 不补该字段，很多真实 reviewer comment 会被静默过滤。

`author_association` 只能 **Approximate**：可结合 repo owner、组织成员与 `GET /api/v1/repos/{owner}/{repo}/collaborators/{login}/permission` 推断 OWNER/MEMBER/COLLABORATOR，但 Gitea 权限模型、查询权限和 GitHub “评论创建时的关联”语义并不完全等价。

## 7. PR inline review comments

### 7.1 GitHub request 与消费字段

```http
GET /api/v3/repos/{owner}/{repo}/pulls/{pr_number}/comments?per_page=100&page=PAGE
```

watcher 消费：

```text
id
pull_request_review_id
user.login
author_association
created_at
body
path
line
original_line
html_url
```

`line` 为空时 fallback 到 `original_line`。它还用 `pull_request_review_id` 对照 reviews，过滤属于 `PENDING` review 的 unpublished comments。来源：[`normalize_review_comments`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L486-L510) 与 pending review 处理逻辑。

### 7.2 Gitea mapping

Gitea 1.25 没有等价的 “list all review comments for PR” endpoint。它只有：

```text
GET /api/v1/repos/{owner}/{repo}/pulls/{index}/reviews?page=N&limit=...
GET /api/v1/repos/{owner}/{repo}/pulls/{index}/reviews/{reviewId}/comments
```

官方文档：[List reviews](https://docs.gitea.com/api/1.25/operations/repo-list-pull-reviews/) 与 [Get review comments](https://docs.gitea.com/api/1.25/operations/repo-get-pull-review-comments/)。所以 Gateway 必须：

1. 遍历全部 reviews；
2. 对每个 review 获取 comments；
3. 聚合并按稳定顺序排序；
4. 对聚合结果应用 GitHub `per_page/page`；
5. 输出 JSON array。

字段映射：

| GitHub field | Gitea PullReviewComment | 兼容性 |
| --- | --- | --- |
| `id` | `id` | Direct。 |
| `pull_request_review_id` | `pull_request_review_id` | Direct。 |
| `user.login` | `user.login` | Direct。 |
| `created_at` | `created_at` | Direct。 |
| `body` | `body` | Direct。 |
| `path` | `path` | Direct。 |
| `line` | `position` | Approximate；JSON 名和 GitHub modern line semantics 不同。 |
| `original_line` | `original_position` | Approximate；同上。 |
| `html_url` | `html_url` | Direct。 |
| `author_association` | 无 | Approximate，策略同 conversation comments。 |

该 endpoint 整体属于 **Emulated/Approximate**。N+1 请求是 Gitea 公共 API 形状造成的明确成本；本阶段不引入缓存设计。

## 8. PR reviews

### 8.1 GitHub request 与消费字段

```http
GET /api/v3/repos/{owner}/{repo}/pulls/{pr_number}/reviews?per_page=100&page=PAGE
```

watcher 消费：

```text
id
user.login
author_association
state
submitted_at (fallback: created_at)
body
html_url
```

`state == PENDING` 的 review 不会作为 published review item 输出，但其 ID 会用于过滤 pending inline comments。来源：[`normalize_reviews`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L513-L535)。

### 8.2 Gitea mapping

```text
GET /api/v1/repos/{owner}/{repo}/pulls/{index}/reviews?page=PAGE&limit=100
```

Gitea 原生支持 `page/limit` 并返回 total count。路径和分页见 [Gitea review API](https://docs.gitea.com/api/1.25/operations/repo-list-pull-reviews/) 与 [`pull_review.go`](https://github.com/go-gitea/gitea/blob/v1.25.5/routers/api/v1/repo/pull_review.go#L26-L109)。Gateway 需要把 `per_page` 翻译为 `limit`。

状态映射：

| Gitea | GitHub REST review state | 说明 |
| --- | --- | --- |
| `APPROVED` | `APPROVED` | Direct。 |
| `PENDING` | `PENDING` | Direct；watcher 用于 unpublished filtering。 |
| `COMMENT` | `COMMENTED` | Emulated rename。 |
| `REQUEST_CHANGES` | `CHANGES_REQUESTED` | Emulated rename。 |
| `dismissed=true` | `DISMISSED` | Emulated；应优先于原 state。 |
| `REQUEST_REVIEW` | 无等价 submitted review | 不应伪装成已发布 review；建议过滤或另行保守处理。 |

Gitea DTO 提供 `id/user/body/submitted_at/html_url`，但没有 `author_association`。因此整个 API 可用但仍是 **Emulated/Approximate**。

## 9. Review item filtering 对兼容实现的影响

watcher 并不会展示所有 API 返回项：

- bot login 必须以 `[bot]` 结尾，且当前只允许 login 包含 `codex`；
- 当前认证用户自己的评论始终可信；
- 其他人类作者只有 `author_association` 为 `OWNER`、`MEMBER` 或 `COLLABORATOR` 时才可信；
- pending reviews 和其 inline comments 被隐藏；
- 已见 ID 存入本地 state，下一轮不重复输出；
- 最终按 `created_at/kind/id` 排序。

来源：[`fetch_new_review_items`](https://github.com/openai/codex/blob/b0af519c39766c173191fc39b341808619b51c74/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L565-L650)。

因此 `author_association` 不是装饰字段，而是 P1-A review monitoring 能否看见人类反馈的关键输入。实现时不能简单省略。

## 10. Error、认证与 pagination 协议要求

这些命令沿用当前 host 的 Authorization；Gateway 固定 token 优先、否则转发客户端 Authorization 的现有策略可以继续复用。

gh 还会携带其标准 User-Agent 和 API version headers；Gateway 不应把 `Accept`、`User-Agent` 或 `X-GitHub-Api-Version` 当作额外业务分支。本文列出的 `gh api` 命令没有显式覆盖 `Accept`，应按普通 GitHub JSON REST 请求处理。

建议保持既有错误边界：

- GraphQL 请求：HTTP 200 + `data`/`errors`，不泄漏 Gitea body；非法 JSON 才是 HTTP 400。
- GitHub REST compatibility 请求：使用匹配的 404/403/502 和稳定 `{"message":"..."}`，不泄漏 Gitea body。
- comments/reviews 正常空结果：HTTP 200 + `[]`。
- `gh_api_list_paginated` 以短页终止，因此三类 feed 的每页数组长度必须真实反映该页，不能依赖 Link header 替代。
- GraphQL checks contexts 使用 `pageInfo.hasNextPage/endCursor`，必须避免 cursor 不前进造成 gh 无限循环。

## 11. 静态调查是否需要 `GH_DEBUG=api`

本次没有执行 `GH_DEBUG=api`。原因是：

- watcher 的 argv、字段读取和手工分页逻辑在 Python 源码中是确定的；
- gh 2.100.0 的 finder、feature detection、checks query builder、variables 与分页循环均有直接源码；
- 2.95.0 到 2.100.0 的相关协议对比没有行为变化。

后续 TDD E2E 仍应启用 Gateway/Caddy access log，验证真实请求序列，尤其是两个并发 introspection query 与多页 `PullRequestStatusChecks`。

## 12. 推荐的 TDD 实现顺序

### Slice 1：authenticated user

先实现：

```text
GET /api/v3/user -> GET /api/v1/user
```

这是最小 Direct slice，可先固化 REST auth、error sanitization 与 presenter 约定。

### Slice 2：expanded PR metadata

1. 新增 `PullRequestByNumber` operation。
2. 扩展现有 `PullRequestForBranch` 的允许字段，但不改变 P0 query 行为。
3. 映射 direct metadata。
4. 对 `mergeable`、`mergeStateStatus`、`reviewDecision` 先写明确的保守语义测试，再实现；不要用空值偶然让 watcher 判定 ready。
5. 覆盖 number、URL、auto branch、fork/deleted head 边界与 GraphQL ID round-trip。

### Slice 3：conversation comments

实现 GitHub REST route、全量 Gitea comments 的本地稳定分页、字段 presenter 和 `author_association` 策略。重点回归 `>=100` 条时第 2 页不会重复第 1 页，也不会让 watcher 无限分页。

### Slice 4：reviews

实现 `per_page -> limit`、state rename、dismissed、pending、空数组和 association。这个 slice 为 inline comment 的 pending filtering 提供 review ID/state 基础。

### Slice 5：inline review comments

实现 list reviews + per-review comments 聚合、本地分页、去重、line/original_line 映射和 association。测试 N+1 上游错误不得产生部分成功或泄漏内部 body。

### Slice 6：PR checks protocol

按真实 gh 子阶段逐层 TDD：

1. checks 使用的 `PullRequestByNumber` lookup 与 PR ID round-trip；
2. `PullRequest_fields` 与 `PullRequest_fields2` introspection，允许并发和任意到达顺序；
3. `PullRequestStatusChecks` 首屏空/单页；
4. GraphQL cursor 多页；
5. Gitea statuses 多页、按 context 取最新；
6. pending/success/error/failure 与 `warning` 策略；
7. 真实 gh 2.95.0 回归，再用 gh 2.100.0 做协议验证；
8. 最后用当前 `babysit-pr --once` 做 P1-A real Codex/gh/Gateway/Gitea E2E。

checks 放在最后，是因为它同时依赖 PR ID、introspection、union presenter、两套分页和近似状态语义；先完成前五个 slice 能更快建立可独立验证的 monitoring 数据面。

## 13. P1-A 范围判定

P1-A 需要新增或扩展的外部 API surface 只有：

```text
POST /api/graphql
  - PullRequestForBranch（扩展字段）
  - PullRequestByNumber
  - PullRequest_fields
  - PullRequest_fields2
  - PullRequestStatusChecks

GET /api/v3/user
GET /api/v3/repos/{owner}/{repo}/issues/{pr}/comments
GET /api/v3/repos/{owner}/{repo}/pulls/{pr}/comments
GET /api/v3/repos/{owner}/{repo}/pulls/{pr}/reviews
```

没有证据要求本阶段实现 Actions、jobs、logs、rerun 或任何 mutation。它们明确不属于本文和 P1-A。
