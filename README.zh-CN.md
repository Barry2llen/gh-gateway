# gh-gateway

[English](README.md) | [简体中文](README.zh-CN.md)

> 一个聚焦于 GitHub API 兼容的网关，让 Codex 和特定的 `gh` 工作流可以配合 Gitea 使用。

`gh-gateway` 将一组范围明确的 GitHub GraphQL 与 REST 请求转换为 Gitea API 调用。在 Windows 11 上，它可以透明地运行在现有 Gitea 主机名前方：仓库、浏览器及其他 Gitea 流量仍使用原域名，受支持的 `gh` 命令则由网关处理。

它有意**不实现**完整的 GitHub API。遇到不支持或无法确定的数据时，网关会保守地返回失败，而不会把拉取请求或工作流误判为成功。

## 目录

- [支持范围](#支持范围)
- [工作原理](#工作原理)
- [快速开始：Windows 透明模式](#快速开始windows-透明模式)
- [服务端模式](#服务端模式)
- [开发与测试](#开发与测试)
- [兼容边界](#兼容边界)

## 支持范围

当前兼容层面向 Codex P0 命令集、P1-A 拉取请求监控和 P1-B Actions 监控。

| 领域 | 支持的工作流 |
| --- | --- |
| 仓库与身份 | `gh repo view`、`gh api user` |
| 拉取请求 | `gh pr view`、按提交查找 PR、会话评论、Review 和行内评论 |
| 检查项 | `gh pr checks` 及其 GraphQL 能力探测与状态检查查询 |
| Actions | 列出工作流运行、列出 Job、获取 Job 日志 |

代表性命令：

```text
gh repo view --json nameWithOwner,parent
gh api user
gh pr view --json number,url,state
gh api -H "Accept: application/vnd.github+json" repos/OWNER/REPO/commits/HEAD_SHA/pulls
gh pr view 1 --json number,url,state,mergedAt,closedAt,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeable,mergeStateStatus,reviewDecision
gh api 'repos/OWNER/REPO/issues/PR/comments?per_page=100&page=1'
gh api 'repos/OWNER/REPO/pulls/PR/reviews?per_page=100&page=1'
gh api 'repos/OWNER/REPO/pulls/PR/comments?per_page=100&page=1'
gh pr checks PR --json name,state,bucket,link,workflow,event,startedAt,completedAt
gh api repos/OWNER/REPO/actions/runs -X GET -f head_sha=HEAD_SHA -f per_page=100
gh api repos/OWNER/REPO/actions/runs/RUN_ID/jobs -X GET -f per_page=100
gh api repos/OWNER/REPO/actions/jobs/JOB_ID/logs
```

## 工作原理

```text
Codex / gh
    │  GitHub 兼容请求
    ▼
gh-gateway
    ├─ /api/graphql ───────► 受支持的 GraphQL 操作 ─┐
    ├─ /api/v3/... ────────► 受支持的 REST 端点 ────┤─► Gitea API
    └─ 其他所有路径 ───────► 透明转发 ───────────────┘   （仅本地模式）
```

默认情况下，网关会转发传入请求的 `Authorization` 请求头，不会持久化令牌，也不会把令牌传给 Docker。在服务端模式下配置 `GITEA_TOKEN` 后，它会覆盖传入的认证信息，并以 `Authorization: token <GITEA_TOKEN>` 发送给 Gitea。

透明模式会先记录该主机原有的 IPv4 地址，再写入带有专属标记的 hosts 条目。容器连接原始 IP，同时保留原始 HTTP Host 与 TLS SNI，从而在不关闭上游证书校验的情况下避免路由循环。

## 快速开始：Windows 透明模式

### 环境要求

- Windows 11，以及以管理员身份运行的 PowerShell
- 使用 Linux 容器的 Docker Desktop
- 可解析到 IPv4、且上游 HTTPS 证书有效的 Gitea 主机名
- 本机端口 `443`；如未关闭 SSH 透传，还需要端口 `22`
- 一个 Gitea 个人访问令牌

### 安装并启动

从 [GitHub Releases](https://github.com/Barry2llen/gh-gateway/releases) 下载与系统架构匹配的文件：

- `gh-gateway-windows-amd64.exe`
- `gh-gateway-windows-arm64.exe`

然后运行：

```powershell
Rename-Item gh-gateway-windows-amd64.exe gh-gateway.exe
.\gh-gateway.exe start git.example.com

$env:GH_HOST = 'git.example.com'
$env:GH_ENTERPRISE_TOKEN = '<Gitea PAT>'
codex
```

正式发布的 CLI 会自动选择 `ghcr.io/barry2llen/gh-gateway` 中版本匹配的运行时镜像。仅在需要显式覆盖时使用 `--image`。

### 状态检查与卸载

请在管理员 PowerShell 中运行本地模式命令：

```powershell
.\gh-gateway.exe status
.\gh-gateway.exe doctor
.\gh-gateway.exe stop
.\gh-gateway.exe uninstall
```

- `stop` 会移除容器以及由 `gh-gateway` 管理的 hosts 区块，但保留当前用户的本地 CA，以便后续复用。
- `uninstall` 还会按已记录的证书指纹移除该 CA，并删除 `%LOCALAPPDATA%\gh-gateway`。
- 如果不需要 SSH 透传，可在 `start` 后添加 `--no-ssh-proxy`。停止本地模式前，使用该 Gitea 主机名的 SSH Remote 将不可用。

本地模式目前仅支持单个主机、IPv4 上游解析、本机 `443` 端口 HTTPS，以及可选的同端口 SSH 透传。它不提供自动 UAC 提权、Linux/macOS 主机编排、多主机管理、后台服务、自定义 DNS、WSL 专用网络、Kubernetes 部署或安装程序。

## 服务端模式

如果 DNS 与 TLS 由外部设施管理，可将网关作为普通 HTTP 服务运行：

```powershell
$env:GITEA_BASE_URL = 'https://gitea.example.com'
$env:GATEWAY_ADDR = ':8080'

# 可选；未设置时会转发传入的 Authorization 请求头。
$env:GITEA_TOKEN = 'gitea-token'

go run ./cmd/gh-gateway serve
```

不带子命令运行 `gh-gateway`，等价于执行 `gh-gateway serve`。

由于 `gh` 要求自定义 Enterprise 风格主机使用 HTTPS，需要配置可信 DNS 和终止 TLS 的反向代理：

```text
https://git.example.com/api/graphql -> http://127.0.0.1:8080/api/graphql
https://git.example.com/api/v3/*    -> http://127.0.0.1:8080/api/v3/*
```

与本地透明模式不同，服务端模式只提供兼容路由；其他路径会返回 `404`，除非外围代理将它们转发到别处。

### 手动冒烟测试

创建一个只配置网关 Remote 的临时仓库：

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

对于非 Fork 仓库，`gh repo view` 的结果应包含：

```json
{"nameWithOwner":"foo/bar","parent":null}
```

## 开发与测试

项目使用 Go 1.27。开发本地透明模式时，需要同时构建运行时镜像与 Windows CLI：

```powershell
docker build --target runtime -t gh-gateway:local .
go build -o gh-gateway.exe ./cmd/gh-gateway
.\gh-gateway.exe start git.example.com --image gh-gateway:local
```

运行 Go 检查：

```powershell
go test ./...
go test -race ./...
go vet ./...
```

容器化测试套件不依赖宿主机 C 编译器，会执行普通测试、Race 检测和静态检查。端到端环境会启动 Gitea 1.25.5、act_runner 0.2.10，并通过 Caddy 提供可信 TLS，然后分别使用 `gh` 2.95.0 与 2.100.0 进行验证。

```powershell
docker compose --profile test run --rm --build tests
docker compose up --build --abort-on-container-exit --exit-code-from e2e e2e
docker compose down -v --remove-orphans
```

另有一个需要管理员权限、默认不运行的 Windows 集成测试：

```powershell
$token = Read-Host 'Gitea PAT' -AsSecureString
.\scripts\windows-local-e2e.ps1 -HostName git.example.com -Image gh-gateway:local -Token $token -Repository owner/repo -PullRequest 1
```

E2E Runner 使用固定在 `0265dd7b4547fd6a88c6458f359b3c20731421c4` 的原版监控程序。它会验证：当运行或 Job 失败时，`--once` 能正常结束，且不会获取日志或触发重跑；它还会单独验证 `--retry-failed-now` 能到达“仅重跑失败项”的端点、收到明确的不支持响应，并且不对 Gitea 产生任何修改。

更详细的 API 映射和验证报告位于 [`docs/`](docs/) 目录。

## 兼容边界

实现刻意保持安全且收敛的行为：

- `mergeable=false` 映射为 `UNKNOWN`；`mergeStateStatus` 仅为 `DRAFT` 或 `UNKNOWN`；`reviewDecision` 为 `null`。未知数据不会让 PR 看起来已满足合并条件。
- `author_association` 为近似值：可识别仓库所有者和直接协作者，但不宣称支持组织 `MEMBER` 语义。
- Review 行号字段根据 Gitea Position 近似映射。
- Gitea 状态仅以 `StatusContext` 暴露；warning 和 skipped 会保守地映射为 `ERROR`。不支持 `CheckRun`、工作流运行的事件/时间信息和必需检查语义；`isRequired` 始终为 `false`。
- 提交到 PR 的匹配仅限 PR 的精确 Head SHA 和已合并 Commit SHA。
- 工作流和 Job 状态只接受已确认的 Gitea 状态。未知或自相矛盾的 Status/Conclusion 组合会返回兼容错误，而不会被当作成功。
- Workflow ID 是根据仓库和 Gitea 工作流文件名生成的稳定数字替代 ID。已从默认分支删除的工作流无法在重跑预检中解析。
- Job 日志属于**近似兼容**：直接返回 Gitea 的原始 `200 text/plain`，不复刻 GitHub 的 `302` 临时下载流程。
- **不支持**运行级 ZIP 日志和“仅重跑失败项”。`POST .../rerun-failed-jobs` 返回 `501`，不会调用 Gitea 的完整重跑或 Web UI 重跑行为。
- 网关不实现根路径 `/graphql`、`/user` 或 `/repos/...`；创建、合并、评论/Review 写入；Actions 完整重跑；Artifact；Dispatch；Cancel；Delete；Attempt API；以及其他通用 GitHub 兼容能力。在本地模式下，非兼容路径会原样转发到 Gitea。

如需精确的端点与字段映射，请参阅 [P0](docs/p0-api-mapping.md)、[P1-A](docs/p1a-api-mapping.md) 和 [P1-B](docs/p1b-api-mapping.md) 文档。
