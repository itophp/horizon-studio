# Horizon IAM 实施与验收记录

## 交付状态

当前发布范围为 IAM 核心。以空 PostgreSQL 安装、初始化和授权验收为切换基线。历史数据和旧 OWN 归属无需迁移，业务应用的接入与验收在后续阶段进行。代码由用户授权在工作树内实施，隔离测试建独立 schema，生产切换待部署。

| 能力 | 当前实现 | 剩余范围 |
| --- | --- | --- |
| IAM-01～03、14～15 主体与认证 | 独立 Human、Agent、Service、Application 主体。平台主体详情含风险标记、备注和关联视图。浏览器续期经密码和适用的 MFA 轮换凭据。支持 OIDC/SAML、一次性恢复码、验证后注册和密码找回。无租户归属的账号可经双边验证关联。同一租户内的存量账号可分别验证后交异人审批，并可回滚 | 仅 SSO 的账号通过身份提供方重新登录。跨租户或已有身份绑定、上下文角色及成员条件冲突的账号合并仍需更广的复核和映射流程 |
| IAM-04～06 租户、成员与绑定 | 显式平台操作者、租户与主体生命周期、默认邀请或配置验证后自注册、成员暂停/恢复/到期、绑定核验/审批/失效、Outbox 对账、可信上下文切换和 CSRF 轮换 | 真实业务权威数据提供器与门户按应用接入 |
| IAM-07～09、11～12 授权 | Casbin 按 Principal 判定。支持租户角色、角色复制、变更差异、影响人数及会话和排期预览。资源目录展示已部署的动作和部分授权属性，可按租户启停。Gateway 执行时复查资源状态和授权约束 | 资源注册和属性提供器通过部署声明维护，具体业务 SQL 映射由应用接入 |
| IAM-13 机器与 Agent | 独立凭据、受众和能力版本。同一资源支持多能力委托及共享次数、期限和风险上限。带绑定的委托校验可信业务关系。机器 Workflow 与受限 ADK 逐节点复验。定时 Service 任务固定凭据及能力版本并通过 Gateway 重新授权 | Service 调度开放给显式允许 `automation` 来源的低风险原子能力。机器 ADK 工具限低风险只读 Direct |
| IAM-10、16～17 治理 | 追加审计、Ed25519 检查点、轮换、归档及到期清理回执。审计导出支持租户级异人审批和一次性下载。提供规则检查、按有效上下文模拟、差异、异主体发布及回退。定期复核成员、有效上下文角色、普通长期角色和直接允许 | 风险报告按明确的敏感权限和已记录认证事件筛查。全租户有效权限影响分析和独立审计存储仍待扩展 |

## 空库安装和部署配置

对空数据库运行 `./horizon-cli bootstrap` 安装初始结构和管理员。部署配置决定浏览器登录所用的 IAM 租户，登录和注册流程共用此配置。

配置 `IAM_PLATFORM_OPERATOR_IDS` 使用允许执行跨租户管理的 Human Principal UUID。操作仍要求正式租户权限、明确目标、理由和一次性再认证。联系验证与恢复用 `PUBLIC_TENANT_ID` 的 SMTP 设置发送邮件。可选短信适配器用 `IAM_SMS_URL` 和 `IAM_SMS_TOKEN` 配置，以 HTTPS POST 投递 `to`、`purpose`、`code` 和 `messageId`，使用同一消息标识作为幂等键。外部提供方与凭据用部署配置提供。

签名检查点用 `IAM_AUDIT_SIGNING_SEED` 启用，轮换时用 `IAM_AUDIT_PREVIOUS_PUBLIC_KEYS` 提供历史公钥。`IAM_AUDIT_ONLINE_DAYS` 控制已签名批次移入同一 PostgreSQL 的可查询归档。`IAM_AUDIT_RETENTION_DAYS` 控制总保留期，默认 0 表示永久保留。正值要求启用签名归档并覆盖在线窗口。Worker 核验整批签名后，在同一事务写入签名回执及删除到期事件。保留检查点、回执和历史验证公钥，校验可以区分已过期批次与完整性错误。部署验收包括登录、租户隔离、上下文切换、撤权、任务恢复、拒绝原因、时延和审计及失效事件积压。

以下保留各开发阶段的实现与验证记录。交付状态以本文顶部为准。

## 外部登录与个人安全

提供方通过 `IAM_OIDC_PROVIDERS_JSON` 部署，字段为 `id/name/issuer/clientId/clientSecret/redirectUrl`；需 32 字节 `SETTINGS_ENCRYPTION_KEY`。生产只允许 HTTPS，回调必须为 `PUBLIC_APP_URL` 同源的 `/login/oidc`。启动时 discovery 失败即拒绝启动，不静默跳过。仅支持 RS256/ES256/EdDSA；不得把测试 HTTP 提供方用于生产。

SAML 提供方使用 `IAM_SAML_PROVIDERS_JSON`，字段 `id/name/entityId/metadataXml/certificatePem/privateKeyPem`。元数据与 SP RSA >=2048 密钥/证书由部署配置，提供方 ID 与 OIDC 全局唯一。公共 origin 必须 HTTPS（包括本地 SAML）；配置有效 IdP HTTP-Redirect 入口与 HTTP-POST ACS `/api/auth/federation/saml/<id>/acs`。启动校验元数据、签名证书和强度，支持元数据内多签名证书轮换，至少一张当前有效；运行时继续拒绝过期证书/元数据。

SP 请求 SHA-256 签名，回调验证 persistent NameID、issuer、受众、请求 ID、目的地、接收者、时间与 XML 签名，断言 ID 在 PostgreSQL 原子消费。仅 ACS 允许 IdP 跨站 POST；flow Cookie 为 Secure/SameSite=None，其余认证端点保持 Origin，浏览器会话保持 Lax。回调不在 URL 放断言/凭证，固定跳转 `/login/saml`，用两分钟 HttpOnly Cookie 显式完成并校验本地 MFA。完成证明与外部关联证明用途分离，不可互换或重放。

`/login` 展示已配置提供方。个人菜单的“账号安全”打开 Runtime `/account-security`，活动标签写入 URL。外部关联从已登录账号发起，验证外部账号后以当前密码消费一次性证明。外部账号已有映射时进入人工复核。需要合并租户登录账号时，从账号安全的已关联账号入口进入合并复核。

验证器设置和恢复码只在内存一次展示，不进入 Provider/Query 结果。激活退出其他设备、撤销既有 MCP 凭据。设备退出经过单次再认证证明；当前设备从菜单退出。解绑删除相应外部会话，之后重新关联也不复活旧会话；本地登录保留。预览不查询个人安全数据；此页面及回调不开放 WebMCP。设备及外部关联表格保留原始 UUID 作为操作标识，不使用表格生成的数字行索引。

## 页面设计选择

1. 使用者是已登录本人，任务为设备撤销、验证器设置、外部关联和范围内审计查询/导出；登录回调为公开认证任务。
2. 先显示安全页名称和任务说明，再用同级标签呈现一个安全对象；设备显示设备/租户/当前设备/最近活动/到期，外部关联显示提供方及关联/撤销时间；审计依次显示时间、操作、结果码、原因码、资源类型及请求链路，不展示无法授权解析名称的关联账号/部门 ID。
3. 表格行命令复用 RowActions，敏感命令使用密码确认对话框；验证器以设置、激活、保存恢复码为顺序操作。审计采用 B1/L1 列表，工具栏放范围、刷新与当前页导出，最新/更早记录为游标导航，URL 保存 `auditScope/before`；不对一页记录伪造全量搜索/排序。
4. 个人安全采用 Runtime 页面加可信组件，原因是短期秘密展示及多步再认证；认证回调沿用聚焦表单专用页。保持后台 tokens，不增加侧栏入口。
5. 复用 page-heading、TablePanelComponent、DataTable、Select、Tabs、Button、Input、EditorDialog、ErrorBanner、RHF/Zod；所有安全业务动作通过生成能力类型及统一 Gateway SDK。可信组件固定 `identity-security-panel@1.0`，代码由身份模块维护。
6. RTL 覆盖表单提交、失败不重放、秘密不入 Query、一次展示及只读预览；审计验证游标、导出重授权、租户范围和旧上下文下载取消；真实库覆盖认证/关联/MFA/撤销。响应式、实际焦点、长文本与浅暗主题尚未进行浏览器实景验收。

## 成员邀请界面设计与能力复用

1. 使用者为已登录本人及有 `accounts:create` 的邀请人，任务是接受现有登录账号邀请、检查可用上下文、邀请他人加入部署租户。
2. 登录身份用已授权的组织名称和身份类型标识。邀请时输入对方已知的登录名，服务端按部署租户核验邀请码。
3. 接受邀请为本人主操作，刷新为次操作；邀请人额外显示“邀请成员”，生成后只在当前弹窗一次展示邀请码和有效期。接受不自动切换，也不赋予角色。
4. 在现有 Runtime 账号安全页增加同级标签，采用 B1/L1 表格与聚焦短表单弹窗，沿用现有账号表格、EditorDialog 与后台 tokens，不新增侧栏或页面工具。
5. 直接复用 `identity.membership.invite/accept@1.0.0` 的同步原子 Gateway 能力，无新数据库/Workflow/角色规则；本人列表和切换复用生成的 Goa 认证协议。邀请码仅在组件内存，不进入 Provider、Query 缓存或 URL。失败同载荷保留幂等键，切换上下文丢弃旧回调。
6. RTL 验证名称/原 ID、允许/无效输入、失败重试、只读预览、秘密隔离和切换卸载；既有真实 PostgreSQL 用例覆盖错误接收人、过期/复用与邀请人撤权。浏览器实景、窄屏与浅暗主题未验证（E2E 暂停）。此入口仅支持已有登录账号，不代表自注册、新账号邀请注册或完整租户成员管理已完成。

## 当前租户成员管理

新增 `identity.membership.list@1.0.0`，因为现有账号快照不含租户成员的撤销/到期状态，也不适合作为成员治理目录。它是同步 Direct 原子只读能力，以现有 `tenant_membership` 为主对象；每次重建浏览器身份并要求正式 `access:manage/ALL`，拒绝身份绑定上下文和自动化来源。SQL 固定可信租户，按账号 ID 游标分页（每页最多 50），仅返回成员识别名称、主部门、成员/账号/登录状态、有效期及管理员保护标记；不返回密码、邮件、主体 UUID 或其他租户归属。

管理员从账号安全页的独立“租户成员”标签查看目录，经确认复用 `identity.membership.revoke` 撤销。撤销与既有 Outbox/会话失效同事务，保护管理员，不影响其它租户；分页/确认状态绑定当前可信上下文，变更后刷新目录。界面采用 B1/L1 表格，识别列优先姓名和登录名，组织/部门与状态辅助判断，行菜单承载撤销。不存在跨请求等待，不另建 Workflow 或审批流程。此次不新增表或调整成员恢复语义；完整主体/绑定/委托管理仍为独立缺口。

成员行还复用 `identity.login.reauthenticate/freeze/restore`：同时有 `accounts:security` 的管理员可对非本人、非受保护账号输入理由和本人密码，先取得绑定目标/操作/上下文的一次证明，再改变登录状态。每次失败重新认证，证明不进缓存；上下文卸载取消并阻断下一步。跨租户共享登录账号仍按既有后端规则拒绝冻结/恢复，不以租户成员撤销替代全局账号冻结；恢复不会复活旧会话或令牌。此项只新增页面接入。

## 策略治理界面

- 使用者与任务：当前租户的正式授权管理员检查限制策略，另一位管理员发布；不开放 Agent/MCP 管理。
- 信息排序：版本列表优先变更理由、版本、当前/历史/草稿状态及本人/其他管理员；详情显示完整规则和相对当前版本的新增/移除。作者内部账号 ID 不作为可见名称。
- 主次操作：新建草稿为列表主操作，刷新为次操作，行命令进入审核详情；发布必须显式确认，回滚填写理由并只创建新草稿。相同载荷失败重试保留幂等键，修改输入使用新键。
- 模式：B1/L1 版本列表、B2/L1 独立详情（URL `policy`），草稿及回滚为 B4 弹窗。规则使用结构化策略语言编辑器；技术动作名、资源名和绑定类型是规则语法，不要求手填关联账号或部门 ID。JSON 不执行代码。最多 100 条规则，重复/冲突及无效规则继续由服务端拒绝。
- 组件和能力：平台托管 Runtime `iam-governance@1.0.0`、可信组件 `identity-governance-panel@1.0`；个人菜单按已授权 Runtime 页面显示入口，不增加侧栏。复用 `identity.policy.history/preview/draft/simulate/publish/rollback@1.0.0` 的 Direct 方案及既有策略事务/Outbox；没有新增并行 REST、SQL 业务实现或 Workflow。准确输出 Schema 生成 SDK 类型；发布声明同时修改版本及头实体。追加 schema 67 的平台预置模板，使运行中创建租户可立即获得账号安全与治理页；Down 仅删除模板，不删除已安装租户的数据。复用 DataTable/RowActions/EditorDialog/Textarea/Select/Button/RHF/Zod，en 与 zh-CN 同步。
- 验收：RTL 的 10 个交互用例通过，覆盖只读/无权不查询、规则语法错误、从当前版本起草、禁止自批/过期发布、显式确认、当前身份模拟、回滚只起草、幂等重试及上下文切换丢弃旧回调及后台刷新不静默更换草稿基准版本；TypeScript 通过。真实库复查输出投影及跨租户/正式 ALL/过期发布负例，定向两用例均通过（分别 7.101 与 7.345 秒）。E2E 暂停，1440/768/390px、键盘焦点、浅暗主题与双语实景尚未验证。

服务端每次重新检查原会话与正式 ALL，绑定身份上下文拒绝管理；界面可见性不授予权限。草稿不可变、禁止自批、基于当前版本发布以及回滚再审均由既有数据库事务强制。当前界面仅模拟当前正式身份的给定 action/resource/permission 组合，未执行目标能力，不能替代真实数据过滤或通用外部 ABAC 属性验证；任意主体的授权搜索选择器仍属待完善范围。切换上下文销毁表单/决策，页面不开放 WebMCP。

## 验证证据

隔离 PostgreSQL 为 `127.0.0.1:54329/baseline`；每例创建独立 schema。OIDC 隔离提供方使用真实 RSA 签名 JWT、JWKS、PKCE code verifier、client secret 和 redirect 验证，非生产 IdP。

- 机器凭据、Agent 审批和瞬态结果：`TestIAMMachineCredentials`、`TestIAMAgentDelegationApproval`、`TestTransientCredentialResults` 已通过定向真实库测试。
- 外部账号：`TestIAMOIDCLinkLoginAndReplay` 已通过，覆盖错误浏览器 cookie、state/nonce/audience/expiry、关联证明与恢复码重放、双账号抢绑冲突审计、原 account_id、解绑外部会话失效和敏感投影。
- SAML：`TestIAMSAMLLinkLoginAndReplay` 定向真实库通过，覆盖实际 SHA-256 AuthnRequest 与 XML 签名、表单 ACS、浏览器/flow/断言/完成 Cookie 重放、双边关联、解绑会话失效、本地 MFA/恢复码重放；错误受众/时间/请求/目的地/接收者/issuer/transient NameID/无签名/篡改/包装/SHA-1 均拒绝。启动配置、HTTPS origin、跨协议 ID、弱或无有效签名证书及精确跨站端点负例通过。回调组件 6 个 OIDC/SAML 交互用例通过；TypeScript 通过。
- 前端：回调/登录/授权版本 3 文件 7 用例通过；验证器/只读预览 1 文件 2 用例通过；设备撤销/外部解绑的真实表格 UUID 提交 1 文件 2 用例通过。TypeScript 与 Go 身份/配置定向检查通过。
- `TestIAMMigrationPreservesAuthorizationAndOwnership` 与 `TestIAMMigrationRejectsAdministratorPolicyConflict` 通过。真实库从 schema 51 升级，比较账号、角色、部门关系和资源归属；核对确定性主体/凭据/成员回填、旧会话撤销、显式拒绝、CUSTOM/部门树/OWN 范围、附加部门权限隔离及停用账号拒绝。管理员覆盖冲突时失败步骤事务回滚，不静默扩大权限。
- 本地 `ci-fast` 已通过：58 个 Vitest 文件、276 个用例（审计入口批次），含格式、lint、TypeScript、Go vet/测试、i18n、生成一致性、实体、Workflow 与构建。实体检查为 88 个声明、115 张表、1053 列，零声明/契约/schema 错误。构建保留现有大 chunk 提示，未关闭规则。新增迁移演练命令后另验证受影响的 CLI 与规则；此前完整 `test-security` 已通过（756.477 秒），但该结果先于 SAML 改动；SAML 批次 `ci-fast` 也已通过；完整安全命令使用旧 CLI 二进制时在 10 分钟总预算超时中断，没有计作通过。改用源码 CLI 的现行 20 分钟预算后，审计入口批次完整 test-security 退出码 0，数据库集成用时 788.542 秒。后续策略治理的变更另用定向数据库用例验证，策略治理批次 ci-fast 已通过（59 文件/286 用例），含生成一致性、实体、Go 与前端检查。schema 67 后的最终完整安全结果另行记录。
- 审计 Runtime 入口：`security-audit.test.tsx` 与原表格回归共 2 文件 7 用例通过，验证游标翻页、受限范围、按当前页事件上界导出、替换上下文取消下载及只读预览不查询；TypeScript 通过。`TestIAMSecurityAuditCursorAndScope` 与签名检查点两个真实库用例已通过（11.771 秒），覆盖其它租户/账号/部门/绑定的敌对投影、租户管理权限、游标不重复及固定页面导出一致。
- 实体解析修复 `ALTER TABLE ADD COLUMN ... REFERENCES` 的外键遗漏及同一 ALTER 中多列/约束操作的遗漏；单元回归通过。CLI Schema 为 `MachineCallable`、`DelegationResourceID` 补齐执行边界说明，契约文档完整性回归通过。
- E2E 未执行（按当前指示暂停）；远程 CI 未触发；人工 MCP 管理能力不对自动化暴露，未为调试扩大工具面。

## 本批次命令记录

以下均在本地执行；没有远程 CI 或生产切换。根目录旧 `horizon-cli` 二进制与源码不一致，因此本批次通过 `go run ./cmd/horizon-cli` 使用工作树版本。

| 命令 | 结果 | 覆盖与限制 |
| --- | --- | --- |
| `go run ./cmd/horizon-cli generate` | 退出码 0 | 从 Goa/SQL/Runtime/Capability 源重生成；中途缺少新增翻译导致的失败已修复 |
| `go run ./cmd/horizon-cli ci-fast` | 退出码 0，59 文件/286 个 Vitest 用例 | 含 lint、TypeScript、i18n、Go、生成一致性、实体、Workflow 与构建；保留既有大 chunk 提示 |
| `go test ./foundation/system/identity ./foundation/runtime/app -run 'Test.*RuntimeApplicationAssembles' -count=1` | 退出码 0 | 身份 Runtime 实际组装；app 包没有匹配测试，未计为该包行为验收 |
| `go test -tags=integration ./tests/integration -run '^TestIAMPolicyReviewReplayAndRollback$' -count=1 -timeout=2m -v` | 退出码 0，7.101 秒 | 异人审批、重放再授权、回滚与完整审核投影 |
| `go test -tags=integration ./tests/integration -run '^TestIAMPolicyGovernanceScopeAndStaleDraft$' -count=1 -timeout=2m -v` | 退出码 0，7.345 秒 | 正式范围、跨租户版本猜测、过期草稿 |
| `go test -tags=integration ./tests/integration -run '^TestIAMRuntimeProvisioningWithoutRestart$' -count=1 -timeout=2m -v` | 退出码 0，7.381 秒 | 运行中新增租户、schema 67 Down/重复 Up、已有租户数据保留 |
| `go run ./cmd/horizon-cli test-iam-migration-drill` | 退出码 0，8.197 秒 | 旧库备份恢复后升级到 67；160 组表/序列一致，容器客户端 18.6 |
| `go test -tags=integration,iamperf ./tests/integration -run '^TestIAMAuthorizationBaseline$' -count=1 -timeout=3m -v` | 退出码 0，11.327 秒 | 独立执行的微型性能基线，边界见单独记录 |
| `go run ./cmd/horizon-cli check-entities` | 退出码 0 | 88 声明、115 表、1053 列，0 错误 |
| `go run ./cmd/horizon-cli check-rules` | 退出码 0 | 规则/Skill/CLI 路由检查 |
| `quick_validate.py .agents/skills/baseline-security`（uv 临时 PyYAML） | 退出码 0 | Skill 元数据验证，不修改项目依赖 |
| `git diff --check` | 退出码 0 | 无空白错误 |

数据库命令均显式设置隔离 `TEST_DATABASE_URL`；恢复演练另设置 `IAM_DRILL_CONTAINER=xxh-baseline-postgres-1`。E2E 与人工 MCP 调试未执行：前者按当前指示暂停，后者的 IAM 管理能力不开放自动化，未扩大暴露面。schema 67 完整 `test-security` 已退出 0，集成测试 848.607 秒；追加 `check-generated` 已退出 0。此安全门禁覆盖后端与迁移 67，后续邀请 UI 的验证单独记录。

## 邀请与成员治理追加验证（2026-11-20）

- 个人入口 `security-memberships` 支持本人上下文列表、显式切换及邀请码接受；`security-invitation` 让有创建权限的邀请人在当前部门生成一次性凭据。en/zh-CN 同步，凭据未进入 Query。
- 成员目录 `identity.membership.list@1.0.0` 及参数化 SQL 已注册、生成并在隔离库验证默认 Direct；无运行环境 Binding/方案切换。Goa 认证协议未改变，没有新表/迁移。已有审计/策略的正式管理守卫重命名为 workspaceIAMAdmin 后共同复用，授权语义保持。
- 账号安全页成员标签复用目录与撤销能力；冻结/恢复另要求 accounts:security 并先再认证。`security-confirm` 与 MFA 链在卸载时取消，旧回调不得触发下一请求；撤销确认失败同载荷保留幂等键。
- 定向前端：成员/邀请、安全页/MFA/审计 4 文件 18 用例通过；成员目录与登录状态最终 2 文件 10 用例通过。中途成员目录夹具错误地把管理员也标为 revoked，导致单值定位命中两行；已修夹具并保留“目标状态变为已撤销”的断言。
- `TestIAMTenantMemberDirectory`、`TestIAMInvitationExpiryAndAuthority`：真实 PostgreSQL 13.317 秒通过，覆盖跨租户、分页/投影、管理员保护、撤销、过期和邀请人撤权。追加受限正式范围与绑定身份拒绝后，目录与 `TestIAMIdentityBindingLifecycle` 15.095 秒通过。
- 邀请阶段 ci-fast 退出 0（60 文件/295 用例）；后续目录/冻结 UI 的最终 ci-fast 已退出 0（62 文件/305 用例，含生成一致性、实体、Go、lint、类型、i18n 与构建）；新目录的完整 test-security 已退出 0，真实数据库集成 844.222 秒。SQL/能力源的完整 generate 退出 0；首次由源码 CLI 编译时因新 sqlc 方法尚未生成而失败，已先用仓库锁定的 `.tools/bin/sqlc` 从 SQL 源生成，再完整 generate，没有手改生成代码。
- E2E 未执行（按当前指示暂停）；实际响应式、焦点、浅暗主题视觉未验收。MCP 管理能力保持人工专用，未为调试扩大暴露面。

## 原文验收补强

原文验收补强：示例 manifest 新增客户联系人类型，复用插件业务参考记录与核验能力；同一个 Principal 的员工记录失效后，原员工会话与切回请求被拒，客户会话、新登录后的客户上下文和读取仍可用。供应商能力新增 20 组伪造字段请求；超委派上限的同次失败关联目标/角色白名单、操作者、原因、请求 trace 与审计查询。三个方向与报告、绑定生命周期的定向真实库五用例通过（35.780 秒）。首次命令漏设 TEST_DATABASE_URL 按规则失败而非跳过，之后显式配置隔离库；审计断言按既有 req_ 前缀修正。

## 风险报告的能力与页面设计

现有 audit.list/export 是逐条事件，roleGrant.list 不含风险筛选和窗口汇总，新增 `identity.governance.report@1.0.0` 为 Direct 原子只读能力。它在同一 Repeatable Read 只读事务内重建浏览器身份，要求正式 access:manage/ALL，拒绝绑定身份与自动化。SQL 仅取可信租户，无新表/迁移或运行环境 Binding 切换，TransientResult 不持久化报告正文。

按数据库时间选择 1～168 小时；认证计数仅含已记录的已知账号失败。授权拒绝包括 denied 与 forbidden/unauthenticated 调用失败，排除普通业务失败；反复拒绝按操作/原因跨账号分组、至少五次、最多 50 组，系统账号 0 不计为涉及账号。敏感授予只筛查当前有效的限时上下文角色及显式账号/角色/权限/审批/应用/发布管理权限，最多显示 50 条并提供全部匹配总数；有效绑定视图排除业务已失效但事件未消费的绑定。它不模拟 override/deny 最终效果，不覆盖所有插件权限、永久角色和未知账号认证；周期复核/保留/撤销仍缺。真实库发现并修复权限 JSON 大小写兼容和失效绑定视图的问题，没有放宽授权。

页面设计六项：

1. 使用者是正式租户管理员，任务是识别敏感授予及反复拒绝等调查线索。
2. 先观察窗/快照时间，再三个真实汇总；授予表以姓名/登录名、角色名称和到期识别，拒绝表以操作/原因、次数及涉及账号数识别，不显示关联 UUID。
3. 仅时间筛选与刷新，不以报告按钮代替授权复核状态流。
4. 在既有 Runtime identity-governance-panel 的同级“访问风险”标签使用 B1/L1 表格；URL 保存 tab/riskHours，不增加侧栏。
5. 复用 Tabs/Select/Button/DataTable/ShortDate/ErrorBanner 与生成 Capability SDK，Query 按可信上下文/窗口隔离，取消传播到 SDK。en/zh-CN 同步。
6. 报告/策略两个前端文件 17 用例通过，覆盖无权/绑定/预览不查询、时间窗与刷新、授权名称、空错状态及旧上下文取消；真实库覆盖窗口/租户/正式范围、过期撤销、失效绑定、50 条上限和总数。浏览器实景、长文案、窄屏与主题未验证（E2E 暂停）。

此批次完整 `go run ./cmd/horizon-cli generate` 退出 0；`ci-fast` 退出 0（63 个 Vitest 文件/312 用例，含规则、格式、lint、TypeScript、Go、i18n、实体、Workflow、生成一致性与构建），保留原大 chunk 提示。集中类型检查发现 DataTable 的数字行键要求，已为只读报告映射独立行键并保留真实 grantId，没有把行索引用作业务操作参数。官方 Skill 元数据校验通过。最终完整 `test-security` 退出 0，真实 PostgreSQL 集成 858.118 秒，包含本轮风险报告、双身份、伪造字段及同次失败授予审计；E2E 未执行（按当前指示暂停），远程 CI 未触发。

收尾修正报告的实体引用名称为已声明的 security_principal；重新 generate、check-generated、check-entities 均退出 0（88 声明/115 表/1053 列，零声明/契约/schema 错误），最终规则检查与 git diff --check 通过。此项仅实体元数据名称修正，未改变已测授权或查询执行逻辑。

## 备份恢复执行方式

`test-iam-migration-drill` 是单独启用的工程验收命令，不包含在默认集成测试中。`TEST_DATABASE_URL` 必须是隔离本地 PostgreSQL；命令只删除和恢复测试自己创建的唯一 schema。使用与服务端兼容的 `pg_dump`/`pg_restore`；客户端未安装或版本不兼容直接失败。若使用仓库 Docker 服务，指定 `IAM_DRILL_CONTAINER=xxh-baseline-postgres-1`，通过容器内客户端执行。密码通过环境传递，备份只在测试内存中，不写入仓库或诊断。

```sh
IAM_DRILL_CONTAINER=xxh-baseline-postgres-1 ./horizon-cli test-iam-migration-drill
```

演练从空 schema 执行当前全部 Goose 迁移并 bootstrap，随后备份该 schema、删除测试 schema 并恢复备份。逐表比较数据，逐序列比较 `last_value/is_called`。恢复后再次执行 Goose 并确认无需重跑迁移，再验证管理员正式授权和登录。历史数据迁移由独立工程回归用例覆盖。

## 行为验收对照

以下对照用户粘贴计划中的可观察行为。原 PRD 已重新读取，精确编号另见 [AC-01～AC-12 对照](iam-acceptance.md)；定向自动化和本地门禁只证明所列范围。

| 验收行为 | 当前证据 | 状态/边界 |
| --- | --- | --- |
| 浏览器会话使用部署租户，切换组织身份时轮换 CSRF | `TestIAMBrowserTenantIsSelectedByDeployment`、`TestIAMOrganizationAuthorityWithoutDepartment` | 已通过隔离 PostgreSQL 定向集成 |
| 本人接受邀请、当前部门邀请发放及上下文切换 | `security-memberships.test.tsx` 8 用例 | RTL 已覆盖；仅现有登录账号，秘密不入 Query，接受不授予角色 |
| 租户成员游标目录、范围拒绝、撤销及管理员保护 | `TestIAMTenantMemberDirectory`、`security-workspace-members.test.tsx` 6 用例 | 真实库/RTL 已覆盖；不含成员恢复与有效期编辑 |
| 邀请过期和邀请人撤权拒绝 | `TestIAMInvitationExpiryAndAuthority` | 真实库已覆盖，拒绝不消费邀请 |
| 冻结/恢复界面的证明链和旧上下文取消 | `security-login-status.test.tsx` 4 用例、`security-panel.test.tsx` | RTL 已覆盖；底层负向授权由已有真实库用例验证 |
| 插件身份认领、核验、失效及局部影响 | `TestIAMIdentityBindingLifecycle` | 已覆盖事件/Outbox 失效与漏消费对账；通用业务源权威属性对账尚缺 |
| 供应商 A 的列表、总数、详情、导出与写入不能访问 B | `TestIAMSupplierOrderIsolation` | 直接 Gateway/API 与单笔/批量已覆盖；订单专用后台恢复/MCP 闭环尚缺 |
| 单个身份失效后其余合法身份仍可用 | `TestIAMSamePersonBindingIndependence` 及已有绑定用例 | 同一人双绑定示例已覆盖；正式 HR/CRM 离职闭环仍缺 |
| 冻结恢复、设备撤销、验证器及恢复码一次性 | `TestIAMLoginFreezeAndRestore`、`TestIAMDeviceSessionRevocation`、`TestIAMAuthenticatorAndRecoveryCodes` | 另以 `TestIAMRegistrationRequiresVerifiedContact` 和 `TestIAMVerifiedContactPasswordRecovery` 验证联系渠道确认 |
| 外部身份双边关联、冲突、协议重放与本地 MFA | `TestIAMOIDCLinkLoginAndReplay`、`TestIAMSAMLLinkLoginAndReplay`、`TestIAMAssociateIndependentLogin` | 外部映射可通过 Runtime 确认、异人复核与回退 |
| 正式委派上限、上下文角色撤销/到期 | `TestIAMContextRoleAuthority`、`TestIAMPlatformPrincipalLifecycle` | 平台管理另行验证操作者、目标租户和正式权限 |
| 机器凭据、绑定委托、人工审批和定时运行 | `TestIAMMachineCredentials`、`TestIAMAgentDelegationApproval`、`TestIAMBoundAgentDelegationChecksResourceRelationship`、`TestMachineWorkflowPlanRevalidatesEachNode`、`TestIAMServiceAutomationUsesOriginalCredential` | 排期凭据轮换后已排队的运行拒绝并停用排期 |
| 异人发布、过期版本拒绝、策略回滚及调用重放再授权 | `TestIAMPolicyReviewReplayAndRollback`、`TestIAMPolicyGovernanceScopeAndStaleDraft`、`TestIAMRowPolicyBeforeSQL` | 含等值行条件和声明字段脱敏 |
| 治理页面失败重试、上下文变化及版本并发 | `policy-panel.test.tsx` 10 用例 | 已覆盖 RTL；实景视觉/E2E 暂停 |
| 审计范围、游标、固定页导出、追加保护及篡改检测 | `TestIAMSecurityAuditCheckpoint`、`TestIAMSecurityAuditCursorAndScope`、`TestIAMSecurityAuditRetentionPreservesSignedEvidence` | 包含签名归档、配置总保留期后的清理与签名回执，独立审计存储待部署选型 |
| 运行中创建租户即获得 IAM Runtime 页面 | `TestIAMRuntimeProvisioningWithoutRestart` | schema 67 模板、回退及重复升级已覆盖，不需重启 |
| 空库安装、管理员初始化和隔离备份恢复 | `TestIAMBackupRestoreDrill` | Schema 88 的 192 组表与序列快照一致，恢复后管理员可正常认证并获得权限 |
| 周期授权复核/保留/撤销、高风险及异常认证报告 | `TestIAMScheduledReviewUsesDedicatedServicePrincipal`、`TestIAMAccessReviewRevokesMembership`、`TestIAMAccessReviewRevokesOnlyCurrentRoleAssignment`、`identity.governance.report` | 复核覆盖成员、有效限时角色及普通长期角色，风险报告按明确条件筛查 |
| 发布监控、真实持续负载和 AC 精确映射 | 本地授权微型负载基线 | 部分覆盖；不能作为完整发布验收 |

## 性能基线

新增显式启用的 `integration && iamperf` 工作负载，在独立 schema 中每次重建会话再授权，分别测量允许/无权限拒绝及 1/8 并发，每场景 256 样本与 8 次预热。2026-11-20 本地测量通过，授权 P99 为 1.959～3.172ms，会话+授权 P99 为 13.322～47.883ms。此数据不含 HTTP/Gateway/审计，也不含通用属性提供器或限制规则负载；它不构成生产性能承诺。完整分位数、吞吐、命令和限制见 [授权基线](iam-authorization-baseline.md)。

## 发布验收

IAM 核心按空库验证安装、初始化、认证、隔离、撤权、任务恢复和治理。[AC 对照](iam-acceptance.md) 记录示例应用与核心控制点的证据。实际生产部署参数及业务渠道验收需由目标环境提供。

当前迁移 88 的本地门禁全部退出 0。

- `go run ./cmd/horizon-cli generate` 同步 Goa、SQL、Runtime 和 SDK。
- `VITEST_MAX_WORKERS=2 go run ./cmd/horizon-cli ci-fast` 通过 63 个 Vitest 文件和 312 个用例，包含 `check-generated` 与 `check-entities`。实体检查覆盖 98 个声明、124 张表和 1146 列，声明、契约及 schema 错误均为 0。Vite 构建有既有大包体提示。
- `go run ./cmd/horizon-cli test-security` 通过安全单元测试和隔离 PostgreSQL 的 120 个集成测试。集成耗时 1061.954 秒。
- `go run ./cmd/horizon-cli test-iam-migration-drill` 通过空库安装与恢复，192 组表和序列快照一致，恢复后管理员登录通过。
- E2E 未执行（按当前指示暂停）。远程 CI 未触发。生产切换、部署级 SSO 和通知验证、持续负载及业务端到端验收待实际环境。

## IAM-01～03 主体详情、续期和合并复核

1. 平台安全管理员在权限管理的平台目录选择租户和主体。详情显示主体类型、状态、来源和风险等级，各关联对象放入账号、成员关系、绑定、角色、会话和审计标签。查询声明目标租户及访问理由，服务端记录审计。风险等级和安全备注通过带密码和适用 MFA 的弹窗提交。
2. 用户在账号安全的当前设备行续期。Goa 会话协议保留设备标识和工作上下文，重新校验会话时间及身份，原子轮换 Cookie 和 CSRF，刷新前端授权状态。
3. 有本地凭据的两个普通账号在同一租户分别验证密码和适用的第二因素。管理员在账号安全的合并复核标签查看当事人、原因、状态和期限，经再认证决定。保留租户账号 ID 及账号级角色和覆盖规则。合并和回滚撤销双方原会话及相关运行权限，保留映射和审计。跨租户、不同成员条件、受保护账号、活动绑定或上下文角色产生冲突时拒绝自动改写关联。
4. 主体详情沿用现有 Runtime 平台治理视图，用 URL 定位目标，复用标签、表格及再认证弹窗。合并复核复用账号安全视图，分开本人申请和租户管理员队列。界面同步英文与简体中文。
5. 详情、风险变更和合并状态分别由 `identity.principal.detail/setSecurity@1.0.0` 和 `identity.loginMerge.list/request/confirm/decide@1.0.0` 提供，使用默认 Direct。每次确认是独立事务，复核请求表保存等待状态。迁移 89 保存风险资料和认证时点，迁移 90 保存复核记录。
6. 测试覆盖跨范围主体拒绝、风险证明防篡改、会话轮换与 MFA 防重放、合并双方验证、异人决策、账号权限分离和回滚。运行中的租户通过默认 Runtime 贡献获取管理界面。

迁移 90 的本地 `generate`、`ci-fast`、`test-security` 和 `test-iam-migration-drill` 均通过。Git 空白检查通过。前端 64 个文件共 313 例，隔离 PostgreSQL 集成套件共 123 例，耗时 1153.606 秒。主体详情的最终会话过滤与日期投影另通过定向真实库测试。恢复演练比较 193 组表与序列快照，并验证恢复后管理员登录。E2E 未执行（按当前指示暂停）。

## IAM-07、08、10 角色、资源目录与审计导出

1. 角色管理员从「权限管理 → 角色管理」复制或编辑角色。复制产生停用的租户自定义角色，继承源角色的说明与权限定义，成员授权需单独配置。编辑首次提交显示权限差异、状态变化及可能影响的主体、会话与启用排期。再次确认提交绑定当前角色和候选配置的指纹。角色更新 API 接受省略预览指纹的请求，并继续校验正式委派上限。周期复核撤销上下文角色时显示该授予范围内的影响计数。
2. 租户管理员从「IAM 治理 → 资源目录」搜索已注册资源，查看能力版本、权限、允许来源、机器访问、可信属性提供器与字段声明。停用和恢复需要说明理由及再认证。Gateway 采用租户和能力主实体检查状态，写事务与停用操作串行化。资源变更刷新会话授权版本。身份管理和系统专用资源仍由部署控制。
3. 租户管理员从「IAM 治理 → 审计导出审批」设置审批要求，初始设置允许直接导出。启用后，个人与租户审计导出都复用 Gateway 异人审批。审批固定筛选条件、记录 ID 上下界及最多条数。获批后只有原账号在原工作上下文可以领取，许可在 15 分钟内单次有效。执行时重新检查当前查询权限并记录实际下载。具有 `capability:approve` 的审核人可以从账号安全页打开审计导出队列。
4. 角色复制和差异预览沿用 Runtime 角色列表和表单。资源状态变更及导出配置沿用短表单和再认证弹窗，审计申请和已批准下载留在账号安全。英文和简体中文同步，预览模式不访问私有数据。
5. 角色、资源和导出许可使用默认 Direct 执行方案。审批等待由已有 Gateway 队列持久化，审核通过时仅签发下载许可。导出在另一入口消费许可并临时返回同一白名单投影。迁移 91 增加资源停用及版本、审计审批配置和许可存储。
6. 预览计数表示可能受影响的对象。实际有效权限仍受其他角色、覆盖规则和策略共同决定。资源注册和属性提供器从部署契约加载。

- `go run ./cmd/horizon-cli generate` 通过，从 SQL、Goa、Runtime 与 Capability 契约重生成产物。定向审计页 Vitest 6 例通过，含筛选条件、记录 ID 上下界及待批阶段的下载限制。
- `VITEST_MAX_WORKERS=2 go run ./cmd/horizon-cli ci-fast` 通过，64 个 Vitest 文件、314 个用例。生成一致性通过，实体校验覆盖 101 声明、127 张表、1176 列，零声明、契约和 schema 错误。构建保留既有大 chunk 提示。安全技能及目录规则同步后，`check-rules` 和 Skill 元数据校验通过。
- 隔离 PostgreSQL 定向集成通过，覆盖角色复制、变更指纹、撤销影响、资源跨租户隔离、异人审批、领取权限、筛选约束与单次消费。空库备份恢复演练核对 195 组表和序列，验证恢复后的管理员登录。
- `go run ./cmd/horizon-cli test-security` 退出码 0，包含隔离 PostgreSQL 的 `test-integration`，126 个顶层用例通过，耗时 1170.840 秒。Redis Stack 向量替换和 MCP 原生客户端验收跳过，分别需要额外服务和独立测试命令。
- E2E 未执行（按当前指示暂停）。示例应用业务验收和生产切换待执行，远程 CI 未触发。

## IAM-16、17 策略治理与权限复核

- 正式租户管理员在既有 IAM 治理页查找规则问题、查看版本差异和模拟授权。新建草稿沿用 B4 弹窗，版本继续使用 B1 列表和 B2 详情。使用者先看到版本、状态、变更理由和作者，再查看影响的动作、行过滤和脱敏。审批发布需确认，回滚创建待审批草稿。沿用 Runtime 可信组件、表格、弹窗与语义样式，账号和部门通过有界远程搜索选择。英文与简体中文同步。
- 新增只读 Direct 能力 `identity.policy.check` 和 `identity.policy.searchContexts`。规则检查返回首个结构问题的行号，并报告当前部署目录中无法匹配的动作、过滤字段、脱敏字段和可信属性。结构冲突阻止草稿和发布。部署不匹配作为诊断线索，支持对尚未注册的能力预先配置限制。运行时继续拒绝匹配但无法实施过滤或脱敏的请求。
- `identity.policy.simulate` 在只读快照内以已注册能力的权限和字段声明比较生效版及草稿。结果显示决定、原因码、绑定要求、行过滤和遮盖字段。页面可选当前租户的有效账号和部门。其他主体的具体绑定和记录 ID 由受控能力输入支持，当前页模拟基身份授权，不执行业务能力或业务查询。
- 发布写入审批主体、决策时间与版本。数据库拒绝同一主体或账号的自批，以及过时草稿。没有已记录作者主体的旧草稿需要重建，已有发布仍可查看和作为回滚来源。复用已有 Direct 事务和持久化草稿，版本头与 Outbox 同事务更新。
- 定期及手动快照复用 `identity.review`。现在也逐项盘点主部门与附加部门上的直接允许，含对应的账号、部门、权限和原规则。决定时核对原规则，变化后标记失效。撤销写入当前部门的显式拒绝，避免恢复继承权限，其他权限继续按各自规则判定。保留和撤销均要求理由。复核列表按状态和游标查询。
- 风险报告新增按账号统计的重复登录失败，以及按部门和权限列出的敏感直接允许。重复失败阈值为窗口内五次，两个明细各最多 50 项，直接授权总数基于全部匹配项。规则沿用人工基身份和正式 ALL。风险模块使用现有报告布局及 DataTable，无新增页面协议、Runtime Binding 或工具入口。
- 迁移 92 增补策略主体和复核快照字段，SQL 与能力 SDK 从源生成。真实浏览器、窄屏与主题实景按 E2E 暂停规则留待验收。

本地 `generate` 和 `ci-fast` 均退出 0。64 个前端文件、316 个用例通过，含规则冲突检查和远程选择模拟上下文。生成一致性通过，实体检查覆盖 101 个声明、127 张表及 1182 列，零声明、契约和 schema 错误。Vite 保留既有大 chunk 提示。安全 Skill 元数据校验通过。

隔离 PostgreSQL 的最新定向组通过，涵盖跨租户模拟拒绝、同主体自批拒绝、权限覆盖变更后的陈旧复核、主部门和附加部门的复核撤权、继承权限拦截，以及风险报表窗口与租户隔离。迁移 92 的空库备份恢复核对 195 组表与序列，并验证管理员登录。

`go run ./cmd/horizon-cli test-security` 退出 0，隔离 PostgreSQL 集成耗时 1110.680 秒，128 个顶层用例通过。Redis Stack 向量替换和 MCP 原生客户端验收跳过。E2E 未执行（按当前指示暂停）。远程 CI 未触发，生产切换待部署。


## 应用与 IAM 边界调整

管理应用 `com.horizon.identity` 贡献账号、企业、部门和权限 Runtime 页面，并注册账号及组织管理的 Gateway 能力。IAM 管理主体、凭据、租户成员、会话、授权、资源和审计，并执行应用请求的账号归属和组织结构更新。登录页、注册和找回密码通过应用入口绑定命名空间，默认入口为 `/login`，命名入口为 `/sign-in/:application`。OIDC 和 SAML 登录及回调由默认管理应用提供。

应用命名空间由部署清单中的 `signIn.id` 和 `signIn.namespace` 确定。用户名在同一应用内唯一，不同应用可以使用同一用户名。数据库 `realm` 列存储内部凭据命名空间。管理应用复用 `local`，因此平台 bootstrap 创建的管理员可通过 `/login` 登录。切换以新库为基线，无需迁移旧认证域中的账号。

迁移 93 降级时恢复租户内用户名唯一约束。同一租户已有来自不同应用的同名账号时，该约束会阻止降级。

隔离 PostgreSQL 的 `TestIAMApplicationLoginKeepsSameNamedAccountsSeparate` 验证两个应用的同名账号可以分别注册和登录，单个应用内大小写不同的同名注册不会签发注册验证码，登录密码和找回令牌不能跨应用使用。`test-iam-migration-drill` 验证空库 195 组表和序列完成备份恢复，恢复后可用初始管理员登录。

本批次 `go run ./cmd/horizon-cli ci-fast` 退出 0。64 个 Vitest 文件共 317 个用例通过，Go、双语资源、生成一致性、实体、Workflow 与构建检查通过。实体检查覆盖 101 个声明、127 张表、1182 列，错误为零。

## 统一组织登记（历史实现）

当前行为见本页的「租户单组织」。

组织目录作为 `com.horizon.organization-registry` 内置应用随部署装配，数据库迁移随应用启动。组织 ID 在部署内稳定，名称可重名、更名；新登记在同一事务中建立创建租户的有效 `managed` 关系并写入 Outbox。目录读取仅返回当前租户有效关系允许的字段。组织单元的父节点必须属于同一组织，同一组织的树写入串行化，移动时拒绝把节点挂到自己或后代。组织成员确认和撤销只更新组织登记的成员关系。确认要求有效的本租户人类主体，由另一位有正式管理权限的操作者执行。

`/organization-registry` 使用 B1/L1 目录页。管理员可以按名称搜索，在服务端分页结果中查看类型、核验状态和生命周期，并通过短表单登记和更名。搜索及分页由 Runtime Provider 绑定，界面复用 `page-heading`、`table-panel` 和表单组件，状态和提示同步维护 en 与 zh-CN。组织、单元和成员的写能力使用直接执行，业务写入和事件同事务落库。

部署初始化时建立租户所有者组织和初始管理员。IAM 分别校验有效租户成员关系和代表组织的资格，供应商会话使用已核验的组织身份绑定。

示例应用从插件版本 1.0.2 起移除 `/supplier-portal`，已有供应商会话进入账号安全页。业务档案保留独立的供应商 ID，通过 `example.supplier.organization.bind` 关联组织。IAM 在选择供应商身份、认证会话和恢复任务时重新校验供应商档案、目标租户可见性、已核验组织成员资格及当前绑定专属角色。供应商身份权限排除基础部门角色、账号覆盖和临时授权。订单列表、详情、导出和写入继续执行租户、供应商和代表组织检查。能力执行记录保留代表组织和历史门户标识，用于关联调用审计和身份绑定。

本阶段本地 `ci-fast` 通过，64 个 Vitest 文件共 317 个用例通过，生成可复现，实体检查覆盖 105 个声明、131 张表、1211 列且错误为零。隔离 PostgreSQL 的组织登记专项通过。`test-security` 在集成阶段达到 20 分钟默认上限，已完成的 126 个顶层用例通过，剩余 4 个用例通过定向补跑。Redis Stack 向量替换与 MCP 原生客户端验收分别跳过。E2E 未执行（按当前指示暂停）。远程 CI 未触发。

IAM 接入批次的本地 `generate` 和 `ci-fast` 通过，64 个 Vitest 文件共 317 个用例通过，实体检查覆盖 106 个声明、132 张表、1216 列且错误为零。`test-security` 的 20 分钟运行中通过 123 个顶层 PostgreSQL 集成用例，剩余 9 个在最终源码上补跑通过，Redis Stack 与 MCP 原生客户端各跳过一项。最终隔离 PostgreSQL 专项复验了租户 owner、旧绑定隔离、伪造代表组织的拒绝及审计、调用上下文持久化、工作流主体恢复，以及组织成员和门户授权撤销。E2E 未执行（按当前指示暂停）。远程 CI 未触发，变更未部署。

### 内部部门切换

内核迁移 95 提供部门目录接口，组织登记应用迁移 3 在启动时导入内部公司和部门树，并将该目录切到 OrgUnit。导入按租户、公司和既有部门 ID 建立映射，保留父子关系、状态和现有账号、角色、会话及业务外键。已有 owner 关系通过初始管理员的公司归属复用，同名组织无需合并。新租户在创建首个部门前先登记隔离边界，初始化的租户 owner 复用首个内部公司的登记组织。

内部部门继续从现有后台和 `identity.department.*` 管理。部门指令复查现有权限，并在同一事务写入 OrgUnit、兼容部门记录及 Outbox；移动在公司范围串行并检查循环，删除空部门时归档对应 OrgUnit。IAM 工作上下文、部门详情和部门树数据范围从 OrgUnit 目录读取。Registry 的通用单元写能力只编辑独立组织的树。应用迁移降级会移除这次建立的部门映射和派生单元，保留原部门记录及 ID，因此重新升级时可能重新分配派生 OrgUnit ID。

最终 `generate` 与 `ci-fast` 通过。64 个 Vitest 文件共 317 个用例通过，生成一致性通过，实体检查覆盖 107 个声明、133 张表、1220 列，错误为零。真实 PostgreSQL 专项验证旧版 Registry 升级保留 owner 和部门标识、部门移动后范围即时变化、冲突回滚、归档，以及停用部门后的会话拒绝。`test-security` 的默认 20 分钟运行通过 114 个顶层集成用例，最初失败的 4 项和超时未完成的 15 项均通过补跑，Redis Stack 与 MCP 原生客户端各跳过一项。E2E 未执行（按当前指示暂停）。远程 CI 未触发，变更未部署。


### IAM 无部门组织授权

`identity.account.createUnplaced` 创建无默认授权的凭据账号和租户成员，旧组织和部门外键存为 NULL。`identity.organizationAccess.bind` 由当前租户管理员调用，核验主体的有效组织成员资格及该组织与目标租户的有效关系，再建立 `iam.organization` 绑定。`identity.roleGrant.create` 为该绑定单独授予限时角色。角色授予和组织成员资格均可撤销，任一失效都会影响后续授权。

`GET /api/auth/identities` 返回部署租户里当前账号可用的登录身份。`POST /api/auth/identity` 接收身份绑定，服务端重新核验成员资格、绑定和角色授权，并轮换会话校验令牌。应用根据核验结果进入组织门户。

迁移 96 的降级会恢复旧账号表的部门和组织非空约束。存在无部门账号时，PostgreSQL 会拒绝降级并回滚该次迁移。

隔离 PostgreSQL 的 `TestIAMOrganizationAuthorityWithoutDepartment` 验证了迁移往返、真实组织成员核验、租户及绑定隔离、会话和调用记录持久化、部门范围过滤，以及角色或成员撤销后会话与任务恢复的拒绝。

本地 `ci-fast` 通过，64 个 Vitest 文件共 317 个用例通过，生成可复现，实体检查覆盖 107 个声明、133 张表和 1220 列，错误为零。安全门禁中的 Go 单元检查通过，PostgreSQL 集成阶段达到 20 分钟上限前通过 119 个顶层用例，剩余 15 个在新建隔离库中补跑通过。Redis Stack 向量替换与 MCP 原生客户端验收各跳过一项。E2E 未执行（按当前指示暂停）。远程 CI 未触发，变更未部署。

### 身份应用改名与 IAM 拆分

身份应用注册为 `com.horizon.identity`，代码位于 `applications/identity/`，前端位于 `apps/web/src/features/identity/`。安装标识 `system.identity` 延续既有页面配置，应用登录使用 `/sign-in/identity`，旧命名入口作为别名。身份应用提供密码登录、注册与找回密码的 HTTP 处理逻辑，由共享 Goa 传输层执行解码和防护，IAM 校验凭据及建立会话。

迁移 97 允许主体凭受保护的租户管理员角色或显式授予的有效租户角色建立基础上下文。创建租户时，IAM 初始化主体、登录凭据、租户成员和角色。身份应用和组织登记在同一事务中初始化管理员档案、组织与成员关系。首任管理员无需部门即可操作 IAM 审查。普通成员只有持有目标租户的有效授权才能选择基础上下文，撤权后会话和任务恢复重新校验。

身份应用迁移保存管理员档案到 `identity_admin_profiles`，原企业、公司、部门及部门任职相关表分别转为 `identity_organizations`、`identity_departments`、`identity_department_memberships`、`identity_department_membership_roles`。改名保留主键、外键、序列和触发器。组织主档依旧由独立的 Organization Registry 持有。旧名称目前由兼容视图提供，当前发布的授权、审批和任务记录仍可读取历史上下文。

迁移尚未解除旧账号与部门的外键、部门写入处理器、预定义部门授权范围以及部分凭据、审批和任务记录对部门 ID 的依赖。旧账号的显示资料仍需迁出并切换读写入口。兼容视图须在这些依赖替换及旧授权等价性复核完成后移除。

本地 `ci-fast` 和 `test-security` 退出 0。Vitest 通过 317 项，真实 PostgreSQL 集成通过 134 项，Redis Stack 向量替换和 MCP 原生客户端各跳过 1 项。独立空库执行部署 bootstrap 后，记录为 0 个部门、1 个身份应用管理员档案、1 个 IAM 租户管理员。E2E 未执行（按当前指示暂停）。远程 CI 未触发，变更未部署。

### 身份应用接管资料、结构写入与旧范围解析

身份应用迁移 3 将已有的人类账号姓名和邮箱迁入 `identity_account_profiles`，并在同一写入事务内维护资料。IAM 账号表中的兼容字段保留登录名回退和空邮箱。账号目录视图向既有的账号、通知、项目和审查读取入口提供应用资料。回滚会将应用资料写回旧列。机器主体名称仍为 IAM 主体标识。

企业、公司及部门的写入契约与执行器由身份应用注册。组织、部门及自定义部门范围由部署注册的身份应用解析器在当前事务内解释。IAM 继续判断权限、审查和撤销。未配置解析器时，相关旧范围拒绝访问。

新建机器主体可以省略部门，并使用租户或本人范围授予权限。机器主体与签发者均无部门时，凭据的两个旧部门字段均为空。恢复时按当前主体、租户和签发者授权检查，凭据撤销后不能恢复。迁移 99 保留既有凭据及其旧绑定。新建定时服务任务只保存租户、账号和凭据执行所需的引用，不复制部门。恢复时读取签发凭据，旧任务仍可比对保存的历史部门值。

旧账号部门外键、部门型会话、临时访问和邀请，以及旧审批、工作流和 MCP 记录中的部门上下文仍依赖兼容机制。组织和部门的兼容视图目前供这些历史路径使用。

本地 `ci-fast` 和 `test-security` 退出 0。Vitest 通过 317 项，PostgreSQL 集成通过 134 项。Redis Stack 向量替换和 MCP 原生客户端各跳过 1 项。专项复验覆盖应用资料迁移回滚、旧部门范围、无部门凭据和按原始凭据恢复定时服务。空库执行部署 bootstrap 后生成 1 个身份应用账号档案、1 个 IAM 管理员和 0 个部门。E2E 未执行（按当前指示暂停）。远程 CI 未触发，变更未部署。

### 统一账号创建、注册和邀请

`identity.account.create` 是账号创建入口。账号管理表单使用登录和档案字段，通过单独的角色授权操作授予租户权限。注册验证和成员邀请码只建立租户成员关系。迁移 100 移除注册配置、待验证注册和成员邀请表中的部门字段及外键。以空库作为切换基线。

密码、OIDC 和 SAML 验证凭据后，直接建立部署租户内的授权会话。没有有效授权时保留短时受限会话，用于接受邀请。只有存在多个可用组织身份且无常规账号身份时，才会显示身份选择。

迁移 101 将再认证凭证归入 `iam_reauthentication_proofs`。凭证在当前租户和会话上下文内单次使用，并绑定操作、资源、修订和有效期。切换登录身份会轮换会话校验令牌，原凭证随即失效。

迁移 102 将临时权限申请、审批与通知切换到租户基础上下文。申请支持整个租户或本人，审批保留独立审批权限、正式权限上限、到期和撤销。迁移在含有原部门型申请的数据库上会拒绝执行，空库安装采用新的数据契约。

迁移 103 从机器凭据和 Agent 委托删除部门字段。恢复凭据时重新验证租户、主体、有效期、签发者权限和目标能力。Agent 委托还原委托人和受信任的组织绑定，保留资源、风险及使用次数限制。迁移拒绝重新解释原部门型凭据和委托。

迁移 104 为远程 MCP 凭据和调用审计移除部门字段。令牌的签发、发现、撤销和本机配对使用当前租户、账号及可选身份绑定。授权仿真通过账号搜索租户基础上下文，具体身份绑定仍由服务端核验。

迁移 105 将访问审查的目标收敛为租户成员关系和独立角色授权。审查撤销只作用于快照中的授权标识。同一主体重新获得同一角色时，旧审查快照不会撤销新的角色授权。

迁移 106 删除浏览器会话中的部门列。IAM 新建角色、机器授权和临时授权只保存能力、效果及租户或本人范围。身份应用仍独立管理组织架构、部门和账号档案。

当前主体恢复只解析有效租户成员关系及当前身份绑定的授权。旧部门归属无法作为会话、后台执行或授权仿真的可信身份。账号表和旧部门授权表仍有历史字段，库存报表及部分应用查询也仍引用这些结构。

本地 `ci-fast` 通过，包含 317 项前端测试、实体及生成一致性检查。隔离 PostgreSQL 上最后两组共 15 项定向测试通过。全量 PostgreSQL 集成尚未通过，仍有使用原部门夹具的用例，治理文件写入也仍要求应用部门外键。E2E 未执行（按当前指示暂停）。

## 租户单组织

部署初始化在同一事务中为每个新租户初始化一个组织档案，数据库对租户 ID 实施唯一约束。身份应用的组织管理页提供组织资料编辑和部门管理，部门创建从可信租户解析组织。IAM 按租户授予账号及角色访问权限。示例供应商仍使用业务档案绑定，并按租户与供应商隔离订单。

独立组织登记及代表组织绑定已移除。空库初始化是当前切换基线，既有多组织数据库需单独规划数据归并。
