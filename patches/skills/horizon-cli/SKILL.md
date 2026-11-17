---
name: horizon-cli
description: Horizon 项目根下的 horizon-cli 参考，覆盖契约发现（capability/workflow/graph/application/page）、schema 读取、流程校验、生成与质量门禁、环境与服务命令，以及二进制缺失时的回退方式。
whenToUse: 需要列出或读取 Horizon 契约、运行 check-entities / generate / check-generated / ci-fast、检查环境或管理开发进程时。
---

# horizon-cli

Horizon 项目的契约发现与工程门禁入口。命令在**目标项目根目录**执行，读取的是该仓库当前工作树注册的能力、流程、Graph 和 Runtime 页面。

## 定位与回退

| 场景 | 命令 |
|---|---|
| macOS / Linux | `./horizon-cli <命令>` |
| Windows PowerShell | `.\horizon-cli.exe <命令>` |
| 二进制缺失、过期或定义未重新构建 | `go run ./foundation/cmd/horizon-cli <命令>` |
| 工具链隔离脚本可用 | `./scripts/go.sh run ./foundation/cmd/horizon-cli <命令>` |
| 需要固定版本 | `./horizon-cli build-cli` |

- 从仓库根构建：`go build -trimpath -o horizon-cli ./foundation/cmd/horizon-cli`。
- 工具链脚本仓库可用时，`./scripts/go.sh run ./foundation/cmd/horizon-cli <命令>` 直接读取当前工作树的定义。
- 不要用其他项目的二进制或编译快照冒充目标项目；装配或编译失败时报告目标项目的错误。
- 执行前先看 `./horizon-cli --help` 或 `./horizon-cli commands --json`，不要猜子命令。

## 契约发现

分析 Capability、Workflow、Graph、Runtime Application 或 Page 前，先用 CLI 发现已有契约，再读源码核对语义。

| 命令 | 作用 |
|---|---|
| `./horizon-cli capability` | 列出 Capability 摘要 |
| `./horizon-cli workflow` | 列出 Workflow 摘要 |
| `./horizon-cli graph` | 列出 Graph 摘要 |
| `./horizon-cli application` | 列出 Runtime Application |
| `./horizon-cli page` | 列出 Runtime Page |
| `./horizon-cli <类型> --full` | 批量返回完整定义 |
| `./horizon-cli <类型> --json` | 以 JSON 输出（默认也是 JSON） |
| `./horizon-cli catalog` | 汇总目录 |
| `./horizon-cli search <关键词>` | 按名称或说明检索 |
| `./horizon-cli <类型> get <id>` | 读取单个条目 |
| `./horizon-cli refs capability:<id[@版本]>` | 查询引用关系 |
| `./horizon-cli validate workflow <文件>` | 校验流程规格 |

省略标识的 `<类型> schema` 返回该类型说明与结构 JSON Schema；带标识时同时返回注册定义及其输入输出、流程节点或页面绑定：

```sh
./horizon-cli capability schema article.get@1.0.0
./horizon-cli workflow schema article-publish
./horizon-cli graph schema
./horizon-cli application schema articles@2.0.0
./horizon-cli page schema articles/article.detail@2.0.0
```

- 输出是 JSON。应用或页面存在多个版本时用 `id@版本` 指定。
- 目录描述源码中的配置；租户发布版本、执行方案绑定和账号权限以运行环境为准。
- `schema` 描述类型结构，`entry.definition` 是注册定义；运行时还会检查引用、权限与业务语义。业务描述缺失时以所属模块源码为准。

## 工程门禁

`./horizon-cli commands` 列出全部工程子命令。常用：

| 命令 | 用途 |
|---|---|
| `check-entities` | 实体声明与迁移的一致性 |
| `generate` | 从源生成 Goa/OpenAPI、sqlc、Orval SDK、Runtime spec |
| `check-generated` | 生成物与源的一致性检查 |
| `check-workflows` | 流程规格检查 |
| `check-rules` | 项目规则、Skills、引用与门禁路由检查 |
| `ci-fast` | 无需数据库与浏览器的快速门禁 |
| `test` / `lint` / `typecheck` / `build` | 单元与 HTTP 测试、ESLint + Go vet、类型检查、构建 |
| `test-integration` | 独立 PostgreSQL schema 上的集成测试 |
| `test-security` | 认证、授权与数据范围负向用例 |
| `test-orchestration-example` | 编排示例回归 |
| `test-mcp` / `test-mcp-e2e` / `test-mcp-native-e2e` | MCP 链路专项 |
| `test-webmcp-e2e` | WebMCP 专项 |
| `test-e2e <spec>` | 只跑指定 Playwright spec |
| `test-e2e-changed [--dry-run]` | 按改动映射选择 spec |
| `test-e2e-parallel`、不带参数的 `test-e2e`、默认 `ci` | 全量 E2E，需用户当轮明确授权 |
| `db-up` / `db-down` / `migrate` / `bootstrap` | 本地依赖、迁移与初始数据 |
| `install` / `dev` / `dev-api` / `tools` | 依赖安装、开发进程与工具 |

按批次执行：连续开发累计受影响模块，阶段验收跑一次 `ci-fast`；表结构变化加 `test-integration`；E2E 按项目现行规则只跑改动模块映射命中的单个 spec，全量集合先取得授权。

## 环境与服务

```sh
./horizon-cli env check [all|frontend|backend]   # 工具链、依赖、端口与数据库连通性
./horizon-cli env configure                       # 交互写 .env
./horizon-cli status [all|frontend|backend] --json
./horizon-cli start | stop | restart [all|frontend|backend]
```

- 服务命令只管理 CLI 启动的进程，日志与进程记录在 `.local/horizon-cli/`。
- `start`/`stop`/`restart` 按用户授权执行，并核对所选项目与目标。
- 目录查询可在前后端和 PostgreSQL 都停止时执行。
- 进程环境变量优先于 `.env`；`REDIS_URL` 可选。

## 新项目初始化

```sh
horizon-cli init
horizon-cli init my-project
horizon-cli init --repo <url-or-path> my-project
```

`init` 把仓库克隆到当前目录下（默认 `horizon/`），目标目录必须不存在，父目录必须存在。初始化用的 CLI 与目标项目自己的 CLI 是两件事：克隆后从新项目根使用它自己的 CLI。

## 边界

- CLI 的离线查询、校验和门禁不等于运行期执行；业务调用仍走项目已有的 Capability Gateway，遵循身份、绑定、审批与幂等语义。
- 不要把 `schema` 或目录输出当成业务安全证明；权限、租户与数据范围以服务端实现和测试为准。
- 跳过或失败的检查要如实报告；缺少数据库或浏览器导致的跳过不是通过。
