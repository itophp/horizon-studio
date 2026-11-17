---
name: horizon-workflow
description: Horizon 持久化编排参考，覆盖 Workflow/Graph/ADK 选型、Step 字段与条件分支、依赖与读写冲突、预算上限、状态机与检查点、审批中断与恢复、执行方案与 Binding，以及验证命令。
whenToUse: 设计或修改 Workflow/Graph/ADK 流程、排查依赖冲突/分支/恢复问题，或创建、发布、绑定 Execution Plan 时。
---

# Horizon 流程编排

## 1. 选型

能力点只做最小业务动作；业务流程用编排实现，不要塞进一个能力，也不要用 Go 顺序调用替代编排。编排内部再按下面选择：

| 条件 | 选择 | 验收证据 |
|---|---|---|
| 单个业务动作（单事务、单对象，可独立授权与复用） | 原子能力，Direct 执行 | Gateway 调用与事务测试 |
| 多步骤业务流程（顺序、分支、人工/外部等待、跨请求恢复） | Eino Workflow 主干 | 版本化定义真实启动、等待期持久化、重建后续接原结果 |
| 流程内循环、有界轮询、多轮复核 | Eino Graph 子图 | 退出条件、重试分类、步数上限测试 |
| 流程内需要模型在受限工具间决策 | Eino ADK Runner / ADK 节点 | 固定版本工具白名单 ∩ 当前权限、迭代预算、拒绝用例 |
| 流程中固定输入、一次分类或提取 | 直接模型调用 | 结构化输出校验；不得宣称使用 ADK |

`ai_runs` 只是生命周期记录，不证明使用了 ADK；`Layer=orchestration` 只说明注册层，不等于业务流程已经进入编排引擎。用户明确要求 Agent 时，不能悄悄退化为单次模型调用。

## 2. 定义与 Step

v1 流程由 Go 代码声明，`Name + Version` 唯一，通过 `orchestration.New` 的 definitions 接入；运行固定定义 hash 与能力版本。

| 字段 | 说明 |
|---|---|
| `ID` | 步骤标识，独立于能力名，同一能力可重复使用 |
| `Capability` / `Version` | 固定版本原子能力 |
| `DependsOn` | 显式顺序；无依赖的只读步骤由 Workflow 原生并行执行 |
| `Input` | 字段映射：`From=input` 表示流程输入，否则是前驱步骤 ID；目标字段缺失即失败 |
| `Constants` | 固定参数 |
| `When` | `{from, field, equals}` 条件分支 |
| `Poll` | 只读轮询：`DoneField`、`MaxSteps` |
| `MaxAttempts` | 只读瞬态重试上限 |
| `Agent` | ADK 节点：`Instruction`、`Tools`（固定版本白名单）、`MaxIterations`、`Output` |
| `Compensation` | 可补偿步骤的补偿能力、版本与映射 |
| `Graph` | 引用已注册 Graph 规格（`ref` + 64 位 hash） |
| `CallMode` / `PlanID` / `PlanVersion` | 执行方案引用，仅替代方案使用 |

结构未支持时报告缺口，不要用伪造节点或滥用风险审批顶替业务审核。

## 3. 依赖、冲突与分支

- 顺序由 `DependsOn` 与输入映射确定；`Reads`/`Writes` 冲突却没有指向顺序时编译失败，不根据“谁写谁读”猜顺序。
- 同一能力多次出现时用不同步骤 ID；普通能力输出投影为对象，当前不支持数组/标量直接作为步骤输出。
- 仅缺输入字段的读取不构成依赖，但显式顺序仍以 `DependsOn` 表达。
- 分支组：同一 `(from, field)` 的步骤必须使用互不相同的 `equals`；最多命中一个，未选中分支被标记 skipped 并沿 END 传播，不产生任何能力调用。
- 一个都不命中时返回 `branch_unmatched` 并使运行失败，不静默成功。
- 分支目标只允许该分叉作为控制前驱；根输入以数据依赖接入（`WithNoDirectDependency`），否则会绕过分支跳过判定。
- 分支来源字段必须在其输出 Schema 中声明（能力 `Output` 或 `agent.output`）；未声明结构化输出的 Agent 不能作为分叉源。
- 同组互斥步骤之间不要求排序，读写冲突检查对它们豁免；与组外步骤的冲突仍需显式排序。
- 分叉只读已落库的归一化事实（例如记录后的 `decision`），不读模型自由文本；模型只能提供原因码。

## 4. 预算与上限

| 项 | 上限 |
|---|---|
| 单流程步骤数 | `MaxWorkflowSteps = 64` |
| 只读步骤重试 | `MaxAttempts ≤ 5` |
| Graph 轮询步数 | `Poll.MaxSteps ≤ 100` |
| ADK 迭代 | `MaxIterations ≤ 20` |
| 调用栈深度 | `MaxDepth = 8`（Graph 循环不累加调用栈深度，另受 `MaxRunSteps` 限制） |
| 单份明文检查点 | 4 MiB |
| Graph 预算 | `MaxModelCalls ≤ 8`、`MaxToolCalls ≤ 16`、`MaxOutputTokens ≤ 2048`、`MaxContextBytes ≤ 32768`、`TimeoutMillis ≤ 30000` |

写能力不因 TxExecutor 或 Effect 被自动重试；权限、Schema、业务冲突不自动重试。

## 5. 状态机与持久化

- 运行头沿用 `ai_runs`，业务调用轨迹沿用 `cap_invocations`，引擎状态在 `orch_checkpoints`（加密，密钥绑定 tenant/run/key；无密钥启动失败）。
- 状态：`running → awaiting_approval → running/awaiting_approval → completed/failed/cancelled`；恢复期间可继续显示 awaiting_approval，直到引擎给出下一稳定状态。
- 启动能力用基础设施事务写运行头、manifest 与 accepted Outbox，事务内不执行任何业务节点。外层 invocation 的 `succeeded` 只表示“已受理”，真实结果看 `ai_runs`。
- 工作器按秒扫描，SQL 条件更新领取 90 秒租约，单次运行时间片 60 秒；租约到期可重新领取。
- run/get/cancel 始终要求 tenant + account；管理员不能凭 runId 读取他人的输入、状态或检查点。
- 取消阻止后续步骤并拒绝未执行的审批；已执行或已进入执行的动作不回滚。
- 运行入口能力：`orchestration.workflow.list`/`get`/`diagram`（`orchestration:read`）、`run`/`cancel`（`orchestration:run`/`cancel`）。`run` 高风险，Agent/API/MCP 必须人工确认并要求幂等键；受理立即返回 `runId`，受理与运行成功分开。目录读取不代表节点内能力已授权，节点执行时再授权。

## 6. 审批中断与恢复

1. 节点先持久化输入，再调用 Gateway。
2. Gateway 返回 `awaiting_approval`；节点保存 invocation 回执并发出 Eino Interrupt，引擎保存检查点。
3. `Gateway.Decide` 用 `PrincipalForTask` 重建申请人身份，执行原载荷。
4. 工作器重新读取申请人身份，恢复相同引擎检查点。
5. 节点读取原 invocation 的已完成结果，后继步骤继续。

- ADK 写工具在工具中断处保存包含 `toolCallID` 的检查点，下个时间片才执行写入；内部中断保持 `running`，业务审批中断显示 `awaiting_approval`。
- 恢复不依赖模型重新规划；已批准动作用原 invocation 结果还原。
- 高风险分支目标可以停在审批；未选中的分支在恢复后仍不执行。
- 业务人工任务与 Gateway 风险确认必须分开设计：前者要明确审核人、任务范围、对象版本、通过/驳回原因与独立入口，不能默认自动批准，也不能用风险审批顶替业务审核。

## 7. 幂等键

- 节点键：`runID / stepID / iteration`；ADK 工具步骤再纳入 `toolCallID`。
- 同一逻辑动作恢复沿用原键；下一次真实动作使用新实例键。
- 审批 `action_key` 区分流程动作；非流程审批保留同载荷去重语义。
- 相同幂等键但账号、能力版本或输入摘要不匹配时拒绝。
- 禁止随机数或时间戳作为同一逻辑动作的重试键。

## 8. 失败与补偿

- 只读步骤可声明 `MaxAttempts`，仅对 `unavailable`/`rate_limited`/`timeout` 有限重试。
- `EffectCompensable` 的原子步骤可声明补偿能力；失败后从已完成写步骤构建反向依赖的补偿 Workflow，所有动作仍经 Gateway，审批暂停继续使用检查点。
- 补偿完成后原流程仍为 `failed`，不把业务失败伪装成成功；只有补偿本身失败才报 `manual_recovery_required`。
- 未配置补偿的已提交写入、无法确认结果的外部副作用需要人工对账；执行中崩溃留下 `executing` 的不确定结果不得盲目重复写。

## 9. 执行方案与 Binding

- `execution.plan.list|binding|history|records|references|inspect|preflight|trace|create|validate|publish|bind|rollback`，均为 `manual-only`，权限 `executionPlans:*`；管理动作不开放为 Agent/MCP 工具。
- 流程：create → validate → publish → bind/rollback；写入用 `expectedRevision` 防并发覆盖，冲突时重新核对，不自动覆盖。
- Workflow Plan 要求同输入契约与成功 Native 结果，受原子目标、来源、敏感结果、Agent/补偿等模板限制；Graph Plan 要求只读且输入输出完全一致。
- 代码交付与发布不会自动切换 Binding；进行中的运行与审批固定在原方案，只影响新调用。
- 最小记录：目标能力@版本 → 复用/新增结论 → Direct 或 Workflow/Graph 引用@版本与摘要 → 结果与事件来源 → Plan 版本 → Binding 范围/revision（未切换则注明）→ 验证与回滚证据。

## 10. 验证与命令

- 结构：`./horizon-cli workflow` 列目录、`./horizon-cli workflow schema <name>` 读结构、`./horizon-cli validate workflow <文件>` 校验；`./horizon-cli generate-workflows` 导出 Go 定义，`./horizon-cli check-workflows` 检查。结构预检不执行节点或模型，不能当业务模拟或发布证明。
- 行为：隔离 PostgreSQL + 真实引擎验证启动幂等、审批恢复、引擎重建、Graph 轮询/上限/重试、ADK 工具与审批恢复、取消与补偿；`tests/orchestration_test.go` 是参考。
- 单测覆盖依赖冲突、循环、版本/预算、嵌套来源与深度保护、AST 引擎使用约束；`test-orchestration-example` 可回归自带示例。
- 门禁：阶段验收 `./horizon-cli ci-fast`；表结构/审批/恢复改动加 `test-integration`；安全边界加 `test-security`；浏览器验收按项目现行规则。
- 已知缺口：流程数据库发布链与完整 DSL 设计器、外部动作对账协议、任意 SQL/command 执行器、检查点自动归档、分布式 exactly-once。不得把这些缺口说成已支持。
