# HorizonStudio

HorizonStudio 是 Web 端的 Horizon 项目开发工作台。Horizon 主站提供业务页面和登录，Horizon 基座通过 Capability Gateway 提供业务能力，DSH 提供 Agent 会话。

本仓库是一份完整的 Horizon 基座 checkout，Studio 业务全部在 `applications/studio/` 内实现；上游 Horizon 的通用文档见 `docs/` 与根 README（已归档于本文末尾）。

## 架构

HorizonStudio 工作台位于 `apps/web/src/features/studio`，通过主站的 `/studio` 路由进入。主站负责登录、会话和导航。Studio 使用当前 Horizon 会话通过 Capability Gateway 调用 `applications/studio` 的 `studio.*` 能力。DSH Web 负责本地 Agent 对话与任务绑定会话。

## 本地运行（macOS / Linux）

```sh
pnpm install --frozen-lockfile
docker compose up -d --wait postgres
make migrate
make dev-api             # API http://127.0.0.1:8080
pnpm dev                 # 应用 http://127.0.0.1:5173/studio
```

打开 [HorizonStudio](http://127.0.0.1:5173/studio)，用 Horizon 主站账号登录。Studio 入口需要 `studio:read` 权限。

要在工作台中使用本地 Agent 会话，准备并启动固定版本的 DSH：

```sh
pnpm bootstrap
pnpm prepare:web
pnpm web                  # DSH Agent http://127.0.0.1:3095
```

在 DSH 新会话的预设菜单中选择「Horizon 开发」。从 Studio 的项目、需求或事项创建 Agent 会话时会直接使用该预设。它保留 DSH Standard 的工具和计划模式，并加入 [Horizon 开发指令](patches/horizon-development.md)：需求分析与 AI 嵌入评估、能力点最小化与 Capability Gateway 的契约模型/执行语义、业务流程经 Workflow/Graph/ADK 编排、发现→需求分析→规划→实现→验证→汇报的开发模式、生成物与门禁规则，以及目标项目根下 `horizon-cli` 的用法。预设还内置四个按需加载的 Skill：[horizon-requirement-analysis](patches/skills/horizon-requirement-analysis/SKILL.md)（业务过程还原、AI 嵌入点与失败语义）、[horizon-cli](patches/skills/horizon-cli/SKILL.md)（子命令与回退）、[horizon-capability](patches/skills/horizon-capability/SKILL.md)（契约字段、风险/审批/幂等与验证）、[horizon-workflow](patches/skills/horizon-workflow/SKILL.md)（Step、分支、预算、状态机与执行方案）。修改预设后，重新执行 `pnpm prepare:web` 并重启 Web 工作台。

### Studio 会话与 MCP

会话栏默认按需求分组，点开需求即可查看或新建其会话。切换到「普通」可查看项目内全部 Studio 会话，包括没有关联需求的会话。现有浏览器绑定会在有写权限的用户打开对应会话时同步到后端。

首次启动先应用数据库迁移，启动 Horizon 后端，并执行 `pnpm prepare:web` 和 `pnpm web`。在本机登录时，Studio 使用本次输入的密码自动完成 DSH 配对。已登录时，也可从 Studio 工具栏的「连接 MCP」配对。Studio 会把 30 天凭据保存到本机 DSH，DSH 会自动连接。该 DSH 实例使用配对时的账号和部门查询 Studio 数据。过期后在同一入口重新配对。

手工设置时，也可以在「MCP」→「远程 MCP」创建同一账号和部门的 Token，并在 DSH 本机凭据中设置 `HORIZON_STUDIO_MCP_TOKEN`。DSH 的 `studio` MCP 提供会话上下文读取和附件内容读取两个只读工具。

`mcp__studio__get_session_context` 接受 DSH `sessionId`，通过 Horizon `/mcp` 读取当前任务、项目开发说明和关联需求；关联需求会附带附件的 `fileId`、文件名、类型和大小。`mcp__studio__read_attachment` 按 `fileId` 读取附件内容（base64，需要 `files:read`；受远程 MCP 结果大小限制，过大的文件返回 `attachment_too_large`）。适配器每次读取时识别 Horizon CLI 管理的后端地址，端口变更后无需重配 DSH。手工启动后端时使用 `.env` 的 `API_ADDR`，也可用 `HORIZONSTUDIO_API` 显式覆盖。

环境变量：

| 变量 | 说明 |
|---|---|
| `HORIZONSTUDIO_API` | 手工覆盖 Studio MCP 使用的本机 Horizon 地址 |
| `HORIZONSTUDIO_HOME` | Studio 数据根目录（会话绑定等 DSH 侧数据），默认 `.local/studio-home` |
| `HORIZONSTUDIO_CLI` | 创建新项目使用的 horizon-cli，默认 `resources/horizon-cli/horizon-cli` |

`resources/horizon-cli/` 由本仓库源码构建（`go build -trimpath -o resources/horizon-cli/horizon-cli ./foundation/cmd/horizon-cli`），`source.json` 记录 sha256，`pnpm test` 会校验。

## 验证

```sh
pnpm test               # CLI 清单/目录 + 代理映射测试
make check-entities     # 实体声明与迁移一致性（当前 0 问题）
go build ./... && go test ./applications/studio/
```

浏览器 E2E 按仓库现行规则暂停；远程 CI 全量执行前需单独授权。

## 在 Studio 中使用

创建项目时填写目标绝对路径及可选 Git 来源（`horizon-cli init`）；已存在的 Horizon 项目可直接关联。开发服务控制、目录巡检和 Gateway 调用都通过目标项目根目录自建的 `horizon-cli` 执行。试跑目标项目流程时使用目标项目账号登录（`gateway.login`），调用按幂等键记录在 `studio_runs`。需求详情支持附件：上传复用共享文件服务（需要 `files:create`），下载复用 `file.content.read`（需要 `files:read`）；移除附件只解除需求绑定，文件仍保留在文件库。
