# 只读 MCP 工具

当前实现使用官方 Go MCP SDK `github.com/modelcontextprotocol/go-sdk v1.4.0`，与仓库 Go 1.24 构建环境兼容。API 是 Host/Client，内部 `mcp` 容器是 Streamable HTTP Server，端点 `http://mcp:8081/mcp` 仅在 Compose 内网可达。协议测试协商到 `2025-11-25`，依次发送 `initialize`、`notifications/initialized`、`tools/list`、`tools/call`；工具调用返回协议的 `structuredContent`。SDK 版本锁在 `backend/go.mod`。

## 管理与调用

管理员在“助手设置”勾选本组织该角色可用的工具。页面通过 `GET /api/mcp/tools` 使用真实 MCP Client 发现固定白名单，只展示该角色的工具；`PUT /api/agents/{id}` 的 `tool_names` 保存绑定。空数组清空绑定，未提供字段保留绑定；保存使用原有 `version` 防并发覆盖。停用助手或解绑工具立即阻止新调用。首版不接受用户填写 MCP 地址，也不接外部 MCP Server。

| 工具 | 允许角色 | 入参 | 返回范围 |
| --- | --- | --- | --- |
| `get_my_work_orders` | 报修人 | 空对象 | 本人本组织最近 20 条工单的编号、状态、类别、位置和时间 |
| `get_assigned_work_orders` | 维修人员 | 空对象 | 本人已分配的本组织最近 20 条工单的相同字段 |
| `list_dispatch_candidates` | 管理员 | `category`，三种故障类别之一 | 同组织启用且负责该类别的人员 ID、名称、匹配类别、待接单和处理中数量 |

模型参数不能指定用户、组织或角色。没有创建、审核、派单、改派或结单工具。`tools/list` 只列出固定三项；每次 `tools/call` 重新检查角色、会话、助手启用状态和工具绑定。未知工具、非法参数、伪造身份字段、跨组织访问和已撤销登录均拒绝。

API 用本地 `MCP_CALL_SECRET` 产生最长 60 秒的 HMAC 签名凭据，载荷含当前登录会话哈希、助手 ID、请求 ID、受众和时间。凭据由 API 放入内部请求的 Authorization 与工具调用 `_meta`，不传给模型或浏览器，也不进审计日志。MCP Server 验签后回查 MySQL 中的有效会话与账号。每个请求 ID 最多三次工具调用，每次 Host 和 Server 均设 10 秒上限；不自动重试。审计只记真实操作者、请求 ID、工具名、最小参数、耗时及成功/拒绝/失败。

当前助手对明确的进度/任务/可派人员问题使用保守的服务端意图路由，实际调用 MCP Client，把原样受控字段格式化为会话回答。自然交流与资料问答走自动资料/模型路径；三项技能及模型原生 tool-calling 尚未接入，模型返回 `tool_calls` 仍按协议错误处理。工具失败、超时或无授权时 SSE 返回 `error`，不会编造实时工单数据；人工工单页面独立可用。

## 本地运行与验收

从零运行时 `apitest/setup_local.py` 会在被 Git 忽略的 `.env` 生成独立 `MCP_CALL_SECRET`。旧 `.env` 需自行补上至少 32 字符的随机值，随后运行 `docker compose up -d --build api mcp web`。服务无主机端口映射；可在管理员“助手设置”查看已发现工具。

在项目根目录执行：

```sh
docker run --rm --network campus-repair_default -v "$PWD:/work:ro" -w /work \
  -e API_TEST_URL=http://api:8080 -e MCP_TEST_URL=http://mcp:8081/mcp \
  python:3.12-alpine python apitest/verify_mcp.py
```

脚本使用本地演示账号，给三个角色的助手绑定各自工具，验证协议消息、页面结果一致性、越权/伪造参数/跨组织/过期与登出拒绝、单轮限额、解绑拒绝、审计以及助手 SSE 和消息持久化，最后恢复临时解绑。它会创建一条演示会话，不删除已有数据，不输出密码或令牌。协议错误和慢响应另由 `backend/internal/app/mcp_test.go` 的受控 HTTP 测试覆盖；真实公网和课程网关联调不由这些测试替代。

自然表述支持示例：“我报的维修到哪一步了”“今天派给我的有哪些活”“照明有几位师傅可以安排”。管理员问“我们有几个师傅”时不会调用缺少类别的工具或推断总人数，而是先追问类别；下一轮短句“照明的”结合本人的上一轮问题调用原有角色工具。对话回答不再显示内部 MCP 协议字样或无关人员 ID。角色、组织、会话和绑定校验保持。
