# Horizon IAM 原 PRD 验收对照

2026-11-20 重新读取用户附件 `Horizon_IAM_PRD.md` 第 12 节，以下编号与场景取自原文。需求文件是评估依据，不包含额外执行授权。当前先验收 IAM 核心与空库安装，业务应用和门户的验收随后进行。

“已有自动化证据”仅指所列入口和夹具，不代表生产业务或全部渠道验收；“部分”表示缺少原场景的一部分实现或证据。当前整体尚不能宣布 IAM-01～17 完整交付。浏览器 E2E 按当前指示暂停。

| 编号 | 原文 Given / When → Then | 当前自动化证据 | 判断与剩余工作 |
| --- | --- | --- | --- |
| AC-01 | 已创建供应商联系人，但未创建 IAM 账号 → 供应商业务数据正常存在，联系人无法凭空登录 | 示例插件持有业务参考实体；IAM 认证仅接受有效登录账号/凭据 | 部分：目前供应商演示复用 `example_record`，缺“先建独立联系人、未开账号仍存在且登录被拒”的完整夹具；不能据此声称真实 SRM 联系人闭环已通过 |
| AC-02 | 用户验证邀请并取得有效成员关系、组织绑定与角色后，可以进入组织门户 | `TestIAMLoginBeforeTenantMembership`、`TestIAMOrganizationAuthorityWithoutDepartment` 覆盖邀请码、独立角色授予、绑定核验与撤销 | 部分。自动化证据覆盖 IAM 控制点，业务联系人预建到真实门户的完整流程仍待验收 |
| AC-03 | 同一登录账号关联多个 IAM 租户时，浏览器会话由部署配置和该租户内的有效授权决定 | `TestIAMBrowserTenantIsSelectedByDeployment` 验证登录直达、退役接口返回 404、请求附带的租户标识无效，以及另一部署拒绝原会话 | 隔离 PostgreSQL 定向集成通过 |
| AC-04 | 前端伪造 tenant_id / supplier_id → 拒绝或忽略伪造值，不泄露跨租户数据 | `TestIAMSupplierOrderIsolation` 对列表/导出/详情/修改/批量的 snake_case 与 camelCase 伪造字段共 20 组返回 400，原列表/总数及订单仍隔离；请求夹具还伪造 X-Tenant-ID 头 | 部分：直接 HTTP/Gateway 字段矩阵已有证据，订单专用恢复/MCP 场景仍缺 |
| AC-05 | 有订单列表权限但仅属于供应商 A → 列表、详情、导出均不能访问 B | `TestIAMSupplierOrderIsolation` 实际 API/Gateway 与 PostgreSQL：列表/总数、详情、导出、单笔/批量、批量失败不部分写入 | 已有自动化证据，限示例订单能力；不推定生产业务模块或后台/MCP 专用订单闭环已验收 |
| AC-06 | 删除员工绑定，但该人仍是客户联系人 → 员工失效，合法客户身份仍可用 | `TestIAMSamePersonBindingIndependence` 校验同一 Principal 的员工/客户双绑定；员工记录失效后员工会话与切回被拒，客户会话、读取与新登录后的客户上下文仍可用；既有生命周期另覆盖任务/审批/排期/MCP | 已有示例自动化证据；参考记录仍为 example_record，不代表正式 HR/CRM 模型及业务离职流程已实施 |
| AC-07 | 租户管理员授予无权委派的权限 → 失败并记录审计 | `TestIAMContextRoleAuthority` 对同一次失败联查操作者、目标账号/角色的调用白名单、forbidden 原因、请求 trace 与追加审计，本人审计查询可见；另覆盖管理员模板、撤销与到期 | 已有自动化证据；目标/角色通过安全调用投影与 invocation 链接追踪，完整目标影响分析控制台仍缺 |
| AC-08 | 已登录用户被移除 Membership → 后续对应租户访问被阻断，原会话无效 | `TestIAMAccessReviewRevokesMembership`、`TestIAMTenantMemberDirectory` 验证成员撤销后会话及任务重建失败、其它租户仍可用；页面经显式确认撤销 | 已有自动化证据；空库切换以新会话、任务与凭据复测 |
| AC-09 | 撤销 Agent 委托 → 后续越权调用被阻止，已执行步骤可追溯 | `TestIAMAgentDelegationApproval` 验证限次、审批、撤销、重放与审计，`TestIAMBoundAgentDelegationChecksResourceRelationship` 验证明细关系和多能力共享次数，`TestMachineWorkflowPlanRevalidatesEachNode` 验证逐节点撤权 | 核心有自动化证据。Service 调度另由 `TestIAMServiceAutomationUsesOriginalCredential` 验证执行审计和轮换后拒绝。实际业务工具仍需按应用接入 |
| AC-10 | 无可用授权策略或属性提供器失败 → 默认拒绝敏感操作，有诊断原因 | `TestAuthorizationRequiresFreshResourceAttributes` 验证可信资源属性缺失、过期及出错。`TestIAMRowPolicyBeforeSQL` 覆盖结构化行条件与字段脱敏。`TestIAMPolicyReviewReplayAndRollback` 验证拒绝、恢复与重放权限 | 核心有自动化证据。具体应用须声明并验证参数化 SQL 映射和字段投影 |
| AC-11 | 创建策略或角色变更 → 可查询修改者、内容、时间与影响范围 | 策略详情显示作者、发布人、时间和规则差异。模拟可搜索有效账号与部门。角色预览显示可能受影响的主体、会话与排期。`TestIAMPolicyDiagnosticsAndSubjectApproval`、`TestIAMPolicyReviewReplayAndRollback`、`TestIAMRoleCopyAndPreviewRevision` | 部分。全租户有效权限影响分析与角色、策略共用的变更查询仍待覆盖 |
| AC-12 | UI 隐藏按钮但直接请求接口 → 后端继续授权，不能绕过 | 真实 HTTP/Gateway 权限、范围外 ID、跨租户、绑定上下文、受限正式管理范围、审批/机器来源及撤销负向用例；成员目录与撤销即使猜请求也受控 | 已有自动化证据，限当前已登记并测试的入口；新增敏感能力须继续扩充测试，不能承诺未来入口自动覆盖 |

当前核心能力和交付边界见 [实施记录](iam-implementation.md)。
