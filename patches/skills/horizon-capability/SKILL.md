---
name: horizon-capability
description: Horizon 能力契约与执行参考，覆盖 Definition 字段、Layer/Kind/Effect、风险与来源策略、审批与幂等键、TxExecutor 与 Outbox 事件、复用与拆分判据，以及能力入口的验证用例。
whenToUse: 新增或修改 Capability、调整权限/风险/审批/幂等/事件、设计能力组合与拆分，或排查调用被拒、重放与审批恢复问题时。
---

# Horizon 能力契约与执行

能力（Capability）回答“允许做什么”，是唯一业务契约；Execution Plan 回答“这个能力版本如何执行”；Binding 是“此范围的新调用使用哪个已发布方案”。三者版本独立，注册了能力或方案不等于业务已经进入相应引擎。

## 1. 先发现与拆分

1. 查实际 `Entries()`、共享装配目录、Definition、实体、成功事件、流程定义与测试；按动作、对象和语义搜索，不只匹配名称。
2. 核对输入输出 Schema/版本、权限与数据范围、风险/审批、幂等、事务/事件、副作用、敏感字段与适配器允许集合。名称相近不等于语义相同，存量绕过 Gateway 或范围过宽的实现不是模板。
3. 选择顺序：直接复用 → 经 Gateway 组合 → 在原模块兼容扩展并维护全部消费者 → 新增（说明候选为何不适用）。

原则：能力点只做最小化能力（单动作、单事务、可独立授权与复用），业务流程通过 Workflow、Graph、ADK 实现；不要把流程塞进一个能力，也不要为复用而合并多个业务步骤。

| 情况 | 结论 |
|---|---|
| 同一完整动作被页面 / Agent / API 复用 | 仍是同一能力，不拆两层 |
| 子操作被多个用例独立复用，或有独立权限/风险 | 拆成原子能力 + 组合能力 |
| 纯读取聚合、字段裁剪、投影（无业务流程语义） | 同步组合能力（`Layer=orchestration`，Go Executor 内经 Gateway） |
| 多步骤业务流程（顺序、分支、人工/外部等待） | 拆成最小原子能力，由 Workflow/Graph/ADK 编排 |
| 多个写必须全成全败 | 合并为一个原子能力，`Entities` 声明全部对象，共享一个事务 |
| 需要等待人工或外部结果并跨请求继续 | 持久化 Workflow，不靠 Go 调用栈 |
| 纯计算、DTO 转换、纯透传 | 不注册为能力 |

## 2. Definition 速查

注册点在所属模块 `Entries()`，`registry.Entry{Definition, Handler}`；Definition 与实现放在一起，避免声明与执行器漂移。

| 字段 | 说明 |
|---|---|
| `Name` / `Version` | `Name` 用 `<domain>.<object>.<action>`，不编码 HTTP/MCP；改语义或结构即新版本 |
| `Title` / `Description` / `Domain` / `Entity` | 目录与审计展示；`Entity` 是主对象 |
| `Layer` | 必填：`atomic`（业务单元）或 `orchestration`（组合/入口） |
| `Kind` | `service` / `sql` / `command` / `external` / `event`；缺省按 `Executor` 前缀推断 |
| `Reads` / `Writes` / `Effect` | 包括子调用在内的业务影响；`Effect` 取 `none`/`compensable`/`irreversible`，只读默认 `none`，写默认 `irreversible` |
| `ReadOnly` / `RetrySafe` | 只读自动 `RetrySafe`；写能力不能声明 `Effect=none` |
| `Entities` | 读写到的全部业务对象，主对象在前；跨实体能力必须声明 |
| `Input` / `Output` | JSON Schema 契约，Gateway 执行前校验；输出投影为对象 |
| `InputAccess` / `OutputAccess` | 按字段路径追加权限（`FieldAccess{Path, Permission}`） |
| `Permission` | 操作授权码，服务端以可信身份校验 |
| `Risk` / `Approval` | `low`/`medium`/`high`/`critical` 与 `auto`/`confirm`/`four-eyes`/`forbidden` |
| `Idempotency` | 写能力通常 `Required: true`，`Scope` 如 `tenant+operation+key` |
| `SuccessEvents` | 声明执行器必须产出的事件类型，注册时校验、目录可预览闭环 |
| `Loggable` | 允许进入审计投影的输入字段，其余只保留摘要 |
| `AllowedOrigins` | 声明允许的来源；未声明的适配器调用会被拒绝（Gateway 仍另行判权） |
| `TransientResult` | 结果不落库、不重放；写能力要求 atomic + `manual-only` 且 `Approval=auto` |
| `Tags` | `manual-only`、`system-only`、`parent:<cap@ver>` 等入口约束 |
| `Executor` | 形如 `go:<pkg>.<action>`；Handler 设 `Execute`（非事务）或 `ExecuteTx`（Gateway 事务），二选一 |

## 3. 注册与实现

```go
func (m *Module) Entries() []registry.Entry {
	return []registry.Entry{{
		Definition: contracts.Definition{
			Name: "article.submit", Version: "1.0.0",
			Layer: contracts.LayerAtomic, Domain: "articles", Entity: "article",
			Permission: "articles:write", Risk: contracts.RiskMedium, Approval: contracts.ApprovalAuto,
			Input:  contracts.Object(map[string]*contracts.Schema{"id": contracts.Int(1, 1<<53)}, "id"),
			Output: contracts.Object(map[string]*contracts.Schema{"id": {Type: "integer"}}, "id"),
			Idempotency:   contracts.Idempotency{Required: true, Scope: "tenant+operation+key"},
			SuccessEvents: []string{"article.submitted"}, Loggable: []string{"id"},
		},
		Handler: contracts.Handler{ExecuteTx: m.submit},
	}}
}
```

- 写能力用 `TxExecutor`：业务数据与 Outbox 同一次提交；不要先提交业务数据再补写事件。
- 读能力用 `Executor`；跨实体读取在 `Reads`/`Entities` 声明真实影响。
- 事务边界就是能力边界；多个写必须同一事务时合并为一个能力，不能用多次 Gateway 调用拼假原子事务。

## 4. 执行语义

调用方构造 `gateway.Request{Name, Version, Input, IdempotencyKey, Origin, CorrelationID, CausationID, TriggerDepth}`。Gateway 依次：

1. 解析可信 Principal；`Origin` 由适配器决定，从不取客户端字段。
2. 校验来源与 `AllowedOrigins`、`manual-only` / `system-only` / `parent:<cap@ver>` 标签、权限、`Input` Schema、字段权限。
3. 声明 `Idempotency.Required` 的能力必须带键；重放校验账号、输入摘要与能力版本，命中直接返回原结果。
4. 由 `policy.Admit` 决定直接执行还是先建审批卡。
5. 执行 Handler；`TxExecutor` 在事务内写业务数据与 Outbox，提交后 Relay 投递事件。

风险与来源策略（`policy.Admit`）：

| Risk / Approval | 人工入口 | 自动化（agent / workflow / event / api / capability） |
|---|---|---|
| `auto`（含低/中风险） | 直接执行 | 直接执行 |
| `high` 且未声明 Approval | 直接执行 | 按 `confirm` 处理，需人工确认 |
| `confirm` | 直接执行 | 需人工确认 |
| `four-eyes` | 需另一名具备 `capability:approve` 的账号批准 | 同左 |
| `critical` | `four-eyes` | `forbidden` |
| `forbidden` | `forbidden` | `forbidden` |

- `OriginApproval`（审批恢复）按 `auto` 执行，因为审批存储已重新校验申请人权限。
- 嵌套调用只能来自执行器内部：Gateway 强制 `OriginCapability`、深度 +1、correlation 透传、causation 指向父 invocation；`MaxDepth=8`，超出拒绝。
- `TxExecutor` 内再次 `Invoke` 会被拒绝；组合能力用非事务 Executor，每次 `Invoke` 后同时检查 `err` 与 `result.Status`。
- `failed` 可能是业务结果（`conflict`、`forbidden`、`unavailable` 等）；把 `awaiting_approval` 当普通数据继续是错误，必须交给持久化中断与恢复。
- 审批用 `Gateway.Decide(id, approve, note)`：校验卡片状态、过期与四眼，按原载荷执行；不重新让模型判断。
- 常用错误码：`invalid`、`unauthenticated`、`forbidden`、`not_found`、`conflict`、`unavailable`、`timeout`、`rate_limited`、`step_limit`、`branch_unmatched`、`cancelled`；按语义选择，不要一律 `internal`。

## 5. 事件与 Outbox

- 业务事件是事实：只有真正完成写入的原子能力在 `Output.Events` 中产出，并与 `SuccessEvents` 一致；外层不得重复声明同一事实。
- 事件携带 `correlation`/`causation`/`depth`；Trigger 去重并限制 `maxDepth`，AI 任务与调用用关联标识追踪。
- 外层可以发布自己独有的流程事件（如 accepted/cancelled），但不得表述成“业务步骤全部成功”，也不得声称流程与事件原子提交。
- 既有的 best-effort 提交后写事件路径保持原有边界，改写时不要静默升级或降级一致性声明。

## 6. 入口与暴露

| 入口 | 约束 |
|---|---|
| 页面 | 使用生成的 capability SDK，不裸调业务 HTTP |
| Agent / MCP | 固定版本允许列表；写工具声明非只读并走审批；注册能力不等于已授权暴露 |
| Workflow 节点 | 只允许 atomic 能力，禁止向上递归编排 |
| 事件 / 外部 API | 由可信适配器声明来源，沿用同一授权、幂等与审批语义 |
| Execution Plan 管理 | `execution.plan.*` 为 `manual-only`，只给人工管理入口 |

## 7. 验证

- 用例：允许与拒绝、跨租户与范围外 ID、幂等重放与并发、审批恢复、事件投影、字段权限与敏感字段。
- 位置：能力模块测试、`foundation/tests/capability_modules_test.go`、`TestCapability*`；真实 PostgreSQL 用隔离 schema。
- 命令：`./horizon-cli check-entities`、`check-rules`、`ci-fast`；触及 SQL/审批/恢复加 `test-integration`，安全边界加 `test-security`。
- 不要把「注册成功」「目录可见」当作调用者已获授权或业务已执行；LLM 判断不能替代确定性策略。

## 8. 反模式

- 页面/API/Agent handler 直连 `dbgen` 或连接池，绕过 Gateway。
- 能力内部 `registry.Resolve` 后直接调 Handler，或事务执行器内互调。
- 用随机数或时间戳当同一逻辑动作的幂等键；重试生成新键导致重复副作用。
- 同名兼容修改旧契约而不升版本，或为 UI/Agent/MCP 各写一份实现。
- 把 `failed` 当异常一律转 `internal`，或把 `awaiting_approval` 当成功继续。
