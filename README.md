# 校园报修与智能派单平台

焦阳、高慧莹小组的独立学期项目。教师已认可选题，当前完成三角色登录、人工报修闭环、知识库检索、角色报修助手及内部只读 MCP 查询。真实网关的嵌入与带引用问答已通过隔离联调；三项技能、模型原生工具调用及公网部署仍待实施。

## 已有功能

- 报修人：提交、查看本人进度、补充信息、接单前取消、确认完成或申请重新处理。
- 管理员：审核、要求补充、驳回、匹配人员派单、待接单改派、查看业务审计。
- 维修人员：查看本人任务、接单、记录处理措施、提交维修结果。
- 服务端按组织、角色与工单归属授权；Bearer 登录、到期/登出撤销、登录失败限流。
- 24 小时创建幂等键、版本冲突、状态规则和业务/历史/审计事务；数据保存到 MySQL。
- 角色待办队列与全量概览、详情状态/责任/最新结果、次要操作按需展开、中文审计名称及手机工单卡片。

- 知识库：管理员上传 UTF-8 文本，持久任务租约恢复、800/80 字符切片、关键词/向量/RRF 检索、失败重试与原文定位。真实嵌入、Qdrant 索引及三路检索已使用演示材料验证。

- 角色报修助手：绑定范围检索、实际引用/原文、私有历史、停止生成和崩溃锁恢复；可自然交流、追问故障信息并自动查询资料；普通对话需服务端 CHAT_* 参数。
- 内部 MCP：管理员发现并绑定三个只读工具，报修进度、已分配任务、可派人员通过真实协议查询；明确的工具问题即使未配置模型也可使用。详见 [MCP 说明](docs/mcp.md)。
- 整站界面：统一导航、文字和控件；手机展开式菜单，助手连续对话与来源侧栏，紧凑材料列表及分组配置、未保存提醒。

首版一个校区，照明、电器、给排水三类故障。不做付款、库存采购、SSO、多校区运营、原生 App 或 AI 自动派单。

## 从零运行

安装 Docker 与 Docker Compose，在仓库根目录执行：

```sh
python3 apitest/setup_local.py
docker compose up --build -d
```

脚本只在 `.env` 不存在时生成独立随机密码、会话密钥与 MCP 调用密钥，文件权限为 600，不打印或覆盖凭据。也可复制 `.env.example` 并自行填写空变量；账号密码须为 12 至 72 字节，SESSION_SECRET 与 MCP_CALL_SECRET 均至少 32 字符。密码和密钥不得提交 Git。

打开 **http://127.0.0.1:18083**。首次初始化 MySQL 和账号可能需要几十秒；就绪检查为 http://127.0.0.1:18083/health/ready 。端口通过 `.env` 的 WEB_PORT 配置，当前仅绑定本机，公网部署在后续阶段进行。

| 账号 | 角色 | 本地密码变量 |
| --- | --- | --- |
| admin | 后勤管理员 | SEED_ADMIN_PASSWORD |
| requester | 报修人一 | SEED_REQUESTER_PASSWORD |
| requester2 | 报修人二 | SEED_REQUESTER2_PASSWORD |
| tech | 维修人员一 | SEED_TECH_PASSWORD |
| tech2 | 维修人员二 | SEED_TECH2_PASSWORD |
| other | 隔离组织报修人 | SEED_OTHER_PASSWORD |
| other_admin | 隔离组织管理员 | SEED_OTHER_PASSWORD |

在自己的 `.env` 查看密码，不在聊天或截图中展示。初始化是幂等的：重新配置种子密码不会覆盖已有账号密码，也不会复制或删除工单。两个组织只用于隔离测试。

Docker 数据卷与 CampusClaw 完全分离。普通 `docker compose down` 和重建保留数据库；不要使用 `down -v`，它会删除数据卷。API、MySQL、Qdrant 不直接开放主机端口。

## 开发与验证

前端：

```sh
cd frontend
npm ci
npm run dev
```

开发代理固定到本机 18083；修改服务端口时同步调整 vite.config.ts。前端构建为 `npm run build`。后台 Go 模块可以用已安装 Go 运行 `go test ./...`；主机没有 Go 时可用容器：

```sh
docker run --rm -v "$PWD/backend:/src" -w /src golang:1.24-alpine sh -c 'go test ./... && go vet ./...'
```

运行 API 成功/失败路径测试：

```sh
python3 apitest/verify_manual.py
```

此命令会创建演示测试工单，并在最后测试登录限流；一分钟后恢复登录。审计故障、账号禁用、数据库暂停和恢复测试只能在独立 `*-check` 环境运行，不允许对日常环境执行。具体隔离方式、实际结果和浏览器验收见 [验收记录](docs/verification.md)。

## 规约与文档

- [提案](openspec/changes/build-campus-repair-mvp/proposal.md)
- [设计与状态转换](openspec/changes/build-campus-repair-mvp/design.md)
- [需求及验收场景](openspec/changes/build-campus-repair-mvp/specs/)
- [实施任务](openspec/changes/build-campus-repair-mvp/tasks.md)
- [已实现接口](docs/api.md)
- [协作约定](docs/collaboration.md)

```sh
openspec status --change build-campus-repair-mvp
openspec validate build-campus-repair-mvp --strict
```

当前变更中的 delta specs 是需求来源；全部实施验收后再同步主规约和归档。worker 与 MCP Server 已作为独立服务运行。GET /api/capabilities 区分知识管理、向量、对话模型及内部 MCP 服务状态。详见 [知识库运行与恢复](docs/knowledge.md)、[助手配置与引用](docs/assistant.md) 和 [MCP 协议与验收](docs/mcp.md)。

## 后续实施

三项技能及草稿表单衔接、模型原生工具调用 → 综合审计/统计 → 完整 AI 能力验收、备份恢复及公网验收。

## 课程依据

- [学期项目说明](https://devops.hello1023.com/项目介绍.html)：完整互联网应用、五项 AI 能力、规约与验收证据、公网交付。
- [分组要求](https://devops.hello1023.com/index.html#requirements)：共同仓库，覆盖规约、开发和验收。

技术方案与工单规则由小组设计，不是课程强制的唯一实现。
