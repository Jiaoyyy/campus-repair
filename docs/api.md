# 已实现接口

接口已实现登录、人工工单、材料管理、带引用的角色资料问答及内部只读 MCP 查询。三项技能和模型原生工具调用尚未实现；能力接口按服务配置返回实际状态。

## 通用规则

除 POST /api/login 和 /health/* 外均须 Authorization: Bearer 令牌。角色和组织取服务端账号，客户端传入身份字段不生效。除流式回答与原文件下载外，成功响应为 JSON；失败为 error.code、message、request_id，并返回 X-Request-ID。无 Cookie 登录，令牌 12 小时有效，登出即撤销。

401 未登录/无效身份，403 不允许的角色操作，404 不存在/不可见资源，409 状态或版本冲突，400 输入非法，429 登录限流，503 数据库暂不可用。JSON 请求体上限 64 KiB。分页默认 20、最大 100。

## 登录

| 接口 | 输入 | 结果 |
| --- | --- | --- |
| POST /api/login | username、password | access_token、token_type、expires_at、user |
| GET /api/me | 无 | user（id、username、display_name、role、org_id） |
| POST /api/logout | 无 | ok；当前令牌撤销 |

同一来源一分钟内五次登录失败后继续尝试返回 429，Retry-After 为 60 秒。当前是单 API 内存限流，不支持多副本。Nginx 覆盖 X-Real-IP，API 仅内网访问。

## 工单

| 接口 | 输入/筛选 | 结果 |
| --- | --- | --- |
| POST /api/work-orders | category、location、description；Idempotency-Key 请求头 | order、replayed；新建 201，重试 200 |
| GET /api/work-orders | page、page_size、status、category、location | items、total、page、page_size |
| GET /api/work-orders/{id} | 工单 ID | order、events、records |
| GET /api/technicians | category；仅管理员 | items：id、display_name、category、pending_count |

category 为 lighting、electrical、plumbing。位置 1 至 200 字，描述 1 至 2000 字，服务端去首尾空白。创建幂等键 8 至 100 字符，有效 24 小时；同用户同键不同内容返回 409。工单包含 number、status、version、归属、指派、时间和显示名称。

所有动作使用 POST /api/work-orders/{id}/{action}，请求必须包含当前 version；成功返回更新后的 order。旧版本、非法状态或终态更新返回 409。

| action | 角色 | 状态及额外字段 |
| --- | --- | --- |
| review | admin | submitted；decision=approve/request_info/reject；后两者必填 reason |
| resubmit | 本人 requester | needs_info；category、location、description |
| assign | admin | unassigned；technician_id |
| reassign | admin | assigned；不同 technician_id、reason |
| accept | 当前 technician | assigned |
| records | 当前 technician | in_progress；measures、result，状态保持处理中但版本增加 |
| complete | 当前 technician | in_progress；measures、result；进入 awaiting_confirmation |
| confirm | 本人 requester | awaiting_confirmation；进入 closed |
| reopen | 本人 requester | awaiting_confirmation；reason；进入 unassigned 并清除当前指派 |
| cancel | 本人 requester | submitted/needs_info/unassigned/assigned；reason |

人员须同组织、启用、负责类别匹配。补充/驳回/改派/重处理/取消原因以及处理措施、结果上限均为 2000 字。成功业务更新、时间线、维修记录和审计同事务保存。历史不可改写。状态码及转换表见 OpenSpec design.md。

## 审计与运行

GET /api/audit 仅管理员，支持 resource_id、page、page_size，返回本组织 items、total。每项含请求标识、动作、操作者/资源 ID、结果、最小元数据及时间，不含登录凭据。

GET /health/live 仅表示进程存活；GET /health/ready 检查数据库。数据库故障时前者 200、后者 503，Qdrant/模型尚未启用不会阻断人工流程。

## 视觉优化后的列表与概览

`GET /api/work-orders/summary` 返回 `total`、`action`、`active`、`finished` 和 `statuses`，基于当前角色可见的全部记录；不受列表页码、位置或类别筛选影响。身份及组织/归属过滤与列表一致。

`GET /api/work-orders` 新增 `queue=all|action|active|finished`：

- `action`：报修人的待补充/待确认，管理员的待审核/待派单，维修人员的待接单/处理中。
- `active`：所有非终态；`finished`：已关闭、已驳回、已取消。
- 可与原有状态、类别、位置及分页条件叠加；无效 queue 返回 400。

审计项保留原 ID 与请求标识，并附加 `actor_name` 和 `order_number`。名称/编号只关联同组织资源；非工单操作的编号为空。界面以中文动作、人员名称和工单编号展示，点击工单后仍按普通详情权限校验。

## 材料与管理检索

当前接口均需管理员 Bearer，范围为登录组织；非管理员 403，跨组织材料 404。不带 agent_id 的管理检索仅管理员可用；角色绑定检索见后文。详情、列表与下载均禁缓存。

| 接口 | 输入 / 返回 |
| --- | --- |
| GET /api/materials | page / page_size；items、total、page、page_size |
| POST /api/materials | multipart：title（1–200 字）、demo（true/false）、file；201 返回 id/status=pending |
| GET /api/materials/{id} | material 含 title、filename、demo、status、error、generation、vector_ready、created_at、text |
| GET /api/materials/{id}/file | 原始文件字节，Content-Disposition attachment；需 Authorization，不能把令牌放 URL |
| POST /api/materials/{id}/reindex | 202 返回 id/status/generation；已有 pending/processing 任务返回 409 |
| GET /api/retrieval | q（1–500 字）、mode=keyword/vector/hybrid，默认 keyword；返回 items/mode/scope=organization_management |

材料扩展名、UTF-8、空白校验失败 400，文件超过 10 MiB 返回 413。索引状态为 pending / processing / ready / failed；generation 为 0 表示尚无可检索版本，failed 仍可有旧可用版本。原文内容不可修改或删除。

检索 item 含 chunk_id、material_id、generation、title、demo、sequence、start、end、text、score；范围为从 0 开始的 Unicode 字符 `[start,end)`。两路候选各最多 100 个，授权/版本复核后按 RRF 排序返回最多 4 条。无命中 200 + 空 items；向量配置、嵌入、Qdrant 故障或存在不兼容的已发布材料返回 503 + vector_unavailable，提供关键词方式。有效 agent_id 使用角色绑定范围；非法编号 400，其他组织或角色的助手 404。

/api/capabilities 现返回 knowledge=true、keyword=true、vector=unconfigured/configured/unavailable；configured 仅表示参数齐全且 Qdrant 集合可用，不保证嵌入网关下一次调用成功。chat 返回 configured/unconfigured；mcp 表示内部 MCP 服务在线，assistant 为 chat 已配置或 MCP 在线。单次工具调用仍须绑定和授权。未配置嵌入/MCP 不影响 /health/ready 或人工业务。

切片、租约、模型维度切换和恢复操作见 [知识库说明](knowledge.md)。

## 角色助手与私有会话

| 接口 | 约束 / 返回 |
| --- | --- |
| GET /api/agents | 当前组织、角色且启用的助手；不返回提示词 |
| GET /api/agents?manage=1 | 仅管理员；本组织配置、version、material_ids、tool_names |
| GET /api/mcp/tools | 仅管理员；通过内部 MCP Client 发现固定只读工具及输入 schema |
| PUT /api/agents/{id} | 仅管理员；name、role、prompt、enabled、version、material_ids、tool_names；冲突 409，越界绑定 400 |
| GET /api/retrieval?agent_id=...&q=...&mode=... | 校验助手角色/组织/启用，限绑定且已发布材料；scope=agent_binding |
| POST /api/conversations | agent_id；201 返回 conversation，创建时重读配置 |
| GET /api/conversations | 仅本人，page/page_size 分页 |
| GET /api/conversations/{id}/messages | 仅本人；最近页默认 20 条，页内按时间顺序，返回 conversation/items/total/page |
| POST /api/conversations/{id}/messages | content（1–2000 个 Unicode 字符）、mode（默认 keyword）；fetch SSE，不能自动重试 |
| GET /api/conversations/{id}/citations/{message}/{chunk} | 本人已完成消息实际引用且当前仍授权；返回 evidence 和完整原文 text，不可见 404 |

会话所有权对管理员也适用；同组织他人的会话和不存在 ID 一样返回 404。助手停用后本人可读历史，但不能新发消息或打开失效来源。消息状态 pending/generating/complete/failed/cancelled；只有 complete 消息进入最近十条上下文。并发生成 409，无模型配置 503 + chat_unconfigured。

SSE 事件：meta（会话/消息/请求/配置版本、skill=none、可选 tool）、delta（text）、done（message_id/answer/citations/warning）或 error（code/message/partial）。明确的工单进度/已分配任务/可派人员问题会调用对应已绑定的只读 MCP 工具并返回真实数据，失败使用 tool_unavailable，不伪造实时结果。实际 citation 含 number 及检索 Evidence 字段；无效编号移除并标明无法验证，不将候选都当引用。中断返回部分正文并持久化；历史和来源读取重新验证绑定及索引版本。

配置、恢复、超时与当前边界见 [助手说明](assistant.md)。
