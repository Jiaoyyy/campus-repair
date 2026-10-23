# 只读 MCP 工具

当前实现使用官方 Go MCP SDK `github.com/modelcontextprotocol/go-sdk v1.4.0`，与仓库 Go 1.24 构建环境兼容。API 是 Host/Client，内部 `mcp` 容器是 Streamable HTTP Server，端点 `http://mcp:8081/mcp` 仅在 Compose 内网可达。协议测试协商到 `2025-11-25`，依次发送 `initialize`、`notifications/initialized`、`tools/list`、`tools/call`；工具调用返回协议的 `structuredContent`。SDK 版本锁在 `backend/go.mod`。

## 管理与调用

管理员在“助手设置”勾选本组织该角色可用的工具。页面通过 `GET /api/mcp/tools` 使用真实 MCP Client 发现固定白名单，只展示该角色的工具；`PUT /api/agents/{id}` 的 `tool_names` 保存绑定。空数组清空绑定，未提供字段保留绑定；保存使用原有 `version` 防并发覆盖。停用助手或解绑工具立即阻止新调用。首版不接受用户填写 MCP 地址，也不接外部 MCP Server。

| 工具 | 允许角色 | 入参 | 返回范围 |
| --- | --- | --- | --- |
| `get_my_work_orders` | 报修人 | 空对象 | 本人本组织最近 20 条工单的编号、状态、类别、位置和时间 |
| `get_assigned_work_orders` | 维修人员 | 空对象 | 本人已分配的本组织最近 20 条工单的相同字段 |
| `list_dispatch_candidates` | 管理员 | `category`，三种故障类别之一 | 同组织启用且负责该类别的人员 ID、名称、匹配类别、待接单和处理中数量 |
| `query_work_orders` | 三角色，按各自授权范围 | queue/status/category/number/period | 全量计数、状态计数、最近更新最多5条及查询时间 |

模型参数不能指定用户、组织或角色。没有创建、审核、派单、改派或结单工具。`tools/list` 只列出固定四项；每次 `tools/call` 重新检查角色、会话、助手启用状态和工具绑定。未知工具、非法参数、伪造身份字段、跨组织访问和已撤销登录均拒绝。

API 用本地 `MCP_CALL_SECRET` 产生最长 60 秒的 HMAC 签名凭据，载荷含当前登录会话哈希、助手 ID、请求 ID、受众和时间。凭据由 API 放入内部请求的 Authorization 与工具调用 `_meta`，不传给模型或浏览器，也不进审计日志。MCP Server 验签后回查 MySQL 中的有效会话与账号。每个请求 ID 最多三次工具调用，每次 Host 和 Server 均设 10 秒上限；不自动重试。审计只记真实操作者、请求 ID、工具名、最小参数、耗时及成功/拒绝/失败。

配置模型时，助手先使用原生function calling产生单次只读计划，严格组装/校验后经MCP Client/Server执行。普通聊天及资料问答继续原路径，明确草稿走原有提取器；业务结论从真实工具结果生成。未知/写工具、越权参数或无效协议均失败。未配置模型时保留关键词降级。此实现不是通用多步Agent循环，三项完整可配置技能仍未完成。工具失败、超时或无授权时SSE返回error，人工工单页面独立可用。

## 本地运行与验收

从零运行时 `apitest/setup_local.py` 会在被 Git 忽略的 `.env` 生成独立 `MCP_CALL_SECRET`。旧 `.env` 需自行补上至少 32 字符的随机值，随后运行 `docker compose up -d --build api mcp web`。服务无主机端口映射；可在管理员“助手设置”查看已发现工具。

在项目根目录执行：

```sh
docker run --rm --network campus-repair-draft-check_default \
  -v "$PWD/apitest:/verify/apitest:ro" -v "$PWD/tmp/draft-check.env:/verify/.env:ro" \
  -e API_TEST_URL=http://api:8080 -e MCP_TEST_URL=http://mcp:8081/mcp \
  python:3.12-alpine python /verify/apitest/verify_mcp.py
```

脚本须在上述独立隔离项目运行，使用隔离演示账号，给三个角色的助手绑定各自工具，验证协议消息、页面结果一致性、越权/伪造参数/跨组织/过期与登出拒绝、单轮限额、解绑拒绝、审计以及助手 SSE 和消息持久化，最后恢复临时解绑。它会创建一条演示会话，不删除已有数据，不输出密码或令牌。协议错误和慢响应另由 `backend/internal/app/mcp_test.go` 的受控 HTTP 测试覆盖；真实公网和课程网关联调不由这些测试替代。

自然表述支持示例：“我报的维修到哪一步了”“今天派给我的有哪些活”“照明有几位师傅可以安排”。管理员问“我们有几个师傅”时不会调用缺少类别的工具或推断总人数，而是先追问类别；下一轮短句“照明的”结合本人的上一轮问题调用原有角色工具。对话回答不再显示内部 MCP 协议字样或无关人员 ID。角色、组织、会话和绑定校验保持。


## 授权工单概览与筛选（2026-10-23）

新增第四个固定只读工具query_work_orders，角色标记为all，但Host与Server仍从真实会话执行三角色组织/归属过滤。schema为queue/status/category/number/period/location/keyword/date_from/date_to；未知参数、伪造身份和非法枚举被拒绝。返回全量计数、最多5条概要、查询时间及日期口径。每次查询计入原有单轮三次限额和10秒预算，不接受任意SQL、外部地址或业务写动作。具体字段见api.md。

迁移6为种子助手一次性添加绑定并提升配置版本；停用助手不因此启用，旧绑定不改。管理员移除后重启不恢复，自定义助手由管理员显式绑定。发现列表中的role=all表示支持三种合法角色，不能当作不鉴权。

人员负载的页面与MCP字段名历史上不同：页面pending_count为两种进行中状态的总量，MCP分别返回pending_count及in_progress_count；协议验收按两项之和与页面核对。历史工具结果只作为会话快照，不作为后续实时数据。

## 语义查询参数扩展

location为位置字面子串，keyword为原始故障描述字面子串，均最多200字，使用参数绑定LOCATE，百分号和下划线不作为通配符。period支持all/today/recent/yesterday/this_week/last_week/this_month；recent含今天近7天，周一开始自然周。date_from/date_to同时为YYYY-MM-DD，含起止日且最多366天，不得与非all的相对period混用。返回列表增加description，原有组织/角色/归属和快照复核不变。日期依据对管理员/报修人仍是创建时间，对维修人员仍是当前指派最近事件。
