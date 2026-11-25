# 个人邮箱应用实施记录

> 边界说明：业务应用与底座核心的拆分原则见 [业务应用插件化与内核边界说明](application-plugin-boundaries.md)。当前邮箱实现中仍有部分集成点待按该文档重构为插件化贡献。

## 当前实现

- 业务模块位于 `applications/mail`，以 IAM 账号为唯一拥有者。每个邮箱使用 SMTP 发件，收件可选 POP3 或 IMAP。
- `mail.mailbox.onboard` 在同一事务创建 mailbox、通道和密封凭证，并验证收发连接。双通道成功后自动激活，失败时保留 unverified 供修正。第一个邮箱自动成为默认发送邮箱。
- 发送入口 `mail.message.send` 创建正文、收件人、附件绑定和 `mail_outbound_deliveries`。系统 worker 经 `mail.outbound.process` 执行已提交的投递，待确认的发送请求由提交能力处理。未到上限的失败自动回到 queued 并指数退避，超时标记 ambiguous。
- POP3S worker 经 `mail.inbox.sync` 按 UIDL 分批复制新邮件及附件，并保留原始远端邮件。导入过的 UID 在选取批次前过滤。
- IMAP worker 经系统专用能力 `mail.inbox.imap.sync` 只读打开 INBOX，按 UID 增量拉取。UIDVALIDITY 变化后重新定位，UID 游标和本地邮件随事务一起提交。收件同步保留远端邮件及已读状态。
- 入站 HTML 与出站 Markdown 生成的 HTML 均经 bluemonday allowlist 清理；可信正文组件使用 sandboxed iframe 展示 HTML，缺失时回退纯文本。
- 页面按邮箱域名及收件协议预填主机、默认端口和用户名。IMAP 可选 TLS（993）或 STARTTLS（143），POP3 使用 TLS（995），端口可自定义。邮箱编辑、凭证轮换、附件上传、发送重试/取消和连接测试均走对应 Gateway capability。

## 安全与一致性边界

- 权限：`mail:read`、`mail:send`、`mail:configure` 仅本人；`mail:sync` 为系统 worker 专用，不临时授予。
- 凭证：AES-GCM 密文，AAD 绑定租户、邮箱、用途和版本；仅 fingerprint/版本可投影。
- 网络使用 `MAIL_ALLOWED_ENDPOINTS` 精确匹配主机和端口，并校验证书。SMTP 和 IMAP 支持 TLS/STARTTLS，POP3 使用直连 TLS。
- 外部发送按至少一次处理；provider 超时标记 ambiguous。POP3 和 IMAP 本地导入按远端 UID 去重。

## 界面实施

1. 账号持有人在邮箱配置中连接或维护自己的外部邮箱，在收件箱、已发送和发送队列中查看邮件状态。
2. 收件箱和已发送使用目录与列表两栏，窄屏时目录链接换行到列表上方。列表优先展示主题、发件人和日期，未读邮件同时使用圆点、加粗和文字。分页由 URL 参数控制。
3. 阅读使用独立详情页，正文按邮件格式展示，可切换至纯文本。发件人、收件人、抄送、时间、回复与附件在详情内。已导入邮件的 MIME 编码主题在读取时解码。
4. 配置页把「连接邮箱」放在标题行。邮箱信息排在下方紧凑列表中，编辑、凭证更新、连接测试和启停使用行菜单。短表单使用 EditorDialog，凭证更新单独提交，失败保留输入。
5. 写信采用 B4/L6 独立页面。发件账号、收件信息、Lexical HTML 编辑器和附件分组排列。编辑器输出 HTML，`mail.message.send` 清理 HTML 并生成纯文本备用正文。已有调用方可继续提交 Markdown。退出有修改的写信页时，页面取消按钮要求确认。
6. 六个 V3 页面由 `applications/mail` 装配。目录、Provider 和 Action 由 Runtime 声明，邮件摘要列表、写信、凭证输入与阅读使用可信组件。校验覆盖空列表、已读未读、打开详情、HTML 渲染切换、编辑提交、附件和连接失败。窄屏和浅暗主题的实际视觉验收遵循浏览器执行规则。

## 验证状态

已实现单元/组件测试覆盖凭证 AAD、HTML 清理、MIME 解析、endpoint 精确白名单、超时分类、自动表单、附件接线、发送重试与 sandbox 渲染；真实 PostgreSQL 集成覆盖迁移、多邮箱、失败保存在 unverified、发送事务/事件、审计脱敏与跨账号拒绝。真实外部 SMTP/POP3 服务器互通和浏览器验收须在独立环境中另行执行，不用单元测试代替。
