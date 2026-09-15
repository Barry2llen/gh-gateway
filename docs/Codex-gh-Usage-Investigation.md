# Codex gh Usage Investigation

调查对象是公开仓库 [openai/codex](https://github.com/openai/codex)，快照为 main 当前提交 a8964cb1bad67bc26a826fb07d1bef99c6a3f008，时间为 2026-09-15。

本报告只做静态事实调查，不实现网关，也不做 GitHub API 到 Gitea API 的映射。命令中的占位符表示源码运行时填充的值。

检索方法：

- 对仓库做 rg -n --hidden -g '!.git/**' -i -w 'gh' 全量检索。
- 原始结果为 90 个文本匹配、28 个文件；其中包含文档、注释、环境变量名、GitHub Action 名称和权限前缀示例。
- 下文把真正出现在 subprocess、shell 脚本或 Codex 运行时命令构造中的调用，与仅作为文档说明的命令分开记录。

## 1. Summary

### 结论先行

当前仓库中能由源码直接确认的、与 Codex PR 工作流相关的 gh 使用主要是：

1. Codex TUI 内置的 PR 状态/识别逻辑：
   - gh pr view --json number,url,state
   - gh repo view --json nameWithOwner,parent
   - gh api -H Accept: application/vnd.github+json repos/OWNER/REPO/commits/HEAD_SHA/pulls
2. babysit-pr skill 的实际 Python watcher：
   - gh pr view
   - gh pr checks
   - gh api 查询 Actions runs、jobs、当前用户和三类 PR review 数据
   - gh run rerun RUN_ID --failed
3. babysit-pr skill 文档给 agent 的 CI 诊断命令：
   - gh run view ... --json ...
   - 直接获取 job logs 的 gh api .../actions/jobs/JOB_ID/logs
   - gh run view RUN_ID --log-failed 作为备用路径

静态源码中没有找到 Codex 自己实际执行的 gh pr list、gh pr create、gh pr comment、gh pr review 或 gh pr merge。codex-pr-body 文档要求使用 gh 更新 PR 标题和 body，但没有写出编辑命令，因此具体 argv 是 Unknown，不能据此推断为 gh pr edit。

公开的 openai/codex 仓库没有桌面 Codex App 的专有实现源码。因此，下面可以确认 CLI/TUI 和仓库内 skills 的行为；“桌面 Codex App 内置 PR 功能是否复用这些调用”仅靠这个仓库无法确认，列为 Unknown。

### 优先级

优先级表示对“让已观察到的 Codex PR 工作流运行起来”的兼容重要性，不表示命令调用频率。

| Priority | 实际依赖 |
| --- | --- |
| P0 | TUI 内置 PR 识别：gh pr view、gh repo view、commit-to-PR 的 gh api |
| P1 | babysit-pr 的 PR 元数据、checks、Actions runs/jobs、review feed、失败 job 重跑 |
| P2 | skill 文档中的人工/agent CI 诊断命令，以及 codex-pr-body 的分支到 PR 号查询 |
| Unknown | 桌面 App 专有调用；PR create/list/comment/review/merge/edit 的确切调用；没有显式 argv 的 skill 行为 |

## 2. Command Matrix

### 2.1 PR、Review、Checks、Actions 相关

| Priority | Command / argv | Scenario | Source | JSON / jq fields | Built-in / Skill | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| P0 | gh pr view --json number,url,state | TUI 当前 checkout 的当前分支 PR 识别 | [branch_summary.rs#L350-L362](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs#L350-L362) | --json: number,url,state | Codex TUI 内置 | 不带 --repo，在当前 workspace CWD 执行；只接受 state=OPEN |
| P0 | gh repo view --json nameWithOwner,parent | 为 commit-to-PR fallback 确定当前仓库及 fork parent | [branch_summary.rs#L406-L423](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs#L406-L423) | --json: nameWithOwner,parent | Codex TUI 内置 | parent 存在时先查 parent，再查当前 fork |
| P0 | gh api -H "Accept: application/vnd.github+json" repos/OWNER/REPO/commits/HEAD_SHA/pulls | 当前分支查询失败后，按 HEAD commit 查 upstream/fork 关联 PR | [branch_summary.rs#L364-L392](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs#L364-L392) | 没有 --json；原始 JSON 数组消费 number,html_url,state | Codex TUI 内置 | 先执行 git rev-parse HEAD；取第一个 open PR |
| P1 | gh [-R OWNER/REPO] pr view [NUMBER_OR_URL] --json number,url,state,mergedAt,closedAt,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeable,mergeStateStatus,reviewDecision | babysit-pr 解析 --pr auto、PR number 或 PR URL | [gh_pr_watch.py#L153-L199](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L153-L199) | --json: number,url,state,mergedAt,closedAt,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeable,mergeStateStatus,reviewDecision | Skill script 实际执行 | wrapper 对非 api 命令加入 -R；auto 第一次没有 positional arg；失败后没有第二种 PR 查询命令 |
| P1 | gh [-R OWNER/REPO] pr checks [PR_NUMBER] --json name,state,bucket,link,workflow,event,startedAt,completedAt | 汇总 pending / failed / passed checks | [gh_pr_watch.py#L276-L313](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L276-L313) | --json: name,state,bucket,link,workflow,event,startedAt,completedAt；实际汇总主要使用 bucket,state | Skill script 实际执行 | --pr auto 解析后改用具体 PR number |
| P1 | gh api repos/OWNER/REPO/actions/runs -X GET -f head_sha=HEAD_SHA -f per_page=100 | 找出当前 PR head SHA 对应的 workflow runs | [gh_pr_watch.py#L316-L364](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L316-L364) | 没有 --json；原始 JSON 的 workflow_runs[] 消费 id,name/display_title,status,conclusion,html_url,head_sha | Skill script 实际执行 | 未使用 --paginate，请求 per_page=100 |
| P1 | gh api repos/OWNER/REPO/actions/runs/RUN_ID/jobs -X GET -f per_page=100 | 获取失败 run 的 jobs，构造 failed_jobs | [gh_pr_watch.py#L367-L427](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L367-L427) | 没有 --json；jobs[] 消费 id,name,status,conclusion,html_url | Skill script 实际执行 | 未使用 --paginate，请求 per_page=100 |
| P1 | gh api user | 获取已认证 GitHub login，用于 review author 过滤 | [gh_pr_watch.py#L430-L436](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L430-L436) | 没有 --json；消费 login | Skill script 实际执行 | 不带 -R |
| P1 | gh api repos/OWNER/REPO/issues/PR_NUMBER/comments?per_page=100&page=PAGE | 获取 PR 顶层 conversation comments | [gh_pr_watch.py#L439-L462](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L439-L462), [#L565-L574](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L565-L574) | 没有 --json；消费 id,user.login,author_association,created_at,body,html_url | Skill script 实际执行 | Python 手工分页，直到返回数量小于 100 |
| P1 | gh api repos/OWNER/REPO/pulls/PR_NUMBER/comments?per_page=100&page=PAGE | 获取 inline review comments | 同上 | 没有 --json；另消费 pull_request_review_id,path,line/original_line | Skill script 实际执行 | 手工分页；用于关联父 review |
| P1 | gh api repos/OWNER/REPO/pulls/PR_NUMBER/reviews?per_page=100&page=PAGE | 获取 review submissions | 同上 | 没有 --json；消费 id,state,user.login,author_association,submitted_at/created_at,body,html_url | Skill script 实际执行 | PENDING review 会被忽略，直到提交 |
| P1 | gh [-R OWNER/REPO] run rerun RUN_ID --failed | --retry-failed-now 触发失败 jobs 重跑 | [gh_pr_watch.py#L800-L852](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L800-L852) | 无 --json | Skill script 实际执行 | 仅在 PR open、checks 已结束、有失败 run、重试预算未耗尽时调用；无备用重跑命令 |
| P2 | gh run view RUN_ID --json jobs,name,workflowName,conclusion,status,url,headSha | agent 手工/按 skill 指令检查失败 workflow | [babysit-pr/SKILL.md#L68-L76](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/SKILL.md#L68-L76) | --json: jobs,name,workflowName,conclusion,status,url,headSha | Agent/skill 文档指令 | 这是文档要求的诊断命令；watcher 本身没有执行 gh run view |
| P2 | gh api repos/OWNER/REPO/actions/jobs/JOB_ID/logs，shell 输出重定向到 /tmp/codex-gh-job-JOB_ID-logs.zip | 直接取得具体失败 job 的日志 | 同上 | 没有 --json；下载日志 zip | Agent/skill 文档指令 | 文档要求优先使用此路径；watcher 只返回 logs endpoint，不自行下载 |
| P2 | gh run view RUN_ID --log-failed | workflow 完成后的失败日志备用查询 | 同上 | 无 --json | Agent/skill 文档指令 | 文档明确这是 fallback，且可能在整体 run 完成前拿不到 failed-job logs |
| P2 | gh pr view BRANCH --repo openai/codex --json number --jq '.number' | 普通 Git 下从当前 branch / commit 找 PR number | [codex-pr-body/SKILL.md#L6-L12](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/codex-pr-body/SKILL.md#L6-L12) | --json: number；--jq: .number | Agent/skill 文档指令 | 文档用 may have to，前置还需要 git branch；不是仓库内实现代码 |
| Unknown | gh 用于编辑 PR 标题和 body，但没有给出具体 argv | codex-pr-body 的 PR 更新动作 | [codex-pr-body/SKILL.md#L10-L18](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/codex-pr-body/SKILL.md#L10-L18) | Unknown | Agent/skill 文档指令 | 不能从这句话推断 gh pr edit，也没有确认 body 查询字段或更新参数 |
| Unknown | gh pr list | PR list | 全仓库静态检索未发现实际调用 | Unknown | Unknown | 没有源码证据 |
| Unknown | gh pr create | PR create | 全仓库静态检索未发现实际调用 | Unknown | Unknown | 没有源码证据 |
| Unknown | gh pr comment / gh pr review | PR comment / review mutation | 全仓库静态检索未发现实际调用 | Unknown | Unknown | babysit-pr 只读查询 review 数据；没有 gh pr 写操作 argv |
| Unknown | gh pr merge | PR merge | 全仓库静态检索未发现实际调用 | Unknown | Unknown | 没有源码证据 |

说明：表中 gh api 命令没有使用 --json，但 GitHub API 的 stdout 是 JSON，Python/Rust 代码会自行解析。--jq 是 gh 的输出过滤参数，不等同于 --json 字段请求。

### 2.2 仓库中找到但不属于 Codex PR 运行时的 gh 调用

这些是全量调查中确实存在的调用，主要服务于 openai/codex 自己的 GitHub Actions、release 流程、issue 自动化或 Python/npm 发布。它们不是 Codex PR gateway 的第一阶段依据，因此不列入 P0/P1/P2 PR 依赖。

| Area | Command / argv | Source | JSON / fallback / scenario |
| --- | --- | --- | --- |
| Release binding | gh api PATH | [.github/scripts/resolve_python_cli_release.py#L12-L23](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/scripts/resolve_python_cli_release.py#L12-L23) | 无 --json；json.loads 解析。动态查询 actions/runs/RUN_ID、分页 jobs、tag ref、annotated tag、release、分页 release assets；把 Python 发布绑定到 Rust release |
| R2 release | gh release view TAG --repo openai/codex --json assets --jq "[.assets[] \| {name, size, state, digest}]" | [publish_r2_release.py#L71-L130](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/scripts/publish_r2_release.py#L71-L130) | --json: assets；jq 输出 name,size,state,digest；无 fallback，失败即报错 |
| R2 release | gh release download TAG --repo openai/codex --dir DIRECTORY --pattern ASSET_NAME ... | [publish_r2_release.py#L133-L158](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/scripts/publish_r2_release.py#L133-L158) | 无 --json；下载指定 release assets，下载后检查文件存在 |
| npm staging | gh run list --branch rust-vVERSION --json workflowName,url,headSha --workflow .github/workflows/rust-release.yml --jq 'first(.[])' | [stage_npm_packages.py#L149-L180](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/scripts/stage_npm_packages.py#L149-L180) | --json: workflowName,url,headSha；jq 取第一条；找不到即报错 |
| npm staging | gh api repos/openai/codex/actions/runs/WORKFLOW_ID/artifacts --paginate --jq ".artifacts[] \| [.name, .size_in_bytes] \| @tsv" | [stage_npm_packages.py#L269-L285](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/scripts/stage_npm_packages.py#L269-L285) | 无 --json；使用 --paginate 和 jq，消费 artifact name/size |
| npm staging | gh run download --name ARTIFACT --dir ARTIFACT_DIR --repo openai/codex WORKFLOW_ID | [stage_npm_packages.py#L288-L325](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/scripts/stage_npm_packages.py#L288-L325) | 无 --json；本地 artifact 目录已有内容时跳过下载 |
| Python runtime fallback | gh release download RELEASE_TAG --repo openai/codex --pattern ASSET_NAME --dir TEMP_ROOT | [_runtime_setup.py#L231-L260](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/sdk/python/_runtime_setup.py#L231-L260) | 无 --json；只有浏览器 URL 下载和带 token 的 API asset 下载失败/不可用后才走此 fallback；shutil.which('gh') 只是存在性检查，不是调用 |
| GitHub Actions release | gh release view RELEASE_TAG --repo GITHUB_REPOSITORY | [rust-release-zsh.yml#L40-L49](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/rust-release-zsh.yml#L40-L49) | 无 --json；检查 release 是否已存在，存在则失败 |
| GitHub Actions release | gh release download TAG --repo GITHUB_REPOSITORY --pattern PATTERN --dir dist/npm | [rust-release.yml#L1788-L1799](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/rust-release.yml#L1788-L1799) | 无 --json；下载 npm release assets |
| GitHub Actions release | gh api repos/GITHUB_REPOSITORY/git/refs/heads/latest-alpha-cli -X PATCH -f sha=GITHUB_SHA -F force=true | [rust-release.yml#L1993-L2003](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/rust-release.yml#L1993-L2003) | 无 --json；修改 branch ref |
| Rusty V8 release | gh release view RELEASE_TAG --repo GITHUB_REPOSITORY --json isDraft --jq '.isDraft' | [rusty-v8-release.yml#L436-L451](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/rusty-v8-release.yml#L436-L451) | --json: isDraft；draft 存在时删除，否则拒绝替换已发布 release |
| Rusty V8 release | gh release delete RELEASE_TAG --repo GITHUB_REPOSITORY --yes | 同上 | 无 --json；删除未完成 draft release |
| Rusty V8 release | gh release create RELEASE_TAG --repo GITHUB_REPOSITORY --title RELEASE_TAG --verify-tag --prerelease ASSETS... | [rusty-v8-release.yml#L457-L479](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/rusty-v8-release.yml#L457-L479) | 无 --json；创建 prerelease |
| Python runtime build | gh release download RELEASE_TAG --repo GITHUB_REPOSITORY --pattern openai_codex_cli_bin-VERSION-*.whl --dir dist/python-runtime | [python-runtime-build.yml#L48-L57](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/python-runtime-build.yml#L48-L57) | 无 --json；下载 wheel |
| Python runtime build | gh release download RELEASE_TAG --repo GITHUB_REPOSITORY --pattern codex-package-*-unknown-linux-musl.tar.gz --dir dist/python-runtime-packages | [python-runtime-build.yml#L58-L61](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/python-runtime-build.yml#L58-L61) | 无 --json；下载 package archive |
| Windows release | gh release download TAG --repo github.repository --pattern PIN_NAME --pattern SOURCE_NAME --dir DIRECTORY | [rust-release-windows.yml#L194-L203](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/rust-release-windows.yml#L194-L203) | 无 --json；下载并随后校验输入 |
| Issue automation | gh issue list --repo REPO --json number,title,body,createdAt,updatedAt,state,labels --limit 1000 --state all --search "sort:created-desc" | [issue-deduplicator.yml#L24-L46](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/issue-deduplicator.yml#L24-L46) | --json: number,title,body,createdAt,updatedAt,state,labels；issue 去重输入 |
| Issue automation | 同上，--state open | [issue-deduplicator.yml#L170-L192](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/issue-deduplicator.yml#L170-L192) | fallback/pass 2，只查 open issues |
| Issue automation | gh issue view ISSUE_NUMBER --repo REPO --json number,title,body | [issue-deduplicator.yml#L48-L52](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/issue-deduplicator.yml#L48-L52), [#L194-L198](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/issue-deduplicator.yml#L194-L198) | --json: number,title,body；两个 pass 各自准备输入 |
| Issue automation | gh issue edit ISSUE_NUMBER --add-label LABEL ... | [issue-labeler.yml#L142-L147](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/issue-labeler.yml#L142-L147) | 无 --json；动态组装多个 label，失败被忽略 |
| Issue automation | gh issue edit ISSUE_NUMBER --remove-label codex-label | [issue-labeler.yml#L149-L153](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/issue-labeler.yml#L149-L153) | 无 --json；移除 trigger label，失败被忽略 |
| Issue automation | gh issue edit ISSUE_NUMBER --remove-label codex-deduplicate | [issue-deduplicator.yml#L414-L422](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.github/workflows/issue-deduplicator.yml#L414-L422) | 无 --json；移除去重 trigger label，失败被忽略 |

## 3. Detailed Findings

### 3.1 TUI 内置 PR 识别

实现文件是 [codex-rs/tui/src/branch_summary.rs](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs)。模块注释说明它负责 TUI 的 git-branch、pull-request-number 和 branch-changes 探测，并通过 workspace command executor 执行命令。逻辑是 best-effort：缺少 gh、未认证或命令失败时，PR 这一项可以隐藏，不会把它当成主流程失败。

实际顺序：

1. 当前分支查询：执行 gh pr view --json number,url,state。
2. JSON 能解析且 state 为 OPEN 时返回 number 和 URL。
3. 否则执行 git rev-parse HEAD。
4. 执行 gh repo view --json nameWithOwner,parent，建立仓库查询顺序。
5. 对 parent（如果存在）和当前仓库依次执行 commit-to-PR 的 gh api。
6. API 返回数组中第一个 state=open 的 PR 即返回；没有则认为没有可显示的 PR。

run_gh_command 会把传入 args 前加上 gh，CWD 设置为当前 workspace，并设置：

- GH_PROMPT_DISABLED=1
- GIT_TERMINAL_PROMPT=0

因此这条内置路径没有 gh pr list，也没有使用 GitHub API 的 GraphQL 命令。测试直接断言了上述 argv，见 [branch_summary.rs#L573-L643](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/codex-rs/tui/src/branch_summary.rs#L573-L643)。

### 3.2 babysit-pr 的实际运行时调用

实际执行器是 [.codex/skills/babysit-pr/scripts/gh_pr_watch.py](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py)。

命令包装器 gh_text 的行为：

- 默认构造 cmd = ["gh"]。
- 对非 api 命令且传入 repo 时，插入 -R OWNER/REPO。
- 对 api 命令不插入 -R，因为 endpoint 已经包含 repos/OWNER/REPO/...。
- 使用 subprocess.run(..., check=True, capture_output=True, text=True)。
- stdout 再由 gh_json 调用 json.loads。

因此脚本中的实际 argv 与 skill reference 文档中的简写略有不同：文档省略了运行时 wrapper 添加的 -R，而 API 命令不会有 -R。

一次 snapshot 的顺序在 [gh_pr_watch.py#L740-L767](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L740-L767)：

1. gh pr view 解析 PR。
2. gh api user 解析当前登录用户。
3. 分页读取 issue comments、inline review comments、review submissions。
4. gh pr checks 汇总 checks。
5. 按 head SHA 查询 workflow runs。
6. 对相关 runs 查询 jobs，并生成 failed_jobs。

review 相关调用都是读取，不是 comment/review 写操作。skill 的状态变更政策要求不要未经用户确认回复 human-authored review，但代码中没有 gh pr comment 或 gh pr review 的调用。

重跑逻辑在 [gh_pr_watch.py#L800-L852](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L800-L852)。它只对筛选出的 failed workflow runs 执行 gh run rerun RUN_ID --failed，然后更新本地 retry state。没有发现重跑失败后的备用 gh 命令。

### 3.3 Skill 文档调用与实际脚本调用的区别

babysit-pr/SKILL.md 要 agent 在分类 CI 失败时使用 gh run view、job logs endpoint 和 gh run view --log-failed。这些是给 agent 的操作指令，不是 watcher Python 文件中的 subprocess 调用。reference 也明确建议：

1. 先查具体 job 状态。
2. job 已失败时直接访问 job logs endpoint。
3. 整体 workflow 完成后，再把 gh run view --log-failed 作为 fallback。

references/github-api-notes.md 是事实性较强的命令清单，但其中 PR view 字段列表比当前 Python 代码少了 mergeable、mergeStateStatus 和 reviewDecision。对于实际兼容，当前脚本中的 pr_view_fields() 是更直接的运行时证据，见 [gh_pr_watch.py#L153-L157](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/babysit-pr/scripts/gh_pr_watch.py#L153-L157)。

codex-pr-body 的文档给出一个明确的 PR 识别示例：

~~~bash
git branch
gh pr view BRANCH --repo openai/codex --json number --jq '.number'
~~~

但它使用 may have to，意味着这是可能需要的 agent 操作，不是固定的内置调用。文档还说要用 gh 编辑标题和 body，却没有提供具体命令、参数或 JSON 字段。因此编辑动作必须列为 Unknown。

### 3.4 Review 与其他 skill 的静态结果

- code-review/SKILL.md 只要求在特定条件下添加 code-reviewed label，并明确不要未经请求留下 GitHub comments；没有给出 gh argv，也没有实现文件中的 gh 调用。见 [code-review/SKILL.md#L6-L14](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/code-review/SKILL.md#L6-L14)。
- update-v8-version/SKILL.md 只说可使用 GitHub check tooling 或 gh 检查 canary，没有具体命令。见 [update-v8-version/SKILL.md#L28-L33](https://github.com/openai/codex/blob/a8964cb1bad67bc26a826fb07d1bef99c6a3f008/.codex/skills/update-v8-version/SKILL.md#L28-L33)。
- babysit-pr 的 resolve、comment/review 等状态变更描述没有对应的 gh pr 写命令。静态证据只能支持其读取三类 review endpoint，不能扩展为 comment/review mutation。

### 3.5 仓库 CI/release 调用不应混入 Codex PR 依赖

.github/workflows 中的 gh release ...、gh issue ... 和 release API patch 是 GitHub Actions 在仓库 CI 环境中执行的。.github/scripts、scripts/stage_npm_packages.py 和 sdk/python/_runtime_setup.py 的调用是发布/安装辅助逻辑。它们说明仓库本身依赖 gh 的其他能力，但不能说明 Codex agent 在 PR 工作流中会调用这些命令。

类似地，下面这些匹配不算 CLI 调用：

- softprops/action-gh-release、pypa/gh-action-pypi-publish 等 action 名称。
- GH_TOKEN、GITHUB_TOKEN、GH_HOST 等环境变量。
- codex-rs/prompts/templates/permissions/approval_policy/on_request.md 中的 ["gh","pr","check"]。这是审批前缀示例，不是执行路径。
- 普通文本、注释、错误信息或文档中仅提到 gh 的行。

## 4. Fallback Paths

### 4.1 TUI 当前分支 PR 识别

~~~mermaid
flowchart TD
    A["gh pr view --json number,url,state"] -->|成功且 OPEN| B["返回 PR number/url"]
    A -->|失败、解析失败或非 OPEN| C["git rev-parse HEAD"]
    C --> D["gh repo view --json nameWithOwner,parent"]
    D --> E["遍历候选仓库：parent -> current"]
    E --> F["gh api .../commits/HEAD_SHA/pulls"]
    F -->|第一个 open PR| B
    F -->|当前候选失败或无 open PR| G["继续下一个候选"]
    G -->|还有候选| E
    G -->|没有候选| X
    D -->|失败| X["无可选 PR 元数据"]
~~~

这里没有以下 fallback：

- 没有 gh pr list。
- 没有在 gh pr view 失败后改查 gh repo view 的 PR 字段。
- 没有用 gh api 查询 branch refs 或 GraphQL。
- 没有按 commit SHA 之外的第二种 API 过滤方式。

### 4.2 babysit-pr 的 PR 解析

babysit-pr 对 gh pr view 的 fallback 很有限：

- --pr auto、数字和 URL 都最终走一次 gh pr view。
- 如果 gh pr view 失败、返回非 JSON 或缺少必要字段，脚本直接报错。
- 成功返回后，repo 的确定顺序是 repo_override、PR URL、headRepository/headRepositoryOwner。
- repo 推断失败也直接报错；没有 gh repo view 或按 commit 的备用查询。

成功解析后，checks、review feed 和 Actions 查询是并列的后续步骤，不是 PR view 的替代路径。review endpoint 使用 Python 手工分页；Actions runs/jobs 只请求 per_page=100，源码没有 --paginate。

### 4.3 CI 日志诊断

skill 文档规定的日志路径是：

1. gh run view RUN_ID --json ... 查看 run 概况。
2. gh api .../actions/runs/RUN_ID/jobs 找到具体失败 job。
3. gh api .../actions/jobs/JOB_ID/logs 直接取 job 日志。
4. 如果整体 workflow 已完成，gh run view RUN_ID --log-failed 可作为 fallback。

这条链是文档给 agent 的行为约束；实际 watcher 只负责提供 failed job ID 和 logs endpoint，不执行日志下载命令。

### 4.4 codex-pr-body 的 PR 识别

普通 Git 路径的文档 fallback 是 git branch 加 gh pr view BRANCH --repo ... --json number --jq ...。Sapling 路径改用：

~~~bash
sl log --template '{github_pull_request_url}' -r .
~~~

以及 sl sl 的输出，不再依赖 gh。PR body/title 的实际编辑命令没有写出。

## 5. Unknowns

以下项目仅凭当前仓库静态源码不能确认：

1. 桌面 Codex App 是否调用 codex-rs/tui/src/branch_summary.rs 中的逻辑，或者使用另一套内部 GitHub 集成。
2. codex-pr-body 编辑标题和 body 的具体 gh subcommand、参数、是否使用 --json、是否先查询 body 的完整字段。
3. code-review 中添加 code-reviewed label 的实际实现方式，是否通过 gh、GitHub connector 或其他工具。
4. update-v8-version 中 “GitHub check tooling or gh as appropriate” 的实际 argv。
5. Codex agent 在用户明确要求时是否会执行 gh pr create、gh pr comment、gh pr review、gh pr merge、gh pr list 或 gh pr edit。这些可以是用户驱动的任意 shell 操作，但不属于当前仓库能静态证明的 Codex 固定依赖。
6. babysit-pr 文档列出的 gh run view 命令在真实 agent 回合中的调用频率、是否带 --repo/-R、以及 agent 如何解析其输出；仓库只有命令示例，没有对应的执行代码。
7. 不同 gh 版本、认证状态、GH_HOST 或企业 GitHub 环境下，命令输出和错误的全部变体。TUI 只明确关闭了 prompt，watcher 没有统一设置这些环境变量。

这些 Unknown 需要后续运行时抓取或在目标部署环境中做动态观测，不应在网关实现阶段被静态猜测成兼容需求。

## 6. Proposed MVP Command Set

下面只根据已经观察到的 Codex PR 工作流提出第一版兼容范围，不包含 API 映射设计。

### 第一版必须覆盖

1. gh pr view
   - 当前目录隐式识别当前分支。
   - 显式 PR number 或 PR URL。
   - --json 字段至少覆盖实际脚本使用的并集：number,url,state,mergedAt,closedAt,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeable,mergeStateStatus,reviewDecision。
   - 支持 TUI 不带 repo 参数，以及 skill script 使用的 -R/--repo 形态。
2. gh repo view --json nameWithOwner,parent，用于 fork/upstream fallback。
3. gh api repos/OWNER/REPO/commits/HEAD_SHA/pulls，包括 Accept: application/vnd.github+json header 形式。
4. gh pr checks --json name,state,bucket,link,workflow,event,startedAt,completedAt。
5. gh api repos/OWNER/REPO/actions/runs，支持 head_sha 和 per_page 参数。
6. gh api repos/OWNER/REPO/actions/runs/RUN_ID/jobs，支持 per_page。
7. 三类 PR review 读取 endpoint：
   - issues/PR_NUMBER/comments
   - pulls/PR_NUMBER/comments
   - pulls/PR_NUMBER/reviews
   并支持 per_page、page 查询参数。
8. gh api user。
9. gh run rerun RUN_ID --failed。

### 第一版可选的 CI 诊断能力

- gh run view RUN_ID --json jobs,name,workflowName,conclusion,status,url,headSha
- gh api repos/OWNER/REPO/actions/jobs/JOB_ID/logs
- gh run view RUN_ID --log-failed
- gh pr view BRANCH --repo openai/codex --json number --jq '.number'

### 不应从当前静态证据直接加入的命令

gh pr list、gh pr create、gh pr comment、gh pr review、gh pr merge 和具体的 gh pr edit 不应被标成当前 Codex 固定依赖。它们只有在后续动态抓取确认，或产品范围明确要求支持 agent 任意 GitHub PR 操作时，才应单独加入兼容范围。

同理，gh release ...、gh issue ...、npm/R2/V8 发布命令不属于 Codex GitHub PR workflow 的 MVP。

本阶段到此为止；没有进行任何网关实现，也没有展开 GitHub/Gitea API 字段映射。
