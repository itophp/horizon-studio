# 数据库约定

PostgreSQL 18.6 开发容器仅绑定 127.0.0.1:54329；示例账号只用于本地。生产连接必须独立配置、启用 TLS 和最小权限。

[初始化 SQL](../deployment/database/init.sql) 定义空库的结构、平台模板和默认管理员。使用 `./horizon-cli bootstrap` 一次性执行。初始化在事务中持有 schema 级 advisory lock，成功后写入完成标记。sqlc 和实体检查读取同一份 SQL。API 启动时按已有部署定义协调 Runtime。

technical_demo_records 是显式技术演示表，不是正式业务实体。所有 list/get/update/delete SQL 共享租户条件及 scope 条件；更新删除在原子 SQL 中检查范围。查询只投影 id/label，不读取 internal_note。禁止通过客户端指定当前 tenant 或 scope。

技术演示的 ScopeResolver 只授予 OWN。应用目录使用 `identity_organizations`、`identity_departments` 和 `identity_department_memberships`。IAM 按租户成员关系、身份绑定和角色授予解析授权。会话仅存令牌摘要。

集成测试显式配置 `TEST_DATABASE_URL`，为每次测试创建唯一 schema，并让初始化和连接池使用相同的 search_path。测试结束时清理该 schema。数据库测试覆盖空库初始化、重复执行、授权范围、回滚、并发和约束。

RLS 当前未启用；启用前必须定义非 BYPASSRLS 应用角色、事务内上下文、连接池复用及完整 CRUD 负向测试。当前安全边界是可信身份、Casbin、Repository 约束与 SQL 投影，不能以管理员数据库账号的 RLS 测试代替实际角色验证。

临时访问表保存临时申请与审批/撤回/撤销审计。原始请求和最终授权分开保存，相对范围批准时固定解析，每次认证按数据库时钟排除到期记录。审批使用带 tenant、操作者与 pending 状态的原子更新，防止并发重复批准。申请人与审批人的可信来源、最小目录字段投影和批准范围的正式授权上限见 ADR 003。
