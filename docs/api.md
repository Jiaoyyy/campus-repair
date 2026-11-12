# 已实现接口

接口已实现登录、人工工单、材料管理、带引用的角色资料问答及内部只读 MCP 查询。已实现原生只读工具决策；完整三项技能和通用多步工具循环尚未实现；能力接口按服务配置返回实际状态。

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
| POST /api/conversations/{id}/messages | content（1–2000 个 Unicode 字符）；fetch SSE，检索由服务端决定，不能自动重试 |
| GET /api/conversations/{id}/citations/{message}/{chunk} | 本人已完成消息实际引用且当前仍授权；返回 evidence 和完整原文 text，不可见 404 |

会话所有权对管理员也适用；同组织他人的会话和不存在 ID 一样返回 404。助手停用后本人可读历史，但不能新发消息或打开失效来源。消息状态 pending/generating/complete/failed/cancelled；只有 complete 消息进入最近十条上下文。并发生成 409，无模型配置 503 + chat_unconfigured。

SSE 事件：meta（会话/消息/请求/配置版本、skill=none、可选 tool）、delta（text）、done（message_id/answer/citations/warning）或 error（code/message/partial）。明确的工单进度/已分配任务/可派人员问题会调用对应已绑定的只读 MCP 工具并返回真实数据，失败使用 tool_unavailable，不伪造实时结果。实际 citation 含 number 及检索 Evidence 字段；无效编号移除并标明无法验证，不将候选都当引用。中断返回部分正文并持久化；历史和来源读取重新验证绑定及索引版本。

配置、恢复、超时与当前边界见 [助手说明](assistant.md)。

对话接口为兼容旧客户端仍接收合法的 mode 字符串，但不采用该值选择检索算法；新客户端只发送 content。问候不检索；报修相关问题自动使用授权资料，无材料/无命中仍调用模型。混合查询失败可回退关键词并在 done.warning 明确降级，全部检索不可用也可继续追问但不能核实制度事实。管理 GET /api/retrieval 的显式 mode 与 503 契约保持。

## 对话式报修草稿（2026-10-23）

仅报修人明确请求“帮我报修”等操作时进入结构化整理；有效草稿存在时可多轮补充。普通问答、制度问题、问候及 MCP 查询继续按原规则处理。本轮仍为 `skill=none`，不表示三项可配置技能已完成。

SSE `done` 及消息历史的完整助手消息新增可选 `draft`：

```json
{"message_id": 2, "conversation_id": 1, "category": "lighting", "location": "三号楼201", "description": "灯不亮", "version": 1, "status": "active", "idempotency_key": "<服务端随机生成>"}
```

类别/位置/描述可暂为空；位置最多200个Unicode字符、描述最多2000个字符。模型结构严格校验，位置/描述须来自本人用户输入或保存过的字段，泛称“宿舍”仍视为缺少具体位置。非法输出、取消生成或失败不发布新卡片；新卡片将同会话旧有效草稿标为 `superseded`。

- `GET /api/conversations/{id}/drafts/{message}`：读取本人草稿；历史 `GET .../messages` 同样返回草稿。报修人以外角色403，他人/跨组织404。
- `PUT /api/conversations/{id}/drafts/{message}`：body `{category, location, description, version, status}`，status仅 `active`（保存）或 `cancelled`（取消）；返回 `{draft}`。实际修改才递增version并换幂等键，同版本同内容保存保持原键。旧版本、生成中、取消/被替代/已提交409；助手停用不可编辑。
- `POST /api/work-orders`：原人工提交保持兼容。卡片确认时在原三字段body增加 `draft_message_id`、`draft_version`，`Idempotency-Key`须为草稿返回的键。字段须与已保存内容一致且满足原有必填校验。服务端从登录身份校验报修人、组织与会话归属，不采用客户端自报身份。

草稿提交与工单/事件/幂等/审计使用同一事务；首次201，重放200，返回原有 `{order, replayed}`。submitted草稿增加 `order_id`、`order_number`；即使幂等记录过期，仍返回关联原工单。失败整体回滚；取消草稿不等于取消工单。草稿生成、修改/取消、确认分别有 `draft.generate`、`draft.update`、`draft.submit` 元数据审计，不记录完整私有内容。没有向模型或 MCP 增加业务写入口。

## 多场景业务查询（2026-10-23）

新增只读 MCP 工具 `query_work_orders`，三角色均可绑定，但结果仍分别限定本人报修、当前指派任务、本组织工单。参数仅 `{queue, status, category, number, period, location, keyword, date_from, date_to}`：queue为all/action/active/finished，status为现有状态码，category为三类故障，number为完整工单号，period为all/today/recent/yesterday/this_week/last_week/this_month；location/keyword为最多200字的字面子串，date_from/date_to为含起止日最多366天的北京时间日期，不能与相对period混用。不接受用户、组织、角色、任意SQL或写动作。默认种子助手通过一次性迁移6绑定；管理员解绑后重启不恢复，自定义助手仍需发现后绑定。

数量基于完整授权匹配集，列表最多5条。`action`复用页面口径：报修人待补充/待确认，维修人员待接单/处理中，管理员待审核/待派单。今天按Asia/Shanghai自然日换算UTC：报修人/管理员按创建时间，维修人员按当前指派最近一次assign/reassign事件。模型决策支持上周等相对日期及明确日期范围，返回实际起止日期；原有无模型关键词降级仍限今天或不限日期。

SSE在meta之后可发送兼容可忽略的 `phase` 事件，data为 `{message}`，提示授权查询、资料核对或草稿整理阶段；原delta/done/error终止规则不变。`done`及完整助手历史消息新增可选 `business`：

```json
{"kind":"orders","query":{"queue":"action","period":"all"},"total":8,"statuses":{"submitted":5,"unassigned":3},"items":[{"id":1,"number":"RP...","status":"submitted","category":"lighting","location":"演示楼201","description":"虚构演示：灯不亮"}],"queried_at":"2026-10-23T01:00:00Z","historical":false}
```

`items`这里只示意一条；实际最多5条。人员查询返回kind=candidates，附带候选的ID、显示名、类别及pending_count/in_progress_count，按两者总量排序给只读建议。工单正文和业务事件不被修改。

结果快照关联本人消息持久化，历史返回historical=true；工单卡片再次检查当前可见性，改派后原维修人员的旧卡片不再包含无权工单。总量/状态计数仍表示查询时快照，详情打开走原授权接口。最新消息只有为成功业务查询时，短筛选追问才继承该语境；问候/资料问答不继承。无模型配置仍可执行已绑定业务查询及有效追问；MCP故障或解绑明确失败，不让模型猜数量。工具审计保留白名单筛选参数和耗时，不保存原始提问。

注意旧 `/api/technicians` 的 `pending_count` 是待接单与处理中总负载；MCP候选的 `pending_count` 专指待接单，另有 `in_progress_count`。两者核对时应使用MCP两项之和。

配置模型时先通过原生tool_calls生成只读查询计划，meta.tool为兼容旧客户端保留的初步意图提示，不作为实际调用证据；查询done另含实际tool，业务成功以business与MCP审计为准。语义计划非法返回query_plan_failed，执行失败返回tool_unavailable，均不发布business。普通模型正文完整缓冲并检查后才输出delta，未通过的缓冲内容不写为可见部分回答；phase及唯一done/error规则保持。新增参数的精确口径见 [MCP说明](mcp.md)。

查询计划阶段网关HTTP错误分为query_rate_limited（429）、query_quota_exhausted（明确额度不足）、query_gateway_unavailable；连接拒绝、DNS/TLS失败、请求及流读取超时同样返回query_gateway_unavailable，超时明确显示“超时”，不能提示用户改问法。非法模型计划仍为query_plan_failed。各错误均不发布business，不自动重试；错误正文/私有提示词不写日志，诊断仅含固定connection/timeout阶段码、状态码及额度分类，不保存底层错误中的URL。

原生查询计划通过严格工具/参数校验后复核日期来源：独立问题未指定时间则使用period=all并清除模型擅自生成的date_from/date_to；有效短追问继承上一查询的实际日期，明确时间表达保留原契约。楼名使用location字面子串即可匹配楼层，不要求楼层必填。结果正文明确不限日期；带日期的空结果说明范围外仍可能有未结束工单，不执行额外查询或扩大时间。MCP接口本身的日期字段和授权规则不变。

稳定化契约：回答正文delta仅在完成事务最终校验成功后发布，失败partial不含未核验缓冲。done已保存而客户端断开时，应读取历史确认状态，不能自动重问。SSE客户端严格核对本轮conversation_id/message_id与字段类型，支持跨字节CRLF/UTF-8分片；失败仍保留输入供编辑。只读工具的null/非字符串/重复键参数被拒绝。

## 上门安排（产品体验迭代）

上门信息独立于九种工单主状态，所有读写按当前登录组织和工单归属校验；模型和MCP没有写工具。建单请求及草稿三字段保持不变，上门时间在建单后选填。

- `GET /api/work-orders/{id}/visit`：返回`context`、工单`version`、当前用户`allowed_actions`、`as_of`。context包含`availability_start/end`、有效`visit`（id/start/end/stage/departed_at/arrived_at）、`exception/reason/expected_resume_at`、`needs_availability`及`revisit_required`。无预约为null；历史在工单时间线保留。
- `PUT /api/work-orders/{id}/visit/availability`：仅本人报修人，body为`version,start,end,reason`；start/end同时为null表示清除。开始须在未来14天内、时长30分钟至4小时，使用RFC3339带时区时间。已确认预约调整需原因并撤销旧预约；出发/到场后不能修改。
- `POST /api/work-orders/{id}/visit/actions/{action}`：仅接单后的当前维修人员，body均含version。schedule附start/end（位于最新偏好内，改已有预约需reason）；depart/arrive记录服务端时间；no-show须先到场并附reason；wait-parts附reason及未来expected_resume_at；resume附reason且仅恢复待备件。出发时已过预约结束时间返回409，已出发后允许迟到到场。
- 上门写入成功返回`{version: 新版本}`，客户端重新读取工单和上门信息。未知字段/非法时间为400，禁止角色403，不可见404，旧版本/非法阶段409，依赖或审计失败503且整体回滚。网络结果未知时先重读，不自动重放写操作。
- 白跑后必须本人重新提供时间并完成新预约到场才能结单。待备件期间禁止结单；恢复不清除既存的重新上门要求。改派、重处理和终止动作撤销现行预约，历史不删除。

保存偏好示例（Bearer和ID由实际登录/建单结果取得，日期需替换为未来有效窗口）：

```json
{"version":1,"start":"2026-10-24T14:00:00+08:00","end":"2026-10-24T16:00:00+08:00","reason":"下午方便在场"}
```

### 工作台与协调统计

`GET /api/repair-workbench?view=…&page=1&page_size=20`返回items（order/context/allowed_actions）、counts、total、page/page_size及as_of，响应中的数量与列表在一致性快照内读取。

- 维修人员：pending待接单；unscheduled为处理中无有效预约且非待备件；today为有效预约开始日在北京时间今天；processing为全部处理中。队列可重叠，今日按预约开始升序/ID升序，其余按更新时间降序/ID降序。
- 管理员：overdue为有效预约尚未到场且结束时间严格早于as_of（包含已出发）；reschedule为处理中曾记录无人、尚无新有效预约；parts为处理中尚未恢复的待备件。逾期按约定结束时间升序/ID升序，其余按更新时间降序/ID降序。
- `GET /api/repair-workbench/metrics?date_from=YYYY-MM-DD&date_to=YYYY-MM-DD`仅管理员；缺省近7天含今天，指定时须同时提供日期，最长366天。counts含当前overdue/parts及区间内no_show。白跑按本组织唯一预约的no_show_at北京时间日期计次，不按改约、备件或重试计数。返回实际日期范围和as_of，不代表全校白跑率或改善比例。

隔离验收：`python3 apitest/verify_visits.py`，仅固定的draft-check模拟依赖环境；不向真实网关发送请求。
