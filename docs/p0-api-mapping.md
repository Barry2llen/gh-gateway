# P0 API Mapping

本文只覆盖 Codex TUI 已确认的三条 P0 调用，不覆盖 checks、Actions、review、create、merge 或其他 `gh` 命令。目标是记录网关必须兼容的请求和响应行为，不实现网关代码。

## 0. 证据基线

- `cli/cli` 源码快照：[`38316c1c4f275030e3df6666382922e75410d68b`](https://github.com/cli/cli/tree/38316c1c4f275030e3df6666382922e75410d68b)。其 `go.mod` 使用 `github.com/cli/go-gh/v2 v2.16.0`。
- Gitea 源码快照：[`7b036e96c2e0d88c9cb582001783f8f96ab06be2`](https://github.com/go-gitea/gitea/tree/7b036e96c2e0d88c9cb582001783f8f96ab06be2)。API 文档基于当前的 [Gitea API 1.25 文档](https://docs.gitea.com/api/1.25/)。
- 上一阶段 Codex 调用调查：附件 [`Codex-gh-Usage-Investigation.md`](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs)。该文件确认 TUI 的实际顺序为：当前分支 `gh pr view`，失败后 `git rev-parse HEAD`，然后 `gh repo view`，最后按 `parent -> current` 查询 commit-to-PR。
- 本环境没有 `gh` 可执行文件，因此没有执行 `GH_DEBUG=api` 动态抓包。对这三条调用，当前 `cli/cli` 源码已经确定了协议、operation、变量、路径和字段；动态验证不是必要证据。凡是 `gh` 源码本身不能确定的语义，本文明确标为 `Unknown`。

`cli/cli` 的 GraphQL 请求体由 `api.Client.GraphQL` 委托给 `go-gh` 生成；当前测试确认其形状为 `{"query":"...","variables":{...}}`，见 [`api/client_test.go#L23-L46`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/client_test.go#L23-L46)。

## 1. gh pr view

### gh behavior

#### command

Codex TUI 实际执行的是：

```bash
gh pr view --json number,url,state
```

不带 number、URL、branch 或 `--repo` 参数；`gh pr view` 不带 selector 时显示当前 checkout 所在 branch 的 PR。`view.go` 的命令定义和 JSON flag 在 [`pkg/cmd/pr/view/view.go#L40-L85`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/view/view.go#L40-L85)，TUI 调用位置在上一阶段调查的 [`branch_summary.rs#L350-L362`](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs#L350-L362)。

`--json` 的值由 pflag 的 slice 原样保存，因此本次 exporter 字段顺序是 `number`, `url`, `state`；`viewRun` 将这组字段传入 PR finder，见 [`view.go#L97-L108`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/view/view.go#L97-L108) 和 [`json_flags.go#L73-L98`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmdutil/json_flags.go#L73-L98)。

#### GitHub protocol

这是 GraphQL，不是 GitHub REST。当前 branch lookup 走 `findForRefs`，通过 `client.GraphQL` 发送 [`finder.go#L386-L423`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/finder.go#L386-L423)。对 `github.com`，GraphQL endpoint 是：

```text
POST https://api.github.com/graphql
```

host 到 endpoint 的解析规则在 [`internal/ghinstance/host.go#L46-L56`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/internal/ghinstance/host.go#L46-L56)；`api.Client.GraphQL` 使用的 `go-gh` 版本和调用位置见 [`go.mod#L21-L21`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/go.mod#L21) 与 [`api/client.go#L106-L118`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/client.go#L106-L118)。

#### exact request / GraphQL operation

`findForRefs` 根据请求字段建立 set，并额外添加 finder 自己用于匹配的字段：`id`, `number`, `state`, `baseRefName`, `headRefName`, `isCrossRepository`, `headRepositoryOwner`。因为 `number`、`state` 已在用户的 `--json` 列表中，实际选择片段的顺序为：

```graphql
query PullRequestForBranch($owner: String!, $repo: String!, $headRefName: String!, $states: [PullRequestState!]) {
  repository(owner: $owner, name: $repo) {
    pullRequests(headRefName: $headRefName, states: $states, first: 30, orderBy: { field: CREATED_AT, direction: DESC }) {
      nodes {number,url,state,id,baseRefName,headRefName,isCrossRepository,headRepositoryOwner{id,login,...on User{name}}}
    }
    defaultBranchRef { name }
  }
}
```

operation 名、查询参数、`first: 30`、创建时间倒序和 `defaultBranchRef` 都直接来自 [`finder.go#L398-L411`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/finder.go#L398-L411)。字段片段构造规则来自 [`query_builder.go#L384-L474`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/query_builder.go#L384-L474)。

对本次调用，variables 的值为：

```json
{
  "owner": "BASE_OWNER",
  "repo": "BASE_REPO",
  "headRefName": "UNQUALIFIED_HEAD_BRANCH",
  "states": null
}
```

`owner` 和 `repo` 来自 base repository；`headRefName` 是解析后的未限定 branch 名；`gh pr view` 没有把 `States` 设置为非空，所以变量键仍存在但 JSON 值是 `null`。这三项在 [`finder.go#L413-L418`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/finder.go#L413-L418)。

GraphQL 请求使用当前 `cli/cli` HTTP client 的默认头：

- `X-GitHub-Api-Version: 2022-11-28`；
- `GraphQL-Features: merge_queue`，由 [`api/client.go#L108-L117`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/client.go#L108-L117) 设置；
- `User-Agent: GitHub CLI <gh-version>`；
- 配置有 token 时由 transport 加 `Authorization: token <TOKEN>`，见 [`api/http_client.go#L35-L79`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/http_client.go#L35-L79) 和 [`api/http_client.go#L153-L188`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/http_client.go#L153-L188)。

#### required response fields

`gh` 最终输出的三个 JSON 字段与 GraphQL 字段是一一对应关系，不是 REST 字段重命名：

| `--json` field | GraphQL field / type | 本次调用中的用途 |
| --- | --- | --- |
| `number` | `PullRequest.number` / `Int` | exporter 输出 PR number；finder 也把它预取到结构体。 |
| `url` | `PullRequest.url` / `URI` | exporter 输出 PR web URL。这里是网页 URL，不是 REST API URL。 |
| `state` | `PullRequest.state` / `PullRequestState` | exporter 输出原始枚举字符串；finder 只把 `OPEN` 视为当前可用 PR。当前测试同时覆盖 `OPEN`、`CLOSED`、`MERGED`，见 [`finder_test.go#L440-L600`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/finder_test.go#L440-L600)。 |

为了得到这三个字段，当前 `gh` 还要求下列额外响应字段：

- `id`：finder 为后续可能的 preload 查询强制加入；这条 P0 没有 reviews/comments 等 preload，但查询仍包含该字段。
- `baseRefName`、`headRefName`、`isCrossRepository`、`headRepositoryOwner.login`：用于 `PRFindRefs.Matches`。跨仓库时 `HeadLabel()` 组成 `<head-owner>:<head-branch>`，见 [`queries_pr.go#L309-L314`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/queries_pr.go#L309-L314) 和 [`find_refs_resolution.go#L104-L110`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/find_refs_resolution.go#L104-L110)。`headRepositoryOwner.id` 与 inline `User.name` 也被选择并解码，但本次匹配只使用 `login`。
- `defaultBranchRef.name`：当 head 是默认 branch 时，finder 不接受 closed/merged PR；逻辑在 [`finder.go#L427-L441`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/finder.go#L427-L441)。

服务端先按 `CREATED_AT DESC` 返回最多 30 个节点；随后 `gh` 在客户端把 `OPEN` 节点稳定地排到其他状态之前，再按 base/head ref 匹配并取第一个。查询本身没有 `baseRefName` 参数，本次 `BaseBranch` 为空，所以可能返回多个 base branch 的候选。这里的行为全部在 [`finder.go#L403-L441`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b) 中实现。

#### 当前仓库、branch、remote、host 解析

`pr` 命令在 root 中使用 `SmartBaseRepoFunc`，不是简单取固定 `origin`，见 [`root.go#L162-L179`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/root/root.go#L162-L179)。实际过程是：

1. 读取 `git remote -v` 和 `remote.*.gh-resolved`；remote URL 经过 SSH translator，解析为 host/owner/repo，并过滤到已认证或默认 GitHub host。代码在 [`git/client.go#L165-L193`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/git/client.go#L165-L193)、[`remote_resolver.go#L28-L95`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/factory/remote_resolver.go#L28-L95) 和 [`context/remote.go#L101-L123`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/context/remote.go#L101-L123)。
2. remotes 按 `upstream > github > origin > 其他名称` 排序。已设置 `remote.<name>.gh-resolved=base` 或具体 `OWNER/REPO` 时优先使用该值；无 prompt 能力时使用排序后的第一个 remote，见 [`context/context.go#L61-L108`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/context/context.go#L61-L108)。
3. 读取当前 branch：`git symbolic-ref --quiet HEAD`，去掉 `refs/heads/`，见 [`git/client.go#L222-L240`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/git/client.go#L222-L240)。
4. 根据 branch config 解析 head repo 和远端 branch。优先 `branch.<name>@{push}`，失败后按 `branch.<name>.pushRemote`、`remote.pushDefault`、`branch.<name>.remote`；`push.default=upstream/tracking` 时 branch 名可以来自 merge ref，见 [`find_refs_resolution.go#L298-L394`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/find_refs_resolution.go#L298-L394)。同仓库 head 使用未限定 branch；跨仓库 head 使用 `<owner>:<branch>`，见 [`find_refs_resolution.go#L132-L170`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/find_refs_resolution.go#L132-L170)。
5. Codex TUI 的 subprocess wrapper 设置 `GH_PROMPT_DISABLED=1` 和 `GIT_TERMINAL_PROMPT=0`，所以 P0 非交互路径应落入“无法 prompt，使用第一个已解析 remote”的分支；这两个环境变量和调用顺序来自上一阶段附件 [`branch_summary.rs#L350-L423`](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs#L350-L423)。若单独在可交互终端运行 `gh`，`SmartBaseRepoFunc` 可能额外执行一次 `query RepositoryNetwork` 来消歧；该条件请求不属于本阶段已观察的三条 TUI P0 请求，具体是否在某个 Codex runtime 中触发为 `Unknown`。查询构造在 [`queries_repo.go#L476-L517`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/queries_repo.go#L476-L517)。

**PR-ref edge case（不计入本次观察到的 TUI minimum）**：如果当前 branch 的 merge ref 是 `refs/pull/<N>/head`，finder 会改走 `query PullRequestByNumber`，而不是上面的 branch query，见 [`finder.go#L145-L150`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/finder.go#L145-L150) 和 [`finder.go#L356-L383`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/pr/shared/finder.go#L356-L383)。如果网关的承诺范围是任意环境下的泛化 `gh pr view`，还需要对应 GraphQL operation；如果承诺范围是附件确认的 Codex TUI 普通 branch 识别，则不计入 P0 minimum。

### Gitea mapping

#### endpoint(s)

Gitea 没有与 GitHub GraphQL `repository(...).pullRequests(headRefName: ...)` 等价的原生 GraphQL API。最接近的原生能力是：

```text
GET /api/v1/repos/{owner}/{repo}/pulls
```

当前 Gitea handler 的 Swagger 注释和参数在 [`routers/api/v1/repo/pull.go#L48-L159`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/routers/api/v1/repo/pull.go#L48-L159)，官方文档为 [`repo-list-pull-requests`](https://docs.gitea.com/api/1.25/operations/repo-list-pull-requests/)。可用参数包括：

- path：`owner`、`repo`；
- query：`state=open|closed|all`，本场景应使用 `state=all`；
- query：`page`、`limit`；
- query：`sort`，支持 `oldest`、`recentupdate` 等；
- query：`base_branch`，本次 `gh pr view` 没有发送 base branch，因此不是必需参数。

Gitea 当前列表查询在没有 `sort` 时按创建时间倒序，并以 issue id 作稳定次序；实现见 [`pull_list.go#L169-L187`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/models/issues/pull_list.go#L169-L187) 与 [`issue_search.go#L72-L129`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/models/issues/issue_search.go#L72-L129)。

因此外部网关需要暴露 GitHub-compatible 的 GraphQL endpoint，并在内部调用上述 Gitea REST endpoint：

```text
POST /graphql                         # 网关提供；Gitea 原生没有该查询
```

#### required request parameters

网关 GraphQL facade 至少要接受 `PullRequestForBranch` 的四个 variables：`owner`、`repo`、`headRefName`、`states`。它应把 `states=null` 解释为不限制 open/closed/merged，并把 Gitea REST 请求至少构造成：

```text
GET /api/v1/repos/{owner}/{repo}/pulls?state=all&page=1&limit=30
```

因为 Gitea 列表 API 没有当前 handler 文档化的 `headRefName` 过滤参数，网关不能只取第一页 30 条后就认为完成：应继续翻页直到收集到足够的 branch 匹配项，或在 Gitea 内部直接按 base repo/head branch 查询。前者是额外 REST 请求，后者不是现成的公共 API，属于网关实现选择。

#### response fields and transformation rules

Gitea 的 PR 结构和分支结构在 [`modules/structs/pull.go#L10-L110`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/modules/structs/pull.go#L10-L110)，列表转换在 [`services/convert/pull.go#L269-L480`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/services/convert/pull.go#L269-L480)。给 GraphQL facade 的映射应为：

| GraphQL 字段 | Gitea 来源 | 规则 |
| --- | --- | --- |
| `id` | Gitea PR `id` | 转为稳定的 GraphQL `ID` 字符串。当前 P0 不消费该值，但 query 明确选择了它。 |
| `number` | `number` | 直接映射为整数。 |
| `url` | `html_url` | 必须使用 PR 网页 URL；Gitea 的 `url` 是 API URL，不应映射到 GitHub GraphQL 的 `url`。 |
| `state` | `state` + `merged` | Gitea 的对象状态只有小写 `open`/`closed`。GraphQL facade 应转换为 `open -> OPEN`；`closed && merged=true -> MERGED`；`closed && merged=false -> CLOSED`。这是网关转换规则，不是 Gitea 原生枚举。 |
| `baseRefName` | `base.label` / Gitea converter 的 `Base.Name` | 取 base branch 名。 |
| `headRefName` | `head.label` / Gitea converter 的 `Head.Name` | 当前 Gitea converter 将 `pr.HeadBranch` 写入 `label`，所以优先使用它作为 branch 名；不要把隐藏的 `refs/pull/<N>/head` 当成用户 branch 名。 |
| `isCrossRepository` | `base.repo_id` 与 `head.repo_id`，或两个 repo 的 full name | 两个 repo 不同时为 `true`。 |
| `headRepositoryOwner.login` | `head.repo.owner.login` | 跨 repo 时必须返回；同 repo 也可以返回 base owner 以保持对象完整。 `headRepositoryOwner.id` 和 User fragment 的 `name` 同样应提供类型兼容值。 |
| `defaultBranchRef.name` | base repo `default_branch` | 直接映射为 branch name。 |

`PullRequest.state` 的 GraphQL enum 以及 `headRefName` / `headRepositoryOwner` / `isCrossRepository` 的语义可与 [GitHub GraphQL PullRequest 文档](https://docs.github.com/en/graphql/reference/pulls) 对照；`Repository.defaultBranchRef.name`、`nameWithOwner` 和 `parent` 可与 [GitHub GraphQL Repository 文档](https://docs.github.com/en/graphql/reference/repos) 对照。

网关拿到 Gitea 列表后，应按当前 `gh` 的行为：

1. 用 `head.label == HEAD_BRANCH`（同 repo）或 head repo owner + branch（跨 repo）筛选；
2. 按 Gitea 创建时间倒序保留顺序；
3. 将 `OPEN` 节点稳定地移到其他状态之前；
4. 复现 head 为默认 branch 时只接受 `OPEN` 的规则；
5. 返回 GraphQL `nodes`。

#### direct / extra requests / known incompatibilities

- **协议不是 direct**：Gitea 有足够的 REST PR 数据，但没有对应 GraphQL schema；对 `gh pr view` 必须由网关仿真 GraphQL operation。
- **需要额外筛选或请求**：Gitea 列表支持 base branch 过滤，但不支持本次 GraphQL 查询使用的 head branch 过滤。大量无关 PR、超过 30 个候选或多个同名 branch 时，需要翻页/服务端查询后再筛选。
- **状态有语义差异**：Gitea 把 merged PR 的对象 `state` 仍表示为 `closed`，另用 `merged` 表示已合并；GraphQL 的 `MERGED` 只能由网关组合得到。
- **分支删除差异**：Gitea converter 在 head branch 不存在时可能把 `head.ref` 回退到隐藏 PR ref，但 `head.label` 仍来自 `HeadBranch`。因此匹配应依赖 `label` 和 `head.sha` 等字段；如果某部署无法返回原始 `HeadBranch`，精确复现 GitHub 的 deleted-head 行为是 `Unknown`。
- **ID 差异**：Gitea ID 是整数，GitHub GraphQL ID 是字符串/opaque ID。当前 P0 不比较该值，但 schema 和 JSON 类型必须兼容。
- **权限/错误差异**：Gitea 原生 list 在 repo 不可见时按 Gitea API 权限返回错误；网关需要把它转换为 GraphQL `errors` 或与 GitHub-compatible host 一致的错误，而不能把内部 Gitea error 文本直接当成功的 `nodes`。

## 2. gh repo view

### gh behavior

#### command

Codex TUI 实际执行的是：

```bash
gh repo view --json nameWithOwner,parent
```

无 repository 参数。`repo view` 无参数时使用当前目录解析出来的 base repo；命令定义、JSON flag 和 no-argument 分支在 [`pkg/cmd/repo/view/view.go#L36-L72`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/repo/view/view.go#L36-L72) 与 [`view.go#L80-L123`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/repo/view/view.go#L80-L123)。root 将 `repo` 命令接到 `SmartBaseRepoFunc`，见 [`root.go#L162-L179`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/root/root.go#L162-L179)。

#### GitHub protocol

这是一次 GraphQL 请求，不是 REST。`FetchRepository` 直接构造 `RepositoryInfo` operation：

```graphql
query RepositoryInfo($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    nameWithOwner,parent{id,name,owner{id,login}}
  }
}
```

operation 和变量在 [`api/queries_repo.go#L286-L315`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/queries_repo.go#L286-L315)，字段选择规则在 [`api/query_builder.go#L555-L572`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/query_builder.go#L555-L572)。variables 为：

```json
{
  "owner": "BASE_OWNER",
  "name": "BASE_REPO"
}
```

GraphQL endpoint、默认认证头与 1 节相同：对 `github.com` 是 `POST https://api.github.com/graphql`；当前 client 还会携带 `X-GitHub-Api-Version: 2022-11-28`、`GraphQL-Features: merge_queue`、GitHub CLI User-Agent 和可用的 `Authorization: token ...`。

#### required response fields

| `--json` field | GraphQL field | `gh` 的实际依赖/输出 |
| --- | --- | --- |
| `nameWithOwner` | `Repository.nameWithOwner` / `String!` | exporter 直接输出 owner/name 字符串，例如 `OWNER/REPO`。它不是从 `name` 在客户端拼接的。 |
| `parent` | `Repository.parent` / `Repository` 或 `null`，选择 `id,name,owner{id,login}` | exporter 只输出一个 mini repository：`id`, `name`, `owner`；没有 parent 时输出 JSON `null`。 |

`Repository.ExportData` 和 `miniRepoExport` 明确了 `parent` 的最终 JSON 形状，见 [`api/export_repo.go#L7-L52`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/export_repo.go#L7-L52)。因此一个 fork 的逻辑输出形状是：

```json
{
  "nameWithOwner": "FORK_OWNER/FORK_REPO",
  "parent": {
    "id": "PARENT_GRAPHQL_ID",
    "name": "PARENT_REPO",
    "owner": {
      "id": "PARENT_OWNER_GRAPHQL_ID",
      "login": "PARENT_OWNER"
    }
  }
}
```

上面的键顺序只用于说明结构，JSON map 顺序不是行为要求。父仓库的 `nameWithOwner` 没有被 query 选择，也不会被 exporter 输出。

仓库不存在时，GraphQL 正常应返回 `repository: null` 并带 `errors`；即使服务端只返回 null，`FetchRepository` 也会把它转换为 `NOT_FOUND` GraphQLError，见 [`queries_repo.go#L299-L315`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/queries_repo.go#L299-L315)。

#### 当前仓库、branch、remote、host 解析

本命令只需要 base repo，不需要当前 branch，也不向 GitHub 发送 branch 参数。base repo 的解析仍然是：

- `git remote -v`、SSH/HTTPS URL 解析、已认证 host 过滤；
- `remote.*.gh-resolved` 的显式选择优先；
- 非交互调用取排序后的第一个 remote；交互调用可能先发 `query RepositoryNetwork` 消歧。

完整 resolver 证据在 [`factory/default.go#L153-L176`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/factory/default.go#L153-L176)、[`context/context.go#L19-L109`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/context/context.go#L19-L109) 和 [`context/remote.go#L60-L123`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/context/remote.go#L60-L123)。TUI wrapper 设置 `GH_PROMPT_DISABLED=1` 的证据仍是附件 [`branch_summary.rs#L573-L643`](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs#L573-L643)。

### Gitea mapping

#### endpoint(s)

最接近的 Gitea 原生请求是：

```text
GET /api/v1/repos/{owner}/{repo}
```

当前 handler 的 Swagger 注释和返回路径在 [`routers/api/v1/repo/repo.go#L498-L528`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/routers/api/v1/repo/repo.go#L498-L528)，官方文档为 [`repo-get`](https://docs.gitea.com/api/1.25/operations/repo-get/)。所需 path 参数只有 `owner` 和 `repo`，没有 query 参数。

#### response fields and transformation rules

Gitea `Repository` 结构直接包含 `id`, `name`, `full_name`, `owner`, `fork`, `parent` 和 `default_branch`，见 [`modules/structs/repo.go#L58-L90`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/modules/structs/repo.go#L58-L90)。Gitea 转换器把 fork 的原始父仓库放入 `Parent`，非 fork 为 nil，并且 parent 层不再递归加载 parent，见 [`services/convert/repository.go#L15-L60`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/services/convert/repository.go#L15-L60)。

GraphQL facade 的映射应为：

| GraphQL 字段 | Gitea 来源 | 规则 |
| --- | --- | --- |
| `nameWithOwner` | `full_name` | 直接映射；不要只使用 `name`，也不要用 Gitea API `url` 拼接。 |
| `parent` | `parent` | `parent == null` 时返回 GraphQL `null`；存在时只填充 `id`, `name`, `owner{id,login}`。 |
| `parent.id` / `parent.owner.id` | Gitea 整数 id | 转为稳定的 GraphQL `ID` 字符串。 |
| `parent.owner.login` | `parent.owner.login` | Gitea User 的 JSON `login` 字段直接映射。 |

Gitea 官方文档也明确 `parent` 只在 fork 场景出现，见 [Get a repository](https://docs.gitea.com/api/1.25/operations/repo-get/)；GitHub 的对应语义见 [REST Get a repository](https://docs.github.com/en/rest/repos/repos#get-a-repository)。两者的 parent 语义接近，但 REST response 更丰富，必须按本次 GraphQL selection 和 `miniRepoExport` 裁剪。

#### direct / extra requests / known incompatibilities

- **数据能力基本 direct，协议不是 direct**：Gitea 一个 GET repo 足以提供本次两个字段；但 `gh repo view` 发的是 GraphQL，网关必须提供 `RepositoryInfo` facade。
- **parent 的形状不同**：Gitea REST parent 是完整/较丰富的 repository object；`gh` 本次只要求 `id,name,owner{id,login}`，且 exporter 会再次裁剪为 mini object。
- **ID 类型不同**：Gitea 整数 ID 与 GitHub opaque GraphQL ID 不同。当前 TUI 只用于解码 JSON，并不比较 parent ID；仍不能把整数 JSON 数字直接放进 GraphQL `ID` 字段后期待所有客户端行为一致。
- **host/URL 不应泄漏 Gitea API URL**：`nameWithOwner` 是逻辑字符串；本次没有请求 repository `url`，所以不需要把 Gitea API URL 暴露给 `gh`。
- **权限错误**：Gitea `GET /repos/{owner}/{repo}` 对不可见 repo 返回 HTTP error；网关应转换为 GraphQL error/null 语义。直接把 Gitea 200 REST JSON 原样作为 GraphQL data 是不兼容的。

## 3. commit -> PR lookup

### gh behavior

#### command

Codex TUI 在当前 branch `gh pr view` 失败后先执行：

```bash
git rev-parse HEAD
```

然后对 `gh repo view` 得到的候选仓库依次执行：

```bash
gh api -H "Accept: application/vnd.github+json" repos/OWNER/REPO/commits/HEAD_SHA/pulls
```

parent 存在时先查 parent，再查当前 fork；每个返回数组中由 Codex 选择第一个 `state == "open"` 的元素，并读取 `number` 和 `html_url`。这些是上一阶段调查确认的实际 caller 行为，见 [`branch_summary.rs#L364-L392`](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs#L364-L392) 和 [`branch_summary.rs#L406-L423`](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs#L406-L423)。

#### GitHub protocol

这是 `gh api` 的 GitHub REST passthrough，不是 GraphQL。`gh api` 的 command implementation 会把无 `--method`、无 field、无 input 的请求保留为 GET，并把 endpoint path 原样交给 `httpRequest`，见 [`pkg/cmd/api/api.go#L311-L331`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/api/api.go#L311-L331)。

对 `github.com`，精确请求为：

```text
GET https://api.github.com/repos/OWNER/REPO/commits/HEAD_SHA/pulls
```

REST prefix 和 path 拼接在 [`pkg/cmd/api/http.go#L16-L35`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/api/http.go#L16-L35) 与 [`internal/ghinstance/host.go#L59-L69`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/internal/ghinstance/host.go#L59-L69)。本次请求：

- 没有 query 参数；
- 没有 request body；
- 没有 `--paginate`、`--jq`、`--template`；
- 明确发送 `Accept: application/vnd.github+json`；
- `api.NewHTTPClient` 额外提供 `X-GitHub-Api-Version: 2022-11-28`、`User-Agent: GitHub CLI <gh-version>`，并在有认证配置时添加 `Authorization: token <TOKEN>`。

headers/path/body 的具体处理在 [`http.go#L38-L92`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/api/http.go#L38-L92)、[`api/http_client.go#L35-L79`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/http_client.go#L35-L79) 和 [`api/http_client.go#L153-L188`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/api/http_client.go#L153-L188)。

`gh api` 本身不请求或裁剪字段，stdout 是 GitHub REST 返回的原始 JSON。GitHub 当前文档将该 endpoint 定义为“List pull requests associated with a commit”：默认 `per_page=30`、`page=1`，并说明它返回引入该 commit 的 merged PR；如果 commit 不在 default branch，还会返回与其关联的 merged/open PR，见 [GitHub REST commit endpoint](https://docs.github.com/en/rest/commits/commits#list-pull-requests-associated-with-a-commit)。

对 Codex caller 来说，响应数组至少要能提供：

| REST JSON field | caller 用途 |
| --- | --- |
| `state` | 与小写字符串 `open` 比较。 |
| `number` | 识别出的 PR number。 |
| `html_url` | 识别出的 PR web URL。 |

`gh api` 对成功 REST response 不做转换；对 HTTP status 大于 299 的 response，它会先把 response body 作为输出路径处理，再在 stderr 输出 `gh: HTTP <status>` 并以错误结束，见 [`pkg/cmd/api/api.go#L477-L565`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/api/api.go#L477-L565)。非 2xx 的完整 stdout/stderr 细节在没有实际 `gh` binary 的情况下不做额外推断。

host 方面，P0 argv 没有 `--hostname`；`gh api` 使用认证配置的 `DefaultHost()`，也会尊重 host 对应的 `api_host` 覆盖，见 [`api.go#L390-L424`](https://github.com/cli/cli/blob/38316c1c4f275030e3df6666382922e75410d68b/pkg/cmd/api/api.go#L390-L424)。此外，这一条 API 命令的 path 已经被 Codex 填成 `OWNER/REPO`，没有 `{owner}` / `{repo}` placeholder，所以不会通过 placeholder 触发本地 BaseRepo 或 branch 解析。

### Gitea mapping

#### endpoint(s)

Gitea 当前最接近的原生 endpoint 是单数路径：

```text
GET /api/v1/repos/{owner}/{repo}/commits/{sha}/pull
```

路由注册在 [`routers/api/v1/api.go#L1556-L1569`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/routers/api/v1/api.go#L1556-L1569)，handler 和 Swagger 定义在 [`routers/api/v1/repo/commits.go#L372-L420`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/routers/api/v1/repo/commits.go#L372-L420)，官方文档为 [`repo-get-commit-pull-request`](https://docs.gitea.com/api/1.25/operations/repo-get-commit-pull-request/)。它的 path 参数是 `owner`、`repo`、`sha`，成功返回 **一个** PR object，找不到返回 404。

该 endpoint 的数据库查询条件是：

```text
base_repo_id = repository.id
AND merged_commit_id = sha
```

证据是 [`models/issues/pull.go#L991-L1008`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/models/issues/pull.go#L991-L1008) 和 [`GetCommitPullRequest` handler#L401-L419](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/routers/api/v1/repo/commits.go#L401-L419)。因此它只查“该 SHA 恰好是 base repo 中某个 PR 的 merge commit”的情况，不是 GitHub endpoint 的直接替代。

可用于仿真 GitHub plural endpoint 的原生基础能力仍是：

```text
GET /api/v1/repos/{owner}/{repo}/pulls?state=all&page=N&limit=L
```

其参数、列表响应和分页行为见 [`pull.go#L48-L159`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/routers/api/v1/repo/pull.go#L48-L159) 和 [Gitea list PR 文档](https://docs.gitea.com/api/1.25/operations/repo-list-pull-requests/)。Gitea PR response 包含 `number`, `state`, `html_url`, `merged`, `merge_commit_sha`, `base`, `head` 等字段，见 [`modules/structs/pull.go#L10-L110`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/modules/structs/pull.go#L10-L110)。

#### required request parameters

网关对外应实现 GitHub-compatible endpoint：

```text
GET /repos/{owner}/{repo}/commits/{sha}/pulls
```

对本次 `gh api`，不应强制要求 `per_page`、`page` 或其他 query 参数，因为 gh 没有发送它们。网关内部可以使用：

```text
GET /api/v1/repos/{owner}/{repo}/pulls?state=all&page=1&limit=...
```

然后按 commit 关联规则筛选。如果使用 Gitea 的单数 `/commits/{sha}/pull` 作为优化路径，必须把单个对象转换并包装成数组；它只能覆盖 merged commit，不能作为唯一实现。

#### response fields and transformation rules

推荐的最小 GitHub-style response 元素为：

```json
[
  {
    "number": 123,
    "html_url": "https://gitea.example/OWNER/REPO/pulls/123",
    "state": "open"
  }
]
```

转换规则：

- Gitea `number` -> GitHub REST `number`，整数直接映射；
- Gitea `html_url` -> GitHub REST `html_url`，必须是网页 URL；
- Gitea `state` 保持小写 `open`/`closed`。即使 Gitea `merged=true`，也不要在这个 REST response 中把 `state` 改成 `merged`；Codex caller 只识别 `open`，而 GitHub REST PR object 的 `state` 也是 open/closed 维度；
- Gitea `merged`、`merge_commit_sha`、`head.sha` 可以作为网关内部筛选依据或额外返回字段，但本次 caller 不依赖它们；
- 无关联 PR 时应返回 `200` 和 JSON 空数组 `[]`，而不是把 Gitea 单数 endpoint 的 `404` 原样透传。GitHub 文档将该 plural endpoint 的正常响应定义为 200；Gitea 单数 endpoint 的 404 只表示其“merged commit exact match”失败。

commit 关联的候选匹配顺序应明确记录为网关策略：

1. 对 Codex 当前 branch 的 `git rev-parse HEAD`，优先匹配 Gitea PR `head.sha == sha`；这是当前 TUI fallback 最直接的语义。
2. 对已合并 PR，可匹配 `merge_commit_sha == sha`，以覆盖 Gitea 原生单数 endpoint 的能力。
3. 如果要完全复现 GitHub “associated with a commit”——例如 commit 是 PR head 历史中的较早 commit、或 GitHub 通过提交图识别 branch association——当前 `cli/cli` 不负责这层语义，Gitea 当前公共 API 也没有等价 endpoint；完整规则为 **Unknown**，不能声称仅凭 Gitea `/commits/{sha}/pull` 已兼容。

#### direct / extra requests / known incompatibilities

- **不是 direct**：GitHub path 是 plural `/pulls` 且返回 array；Gitea 原生 path 是 singular `/pull` 且返回 object。
- **语义不同**：Gitea handler 只按 `base_repo_id + merged_commit_id` 查询 merged PR；它对 open PR head SHA 通常返回 404，而 Codex 正是用当前 branch HEAD SHA 做 fallback。
- **需要额外请求/分页**：使用 Gitea list endpoint 时必须 `state=all`，再检查 `head.sha`/`merge_commit_sha`；Gitea list 没有 commit filter，所以可能需要多页。若 list response 在某部署缺少可靠的 head SHA，需再调用 `/pulls/{index}`；当前 Gitea struct 和 converter 已定义 `head.sha`，但不同存储状态下 deleted head 的可恢复程度需要部署验证。
- **关联范围差异**：GitHub 官方文档明确该 endpoint 对 default branch 与非 default branch 有不同的 associated-PR 语义；Gitea 当前源码只证明 exact merged commit lookup。commit history/graph association 的 parity 为 **Unknown**。
- **状态差异**：Gitea `StateType` 只有小写 `open`/`closed`，并单独有 `merged` bool，见 [`modules/structs/issue.go#L16-L29`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/modules/structs/issue.go#L16-L29)。
- **响应/错误差异**：Gitea API error 是包含 `message`（以及可能的 `url`）的 JSON object，见 [`modules/structs/miscellaneous.go#L117-L123`](https://github.com/go-gitea/gitea/blob/7b036e96c2e0d88c9cb582001783f8f96ab06be2/modules/structs/miscellaneous.go#L117-L123)；网关需要对外保持 GitHub REST-compatible status/body，而不是把单数 Gitea 404 当成“请求失败”来终止 TUI 的 fallback。

## 4. Compatibility Table

| gh command | GitHub request | Gitea request | Direct / Emulated | Main differences |
| --- | --- | --- | --- | --- |
| `gh pr view --json number,url,state` | `POST /graphql`, operation `PullRequestForBranch`; `owner`, `repo`, `headRefName`, `states=null`; `pullRequests(... first:30, orderBy: CREATED_AT DESC)` | `GET /api/v1/repos/{owner}/{repo}/pulls?state=all&page=...&limit=...`，然后按 head branch 过滤；对外由网关 `POST /graphql` 仿真 | Emulated | Gitea 无 GraphQL/head filter；需要分页/额外筛选；`open/closed+merged` 与 `OPEN/CLOSED/MERGED` 不同；ID/URI 类型不同。 |
| `gh repo view --json nameWithOwner,parent` | `POST /graphql`, operation `RepositoryInfo`; `owner`, `name`; fields `nameWithOwner,parent{id,name,owner{id,login}}` | `GET /api/v1/repos/{owner}/{repo}`；对外由网关 `POST /graphql` 仿真 | Emulated（数据近似 direct） | `full_name -> nameWithOwner`；Gitea parent response 更丰富；parent 需要裁剪；整数 ID 需转 GraphQL ID；权限错误需转 GraphQL errors/null。 |
| `gh api -H "Accept: application/vnd.github+json" repos/{owner}/{repo}/commits/{sha}/pulls` | `GET /repos/{owner}/{repo}/commits/{sha}/pulls`; no query/body; raw JSON array | 对外同一路径；内部推荐 `GET /api/v1/repos/{owner}/{repo}/pulls?state=all` + `head.sha`/`merge_commit_sha` 筛选；`GET .../commits/{sha}/pull` 仅作 merged fast path | Emulated | Gitea 原生是 singular object、merged-only、404-on-miss；GitHub 是 plural array、200 `[]`、关联 open/merged；需要分页与响应重塑。 |

补充：若要覆盖 `gh pr view` 在 `refs/pull/<N>/head` checkout 上的 edge path，还要增加 `PullRequestByNumber` GraphQL operation；该路径不是附件确认的普通 branch TUI P0 请求，故不列入下表的 minimum surface。

## 5. Minimum Gateway Surface

以下是让附件确认的三条 Codex TUI P0 调用通过所需的最小对外 surface。

### REST endpoint

1. `GET /repos/{owner}/{repo}/commits/{sha}/pulls`
   - 接受当前 `gh api` 发送的 `Accept: application/vnd.github+json`、`X-GitHub-Api-Version: 2022-11-28` 和 `Authorization: token ...`；不要要求 gh 没有发送的 query 参数。
   - 成功返回 JSON array；无匹配返回 HTTP 200、`[]`。
   - array element 至少有 `number`、`html_url`、`state`，其中 state 是小写 `open`/`closed`。
   - Gitea backend 可使用 `GET /api/v1/repos/{owner}/{repo}/pulls?state=all&page=&limit=`，并按 `head.sha` / `merge_commit_sha` 仿真 commit association；原生 `GET /api/v1/repos/{owner}/{repo}/commits/{sha}/pull` 不是必需 endpoint，也不能单独满足 P0。

### GraphQL endpoint and operations

对外提供：

```text
POST /graphql
```

请求 body 兼容：

```json
{
  "query": "...",
  "variables": {"owner": "...", "repo": "...", "headRefName": "...", "states": null}
}
```

最低需要支持两个 operation：

- `PullRequestForBranch`：
  - `Query.repository(owner: String!, name: String!)`；
  - `Repository.pullRequests(headRefName: String, states: [PullRequestState!], first: Int, orderBy: PullRequestOrder)`；
  - `PullRequestConnection.nodes`；
  - `PullRequest.number: Int!`；
  - `PullRequest.url: URI!`；
  - `PullRequest.state: PullRequestState!`，至少 `OPEN`, `CLOSED`, `MERGED`；
  - `PullRequest.id: ID!`；
  - `PullRequest.baseRefName: String!`；
  - `PullRequest.headRefName: String!`；
  - `PullRequest.isCrossRepository: Boolean!`；
  - `PullRequest.headRepositoryOwner`，支持 `id`, `login` 和 inline `... on User { name }`；
  - `Repository.defaultBranchRef.name: String!` 或兼容的 nullable object/name 组合，使当前 Go decoder 能解码 `defaultBranchRef { name }`；
  - `PullRequestOrder` 能接受 inline 的 `field: CREATED_AT`、`direction: DESC`。
- `RepositoryInfo`：
  - `Query.repository(owner: String!, name: String!)`；
  - `Repository.nameWithOwner: String!`；
  - `Repository.parent: Repository` 或 null；
  - `Repository.id: ID!`、`Repository.name: String!`；
  - `Repository.parent.owner` 至少支持 `id: ID!`、`login: String!`。

本阶段不需要为这三个调用实现 reviews、checks、Actions、mutation 或其他 GraphQL fields。也不需要把 `PullRequestByNumber` 计入普通 branch TUI minimum；若产品目标改为任意 checkout 状态，则另行加入。

### GraphQL response / error behavior

- 成功响应使用标准 envelope：`{"data":{"repository":...}}`；
- GraphQL 失败使用 `errors` array，使 `cli/api.Client.GraphQL` 能返回 `GraphQLError`；
- 找不到 repo 时返回 `repository: null` 并带 GraphQL error，或至少使 `FetchRepository` 的 null fallback 能得到等价 `NOT_FOUND`；
- branch 没有匹配 PR 时返回空 `pullRequests.nodes`，不要把“没有 PR”误报为 HTTP transport error；`gh` 会在本地生成 `no pull requests found`；
- 所有请求应保留认证、host、权限和 HTTP 状态语义；Gitea 的内部 `message/url` 错误需要转换到对外兼容层。

### Headers / status / host behavior

- GraphQL：接受 `Content-Type: application/json`，返回 `application/json`；容忍并可转发 `X-GitHub-Api-Version: 2022-11-28`、`GraphQL-Features: merge_queue`、`User-Agent` 和 `Authorization: token ...`。
- REST commit lookup：返回 `Content-Type: application/json`；接受 `Accept: application/vnd.github+json`；正常响应 200，空数组仍为 200；仓库不存在、无权限、后端错误分别保留可识别的 4xx/5xx，不要把 Gitea singular endpoint 的 404 当成无条件的最终响应。
- host：当 `gh` 的 remote host 被配置为 gateway host 时，GraphQL 和 REST 都要在该 host 的 GitHub-compatible API 路径工作；对默认 `github.com` 形态，路径分别是 `/graphql` 和 `/repos/...`，由 `gh` 解析到 `api.github.com`。`GH_HOST`、认证 default host、`api_host` 覆盖会改变实际 host，不能在网关层假设永远只有 github.com。

以上 minimum 不包含当前 `cli/cli` 可能在可交互 remote 消歧时触发的 `RepositoryNetwork`，也不包含 PR-ref edge path；这两项如需支持，应作为单独兼容性增量验证，而不是悄悄混入本批 P0。

