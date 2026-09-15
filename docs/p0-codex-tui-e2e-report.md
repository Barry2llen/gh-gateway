# Codex TUI P0 Compatibility E2E 验证报告

- 验证日期：2026-09-15
- 项目：`gh-gateway`
- 结论：通过

## 1. 验证目标

验证以下完整链路能够识别 Gitea 中的 open pull request：

```text
Codex TUI
   ↓
real gh
   ↓
gh-gateway
   ↓
real Gitea
```

本次验证覆盖：

1. 普通 `feature` branch 上的直接 PR detection。
2. 同一 PR HEAD commit 上 detached HEAD 状态下的 commit fallback。

未实现或验证 checks、Actions、review、create、merge 等 P1 功能，也未修改 Codex 源码。

## 2. 版本与环境

| 组件 | 版本 |
| --- | --- |
| Codex CLI | `0.154.0` |
| Codex release tag | `rust-v0.154.0` |
| Codex commit | `36eab01061df3cde5f95ec20a526777b430091ba` |
| gh | `2.95.0`（2026-06-17） |
| Gitea | `1.25.5`，Go `1.25.8` |
| TLS proxy | Caddy `2.10.2` |

测试在现有 Docker Compose 网络中完成。Codex 登录文件仅在运行时只读挂载，没有写入镜像或仓库。Caddy 使用内部 CA 提供受信任 HTTPS，并将访问日志写入测试卷；日志中的 Authorization header 自动脱敏。

Codex 使用以下真实运行策略启动：

```text
sandbox = danger-full-access
approval policy = never
```

这是为了允许 Codex background workspace command 在测试容器中访问 Gitea 网关网络，与当前本地 Codex 配置一致。状态栏启用了：

```toml
[tui]
status_line = ["git-branch", "pull-request-number"]
status_line_use_colors = false
```

OpenAI 官方配置文档将该选项定义为 `tui.status_line`：[Codex configuration reference](https://developers.openai.com/codex/config-reference)。

## 3. Gitea fixture

测试仓库：

```text
gateway/bar
```

测试数据：

```text
main
└── feature
    └── open PR #1 -> main
```

PR 信息：

```text
number = 1
state = open
url = https://git.example.test/gateway/bar/pulls/1
feature HEAD = d0e1ef8fcda56f888dcd1a78536faa38fd221dff
```

## 4. 场景一：普通 branch path

### Checkout 状态

```text
branch = feature
HEAD = d0e1ef8fcda56f888dcd1a78536faa38fd221dff
```

### Codex TUI 结果

Codex TUI 状态栏实际显示：

```text
feature · PR #1
```

其中 `PR #1` 的 hyperlink 指向：

```text
https://git.example.test/gateway/bar/pulls/1
```

### Codex 实际子进程

Codex 自身日志确认执行：

```text
gh pr view --json number,url,state
```

没有执行 `git rev-parse HEAD`、`gh repo view` 或 commit-to-PR REST fallback。

### Gateway 实际请求

Caddy access log 记录：

```text
POST /api/graphql
HTTP 200
User-Agent: GitHub CLI 2.95.0
```

这次 GraphQL 请求对应 `PullRequestForBranch`。正常 branch 场景没有访问 `/api/v3/repos/.../commits/.../pulls`。

### 结果

通过。Codex 通过真实 `gh pr view` 和 `gh-gateway` 正确识别 Gitea PR #1。

## 5. 场景二：detached HEAD commit fallback

### Checkout 状态

同一 checkout 切换为 detached HEAD：

```text
branch = <empty>
HEAD = d0e1ef8fcda56f888dcd1a78536faa38fd221dff
```

该状态符合 Codex `branch_summary.rs` 的 fallback 条件：branch lookup 失败后仍可通过 `git rev-parse HEAD` 得到当前 commit。相关实现见 [Codex 0.154.0 branch_summary.rs](https://github.com/openai/codex/blob/36eab01061df3cde5f95ec20a526777b430091ba/codex-rs/tui/src/branch_summary.rs)。

### Codex TUI 结果

Codex TUI 状态栏实际显示：

```text
PR #1
```

detached HEAD 下没有 branch 名，PR hyperlink 仍指向：

```text
https://git.example.test/gateway/bar/pulls/1
```

### Codex 实际子进程顺序

Codex 自身日志记录：

```text
gh pr view --json number,url,state
git rev-parse HEAD
gh repo view --json nameWithOwner,parent
gh api -H "Accept: application/vnd.github+json" \
  repos/gateway/bar/commits/d0e1ef8fcda56f888dcd1a78536faa38fd221dff/pulls
```

`gh pr view` 在 detached HEAD 下于本地失败，没有向 Gateway 发起 `PullRequestForBranch` 请求。Codex 随后按预期进入 commit fallback。

### Gateway 实际请求

Caddy access log 按顺序记录：

```text
POST /api/graphql
GET /api/v3/repos/gateway/bar/commits/d0e1ef8fcda56f888dcd1a78536faa38fd221dff/pulls
```

两次请求均返回 HTTP 200：

- `POST /api/graphql` 对应 `gh repo view` 的 `RepositoryInfo`。
- `GET /api/v3/.../pulls` 对应 commit-to-PR REST lookup。

REST 请求携带：

```text
Accept: application/vnd.github+json
User-Agent: GitHub CLI 2.95.0
X-GitHub-Api-Version: 2022-11-28
```

Gateway 返回的有效数据为：

```json
[
  {
    "number": 1,
    "html_url": "https://git.example.test/gateway/bar/pulls/1",
    "state": "open"
  }
]
```

### 结果

通过。Codex 在 `gh pr view` 失败后，经真实 `git`、`gh repo view` 和 `gh api` 成功识别同一个 Gitea PR #1。

## 6. 新请求与兼容性结论

本次没有发现此前静态调查未记录的新 P0 API 请求。

实际出现的 Gateway API surface 只有：

```text
POST /api/graphql
GET /api/v3/repos/{owner}/{repo}/commits/{sha}/pulls
```

没有触发 checks、Actions、review、create、merge 或其他 P1 endpoint，也没有为了通过测试扩展 Gateway。

## 7. 验证与清理

验证期间同时确认：

```text
go test ./...  -> pass
go vet ./...   -> pass
docker compose --profile codex config --quiet -> pass
```

验证结束后已删除测试容器、网络和测试卷。

## 8. 最终判定

| 验收项 | 结果 |
| --- | --- |
| 普通 branch PR detection | 通过 |
| detached HEAD commit fallback | 通过 |
| PR number 识别 | `1`，正确 |
| PR URL 识别 | 正确 |
| Gateway 实际路径符合预期 | 通过 |
| 新的 P0 API 依赖 | 无 |
| P1 scope 被触发或实现 | 否 |

基于当前固定版本组合和两个真实 Codex TUI 场景，`gh-gateway` 的 Codex TUI P0 compatibility milestone 可以判定通过。
