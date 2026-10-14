# Design

## Context

此仓库从零初始化；制定本设计时只有文档与 OpenSpec 工具，没有业务代码、数据库或运行服务。当前人工工单阶段的实现与验证状态见 README 和 docs/verification.md。选题获教师认可，动机见 proposal.md。七份 specs 定义目标行为；所有下列接口、表与模块为拟新增设计。

## Goals / Non-Goals

**Goals:** 一个可部署的人工工单闭环优先运行；AI 依赖故障不影响人工业务；权限在 API 和 MCP 两层执行；任务和索引可恢复；状态变更具有可核对证据。

**Non-Goals:** 首版单 API 与单 worker，不设计高可用、多租户运营后台或真实校方集成；不引入额外调度/支付系统；不把课程所有后续课件示例照搬成报修功能。

## Decisions

### 1. 运行结构与代码边界

采用 React/TypeScript 前端、Go 模块化单体 API、独立 Go 索引 worker、独立内部 MCP Server、MySQL、Qdrant、Nginx 与 Compose。拟目录为 frontend/、backend/（cmd/api、cmd/worker、cmd/mcp 和内部业务模块）、deploy/、apitest/、docs/。API 与 MCP 共享受授权约束的只读业务查询模块，但 MCP 必须提供实际协议端点。

选择模块化单体便于两人维护和事务一致性；不采用业务微服务，避免跨服务事务和额外运维。worker 单独运行避免索引阻塞 HTTP。后台任务首版用 MySQL 持久任务表和租约；不立即增加消息中间件，后续课程若明确要求消息队列，再单独调整规约与设计。

Nginx 是浏览器唯一入口，转发 /api 和流式请求，关闭 SSE 路径缓冲。API、worker、MCP、MySQL、Qdrant 用内部网络连接，数据服务和 MCP 不映射公网端口。

### 2. 身份、账号与权限

采用 256 位随机不透明 Bearer token，服务端保存带服务密钥的哈希，12 小时有效，登出删除会话；不用 Cookie 认证。浏览器通过 sessionStorage 保存当前标签页令牌，流式请求用带 Authorization 的 fetch，避免 URL 携带秘密。密码使用自适应哈希；种子密码只从环境配置读取。账号 role 为 requester、technician、admin；org_id 与 enabled 均从数据库读取，不接受客户端自报身份。

相较 JWT，持久会话更易立即撤销且已有课堂经验；不增加 refresh token。首版无公开注册、账号管理页面和自助角色变更；部署阶段初始化账号。以角色权限判断 403，再对允许的资源类型执行组织/归属查询，不可见 ID 返回 404。

本组织至少两个报修人、两个类别匹配的维修人员和一个管理员；另一个组织的测试账号只服务隔离验收，不构成多校区运营能力。

### 3. 工单状态与事务

数据库状态码与产品中文状态映射如下：

| 当前状态 | 动作、角色 | 下一状态 |
| --- | --- | --- |
| submitted（待审核） | admin 通过 / 要求补充 / 驳回 | unassigned / needs_info / rejected |
| needs_info（待补充） | 本人补全重提 | submitted |
| unassigned（待派单） | admin 指派有效匹配人员 | assigned |
| assigned（待接单） | 当前人员接单 | in_progress |
| assigned | admin 填理由改派 | assigned，更新指派人 |
| in_progress（处理中） | 当前人员提交措施和结果 | awaiting_confirmation |
| awaiting_confirmation（待确认） | 本人确认 / 填原因重处理 | closed / unassigned |
| submitted、needs_info、unassigned、assigned | 本人填原因取消 | cancelled |

所有其他转换禁止。重处理清空当前指派，旧人员与处理记录保留在不可改写的历史；已关闭/已驳回/已取消为终态。改派仅发生在待接单，处理中人员调度不纳入首版。

工单有单调递增 version。更新 SQL 条件同时包含 org_id、授权归属、当前状态和 version，未更新到记录则在已授权范围确认冲突并返回 409。工单、事件、处理记录及业务审计在同一数据库事务中写入。不选择前端防重复作为唯一保护。

创建请求使用 Idempotency-Key，作用域为 org_id + requester_id + key；存请求摘要、24 小时过期时间与返回工单。相同内容返回原编号，不同内容 409；并发创建通过唯一约束与事务去重。清理过期键后不承诺跨有效期去重。

### 4. 数据模型

| 数据组 | 拟建表与关键职责 |
| --- | --- |
| 身份 | organizations、users、sessions；组织、角色、启用和会话哈希/到期 |
| 人员 | technician_categories；人员负责类别，不按模型猜测 |
| 工单 | work_orders；编号、本人、组织、类别、位置、描述、状态、指派、version |
| 历史 | work_order_events、repair_records；操作者、状态前后、原因、措施、结果 |
| 幂等 | idempotency_records；用户作用域唯一键、请求摘要、响应、到期 |
| 知识库 | knowledge_materials、knowledge_chunks、index_jobs；原文、切片字符范围、索引版本、状态、租约与错误 |
| 助手 | agents、skills、agent_skills、agent_materials、agent_tools；按组织配置和绑定 |
| 会话 | conversations、messages；本人、助手、请求标识、状态与流式部分正文 |
| 审计 | audit_events、ai_call_events；请求、操作者、资源、结果与耗时元数据 |

所有业务查询带 org_id；会话另带 owner_id。时间使用 UTC 保存，页面按 Asia/Shanghai 展示。原文件保存在只挂载到后端和备份工具的卷，以不可猜测存储名保存。

### 5. 接口契约与页面

错误统一为 error.code、message、request_id；字段错误附 fields。401 未登录，403 角色操作不允许，404 不存在/不可见，409 版本或状态冲突，400 输入非法，413 文件过大，429 限流，503 必需依赖不可用。所有受保护接口都读取 Bearer。禁止通用 PATCH 接口让客户端直接写 status、assignee_id 或 org_id。

| 接口组 | 拟定接口 |
| --- | --- |
| 登录 | POST /api/login、POST /api/logout、GET /api/me |
| 工单 | GET/POST /api/work-orders、GET /api/work-orders/{id} |
| 动作 | POST /api/work-orders/{id}/review、resubmit、assign、reassign、accept、records、complete、confirm、reopen、cancel |
| 人员 | GET /api/technicians?category=...，仅管理员 |
| 知识 | GET/POST /api/materials、GET /api/materials/{id}、GET /api/materials/{id}/file、POST /api/materials/{id}/reindex |
| 检索 | GET /api/retrieval?q=...&mode=keyword|vector|hybrid、GET /api/capabilities |
| 助手 | GET /api/agents、管理端配置更新、技能启停/绑定接口 |
| 会话 | GET/POST /api/conversations、GET /api/conversations/{id}/messages、POST /api/conversations/{id}/messages（SSE） |
| 管理 | GET /api/audit、GET /api/statistics，限管理员 |
| 运维 | GET /health/live、GET /health/ready |

动作请求带 version；指派带 technician_id，其他必要原因、维修字段按 spec 校验。分页默认 20、最大 100。首版页面为登录、角色首页/工单列表、提交表单、工单详情/时间线、助手会话；管理端增加人员选择、材料、助手技能配置、审计和状态/类别统计。

### 6. 异步知识索引与检索

UTF-8 .txt/.md 保存原文后入队 index_jobs。worker 原子领取任务并续租，失效租约可重新领取；按字符最大 800、重叠 80 切片，保留原文字符范围与 generation。关键词用 MySQL ngram FULLTEXT，嵌入经服务端兼容网关写 Qdrant，向量 payload 不含正文；正文从 MySQL 回读并复核组织。

每次重建生成新 generation，查询只使用已发布 generation；首次索引失败但关键词已成功时，允许发布为仅关键词可用，明确向量失败。重建失败不替换旧已发布版本。成功原子切换当前 generation，旧向量后台清理；未发布向量不能进入检索。嵌入模型/维度变化需显式重建集合，不混用。

混合检索采用两路组织过滤后 RRF，回读再校验材料绑定和版本；最多给模型四条证据，返回引用索引。输出仅展示实际引用且仍可授权核对的片段。首版材料不提供删除/正文修改，只有上传与重建，避免引用生命周期额外复杂度。

### 7. 智能体、技能与流式会话

初始化三种角色助手；管理员可配置提示词、启停、角色受众、技能、材料及工具绑定。技能首版为内置配置化规则，不允许上传任意执行代码。路由先以当前角色和绑定形成白名单，再按明确意图选择最多一项：补全用于报修草稿，安全建议用知识证据，派单建议仅管理员。模糊输入先追问，不把模型决定当授权。

每次输入重读配置版本，保存本轮所用配置标识；上下文只取本人最近 10 条已完成消息，失败部分不混入下一轮正常历史。用持久会话生成锁避免并发；最大一轮 120 秒，锁设租约，崩溃后标记未完成并释放。模型调用不自动重试，以免重复费用和重复消息。

fetch SSE 返回 meta（conversation_id、request_id、skill）、delta（text）、done（message_id、answer、citations）或 error（code、message）。流建立前返回普通 HTTP 错误；开始后只有一个终止事件。连接断开取消下游请求，持久化 cancelled；模型故障记录 failed；UI 不伪装完成。

无知识命中仍可按提示词追问或解释表单，但不得伪造制度或引用；安全类回答只给受证据支持的提醒。AI 草稿填到可编辑表单后用户提交，业务接口独立校验。禁用、工具越权和提示词注入均在调用之前用服务端规则阻断。

### 8. MCP 协议与身份传播

采用支持 MCP Streamable HTTP 的 Go SDK，在内部网络提供 /mcp，客户端执行 initialize、协商版本、notifications/initialized、tools/list、tools/call。实施前锁定 SDK 及双方兼容协议版本并在接口文档记录；协议测试验证真实消息，不能只测共享业务函数。

API 充当 Host/Client，只连接固定内部 Server。工具白名单为 get_my_work_orders、get_assigned_work_orders、list_dispatch_candidates；schema 不接受用户、组织或角色字段。管理员绑定只选发现的白名单项；本轮客户端和服务器都校验角色与绑定。

API 使用独立服务密钥产生短时签名调用凭据（有效期最多 60 秒），含 session_id、agent_id、request_id、audience 与时间；MCP 验签并回查当前有效会话、账号和工具绑定，从数据库恢复身份。签名凭据不传给模型，不持久日志；不用模型提供身份，也不只靠长期共享服务令牌代表所有用户。每轮最多三次只读工具调用，每次 10 秒超时，不自动重试；失败返回真实错误。

选择内部 HTTP 服务便于协议证据与独立故障测试；不允许任意外部 MCP URL，避免未经授权的数据转发和服务端请求目标扩张。

### 9. 审计、验收与共同开发

业务成功审计与工单事务共写；登录/权限拒绝/AI 调用单独记录，数据库故障至少留下脱敏运行错误与请求标识，不声称已保存数据库审计。AI 审计只存元数据而非完整对话、检索原文或秘密。页面仅管理员看本组织审计；管理员也不能读取他人完整私有会话。

测试分为状态/授权单元测试、带 MySQL/Qdrant 的 API 集成、真实 MCP 协议、模拟模型流故障、三角色端到端。使用隔离测试 Compose 项目与卷，不改变开发演示数据。实际课程网关和公网结果另记，不能用 mock 通过替代五项能力的真实联调。

共同仓库任务分支经 PR 交叉审阅，不自动合并。贡献按实际提交与验收记录，分工见 docs/collaboration.md。

### 10. 页面信息架构

增加 GET /api/work-orders/summary，复用组织及归属过滤统计全部状态数量；列表增加 queue=action/active/finished，角色待办集合与概览共用同一规则。不从分页行数推算整体概览。所有队列均可与原有位置/类别/状态筛选叠加，返回列表保留筛选状态。

详情采用“状态与处理责任 → 主要动作 → 原始报修/最近维修结果 → 事件历史”。报修人待审核不默认打开取消表单；维修处理中默认提交结果，记录进展为次要选择；待接单管理员以等待人员接单为主，改派为次要选择。取消、改派、重新处理必须由用户主动选择，并保留服务端状态和角色校验。

审计接口附加当前组织内人员名称与相关工单编号，保留原 ID 和请求标识用于核对；不能跨组织关联。失败、空态和正在加载使用互斥显示，刷新失败保留旧详情但禁用操作。视觉统一为清晰标题、状态标签、分组数据与可读正文，手机使用工单卡片替代表格。

## Risks / Trade-offs

- [两人范围较大] → 按人工闭环、AI、交付分段验收，不加入附件/通知等扩展。
- [模型错误或注入] → 白名单、双层授权、引用校验与人工确认；关闭所有 AI 写路径。
- [worker 重复领取或索引部分成功] → 租约、版本发布与幂等写入，只查询已发布 generation。
- [数据库与文件不一致] → 文件写入失败撤销材料记录；数据库失败清理临时文件；备份在暂停写入后完成。
- [公网依赖与模型费用] → 服务端限流、超时与能力提示；模拟测试先验证，真实网关联调按需执行。
- [单实例限制] → 首版明确单 API/worker；迁移多副本前增加共享限流并重审租约策略。

## Migration Plan

1. 从空库运行版本化迁移、幂等账号和演示材料初始化；环境和卷与 CampusClaw 分离。
2. 本地验收人工流程，再配置模型与 MCP，核对所有 specs 的证据。
3. 部署者选择公网机器与域名后配置 HTTPS；仅开放统一入口，验证外部访问。
4. 升级前暂停写入，同一时点备份数据库和材料。小变更保留卷回滚镜像；不兼容迁移用配套备份恢复，不执行 down -v。
5. 在隔离环境恢复并重建派生向量，检查账号、工单、材料、历史和引用。

## Open Questions

- 公网主机、域名及证书部署方式，在交付阶段选定，不改变产品权限和流程。
- 实际模型名称、网关和嵌入维度，在 AI 实施前配置并记录，不在规约中写密钥。
- 高慧莹的 GitHub 用户名，获知后邀请为协作者；不影响本地设计与任务拆分。
