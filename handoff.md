# 智能校园报修助手交接

更新日期：2026-10-22。本文用于新对话续接；实际代码、Git 状态及 OpenSpec 任务是最终依据。本文不含凭据。

## 1. 先定位正确项目

- **实际项目目录：`/Users/jiaoyang/projects/campus-repair`**。
- 对话环境可能默认位于 `/Users/jiaoyang/projects/campusclaw-mygroup`，执行命令时必须显式切到实际项目。不要修改 CampusClaw 的代码、数据或凭据。
- 公开仓库：https://github.com/Jiaoyyy/campus-repair
- 当前分支：`codex/readonly-mcp`；本轮上传前基线：`4695f0f feat: add read-only MCP tools for repair assistants`。最新提交请用 `git log -1` 查询。
- 2026-10-22 用户要求将现有成果提交并推送至当前分支，范围包含真实网关验证、整站视觉统一、自然对话修复及本文。接手先检查 `git status` 与远端同步状态；不要重置或覆盖后续本地改动。
- 草稿 PR #1：https://github.com/Jiaoyyy/campus-repair/pull/1 （人工闭环分支 → main）。
- 草稿 PR #2：https://github.com/Jiaoyyy/campus-repair/pull/2 （当前分支 → 人工闭环分支）。两者此前未合并；后续先查询实际状态，不自动合并。

接手先读：`AGENTS.md`、`README.md`、本文，以及 `openspec/changes/build-campus-repair-mvp/` 下的 proposal、design、tasks 和相关 specs。

## 2. 用户最新意图与已达成的产品约定

教师已认可选题。用户希望助手能实际帮助办理报修，而不只是问答；不接受聊天界面要求选择关键词、向量或混合检索。

最新讨论的下一项功能是 **AI 对话式报修**，用户认可以下方式：

1. 用户说“宿舍灯坏了，帮我报修”。
2. AI 结合上下文补问缺失信息，整理类别、位置、故障描述。
3. 在对话中展示可编辑的结构化报修卡片。
4. 用户点击“确认提交报修”，调用现有业务接口创建工单。
5. 对话中显示真实工单编号和查看入口，无需先跳转到另一张表单。

这属于“对话式业务办理”，**不必实现成 MCP**。MCP 是工具连接协议；当前计划可复用现有创建工单 API，AI 负责组织信息，用户确认后由正常业务链路提交。现有 MCP 继续只读；不为了这个功能增加写 MCP 工具。

**当前仅完成方案讨论，尚未实现此功能，也尚未为此修改规约。** 本轮用户要求是编写交接文件；新对话应把对话式报修作为优先接续项，并根据用户后续指令开始实施。

用户偏好：中文、简洁、实用，明确授权后直接推进，避免反复确认；视觉和操作逻辑要一致。贡献记录目前留空，不虚构个人贡献。不要擅自邀请组员、发送外部消息或创建额外聊天。当前不启用子代理，除非用户或适用项目指令明确要求。

## 3. 已完成的工作

### 人工业务与基础设施

- React / TypeScript / Vite 前端，Go API / worker / MCP，MySQL、Qdrant、Nginx，独立 Docker Compose 和持久卷。
- 三角色登录、服务端组织/角色/归属校验、会话撤销、登录限流。
- 人工工单完整闭环：创建、审核、补充重提、派单/改派、接单、维修记录、提交完成、本人确认、重新处理、取消、驳回。
- 创建幂等、更新版本冲突、事务审计、角色列表/筛选/概览/待办、工单详情时间线。

### 知识库、助手和 MCP

- 管理员材料上传、原文、异步索引、任务租约/恢复/重试、Unicode 切片、嵌入与 Qdrant 版本管理。
- 关键词、向量、RRF 混合检索；组织、助手绑定、发布版本过滤；引用核验与原文范围定位。
- 三角色助手、管理员提示词/启停/材料和只读工具绑定、私有会话历史、流式输出、主动停止、生成锁与崩溃恢复。
- 内部真实 MCP Client/Server，固定三种只读查询：报修人查本人工单、维修人员查已指派任务、管理员按类别查可派人员。服务端身份复核，不接受模型自报身份。
- **尚无原生模型 tool_calls 循环**；目前是服务端确定性意图路由。模型返回原生 tool_calls 仍视为协议错误，不能描述成已支持通用 Agent 工具调用。

### 本对话近期重点：真实网关联调

- 用户已配置本项目 `.env`。旧运行容器最初未加载新配置，已重建 API/worker/MCP。
- 真实对话接口 HTTP 200，SSE 正常结束；真实嵌入接口返回 **2048 维**有效向量，与本地配置一致。
- 新增 `apitest/verify_gateway.py`：只在内存读取本项目网关配置，创建随机名称、独立密码和数据卷的 `campus-repair-gateway-*-check` 环境。
- 实际验证上传/索引、三路检索、三角色带引用回答、原文范围、只读 MCP、审计与秘密扫描。首次完整结果 46 项，扩展自然对话后最终 66 项通过。

### 本对话近期重点：整站视觉统一

- 统一颜色、字体、间距、控件、图标、页头、加载/空态、状态反馈。
- 浅色侧栏配深绿色强调，工作/管理入口分组，当前位置和手机导航一致。
- 工单列表、详情、表单、审计页面统一；手机工单卡片和操作层次整理。
- 助手历史紧凑、消息区域独立滚动、输入区置底，手机历史按需展开。
- 新增 `frontend/src/ui.tsx`：共享 Icon、PageHeading、Loading、Empty、Drawer、OriginalText。
- 原文/引用/上传使用统一侧栏；包含焦点管理、Tab 循环、Escape 关闭、关闭后恢复触发点焦点、原文高亮定位。
- 材料列表紧凑，检索按需展开，索引详情/重建收进低频操作。
- 助手设置分组、材料搜索、固定保存区；离开/切换/刷新/退出时保护未保存编辑，冲突后保留输入。
- 桌面及 390px 手机实际检查通过，无整页横向溢出。

### 本对话最近完成：让助手正常对话

- **所有角色的聊天界面均移除检索方式及“高级选项”**；前端发消息仅传 `{content}`。知识库管理的显式检索测试保留。
- 新增 `backend/internal/app/assistant_context.go`：后台自动决定资料策略，问候/感谢/能力介绍跳过检索，报修追问结合近期已完成用户消息。
- 没绑定资料时不依赖外部向量服务；无命中仍调用真实模型自然交流和追问，不再固定回复“没有证据”。
- 向量故障回退关键词并显示提醒；全部检索故障仍允许对话，明确资料不可用。
- 校方制度、维修时限等事实须有证据，不能编造；一般故障追问不强行显示无资料警告。
- 支持“我报的维修到哪一步了”“今天派给我的有哪些活”“照明有几位师傅”等自然查询；管理员缺少类别时追问，后续“照明的”可衔接真实工具查询。
- 旧客户端 `mode` 参数仍可被后端接收/校验，但不决定检索策略。
- 当前 `skill=none`；**没有创建报修卡片、自动建单或自动派单**。
- 最新 API/worker/MCP/web 已更新到日常预览，保留原有业务数据。

## 4. 进度与未完成项

OpenSpec 变更：`build-campus-repair-mvp`，当前复选框 **46/60**。这是任务计数，不代表完成质量或工作量百分比。

- 已完成：1–6 章、7.1–7.4、8.4–8.5、11–13 章。
- 7.5：完整工具限额/10 秒超时/协议异常验收仍未完成；已有部分限额、审计和协议测试，不能整项勾选。
- 8.1：已有助手、绑定、会话及配置页面，但完整技能配置未完成。
- 8.2：三项白名单技能及配置化路由未完成。
- 8.3：带确认报修草稿、安全建议和真实人员派单建议未完整实现。
- 8.6：已有助手选择、历史、流式和出处，**报修草稿与业务提交衔接未实现**。
- 9.1–9.3：已有部分业务/知识/工具审计与页面，综合审计、统计和完整证据整理尚未逐条验收。
- 10.1–10.6：完整三技能集成、最终持久化验收、备份/隔离恢复/回滚演练、公网主机/域名/HTTPS、交付脚本与最终规约归档均未完成。
- 10.2：真实模型、嵌入、RAG 和只读 MCP 已验收，但三项技能未完成，不能勾选整项。

注意任务 12.2 描述中仍保留上一轮“管理员保留高级选项”的历史文字；最新行为被第 13 章覆盖，所有角色聊天都不再选择检索算法。`docs/verification.md` 是分阶段记录，早期“网关未配置”等陈述也应结合最后日期章节阅读。

## 5. 下一步对话式报修的实施入口

先按 OpenSpec 流程同步 proposal、specs、design、tasks，再写代码。现行 `AGENTS.md` 的“AI/MCP 只读”应明确与“用户确认后正常业务接口写入”的边界，保持各文档一致；不能让模型直接绕过确认提交。

建议本轮只聚焦对话式报修，不顺手宣称三项技能全部完成：

1. 明确结构化草稿契约、缺失字段追问、草稿展示/编辑、取消、提交与失败恢复行为。
2. 仅报修人能够确认创建；草稿数据服务端验证，身份/组织来自当前登录会话，不信任模型输入。
3. 聊天过程中不创建工单；确认时复用正常创建链路和幂等机制。
4. 同一确认内容重试保持同一幂等键；修改内容后换键，防止双击、网络重试重复创建。
5. 成功返回真实编号/详情入口；失败保留草稿并明确显示原因，不能把模型文字当成功结果。
6. 验证未确认不写库、其他角色拒绝、字段校验、上下文补全、编辑后提交、并发/重复点击、失败重试及成功后的真实详情。

可复用接口与文件：

- `POST /api/work-orders`，仅 requester；body 为 `{category, location, description}`。
- category：`lighting` / `electrical` / `plumbing`；位置必填、最多 200 Unicode 字符；描述必填、最多 2000 字符。
- `Idempotency-Key`：用户范围、24 小时；同键同内容重放，同键内容变化 409；首次创建 201，重放 200，返回 `{order: ...}`。
- `frontend/src/main.tsx` 中 `CreateOrder` 已有提交和幂等键样例。
- `frontend/src/api.ts` 有 Order 类型、鉴权 API 辅助函数。
- 页面使用 App 的 `view` / `selected` 状态切换，未使用独立路由库。
- `frontend/src/Assistant.tsx` 当前仅接收 `onManual`、`role`，尚无草稿卡片及建单回调。
- `frontend/src/assistant-api.ts` 定义流事件与发送接口。
- 后端重点：`chat.go`、`assistant_context.go`、`mcp_assistant.go`；查找现有创建工单 handler 后复用业务校验。
- 原有三种 MCP 工具保持只读，不增加写工具也能实现本功能。

## 6. 环境与操作边界

- 日常预览：http://127.0.0.1:18083/ ，Compose 项目 `campus-repair`。
- 隔离预览：http://127.0.0.1:18084/ ，项目 `campus-repair-check`。
- 真实网关脚本默认端口 18085，使用随机 `*-check` 项目。结束会停止容器，保留演示卷。
- Docker Desktop 可用；主机无 Go 时用 `golang:1.24-alpine`，Node/npm 可用。
- `.env` 已有真实课程网关密钥，只允许后端使用。**不要输出 `.env` 或完整 Compose 展开配置，不把凭据写到命令行、前端、文档、日志和提交中。**
- 修改 `.env` 后需要重建对应容器环境，单纯 restart 不会读取新值。
- 不执行 `docker compose down -v`；不删除日常数据卷。故障/停库/账号篡改/SQL 注入测试仅在隔离 `*-check` 项目。
- 真实网关只发送虚构演示材料和问题。
- 浏览器实际 UI 操作用 `mcp__cua_repl`；不要用 shell Playwright 替代。新对话重新获取浏览器状态，不复用旧 JS 绑定。

## 7. 已有验证及证据

这些是此前已完成工作的结果，本轮仅交接整理，没有重新运行全部测试。详细范围以 `docs/verification.md` 为准。

| 验证 | 最近记录 |
| --- | --- |
| 前端 TypeScript/Vite build | 通过 |
| Go 测试与 go vet | 通过 |
| OpenSpec strict / git diff --check | 通过 |
| 人工 API 回归 | 97 项通过 |
| 自然对话及助手隔离回归 | 226 条断言通过，含轮询，数量会随处理速度变化 |
| 真实网关扩展集成 | 66 项通过 |
| 桌面/390px 手机、来源侧栏与设置保护 | 实际浏览器检查通过 |

证据在被忽略的 `tmp/`，不会随 Git 克隆到新机器：

- `tmp/campus-repair-gateway-34452d49-check.json`：最新真实网关回答、引用、请求标识及检查结果。
- `tmp/campus-repair-gateway-595a4da6-check.json`：较早 46 项真实联调证据。
- `tmp/visual-unification/`：整站桌面/手机、来源侧栏、设置等截图。
- `tmp/natural-assistant/admin-conversation.png`：自然对话界面截图，来自**模拟**隔离模型；真实模型证据以 JSON 为准。

常用命令（在实际项目目录运行）：

```sh
git status --short
git branch --show-current
docker compose ps
curl -fsS http://127.0.0.1:18083/health/ready
openspec status --change build-campus-repair-mvp
openspec validate build-campus-repair-mvp --strict
git diff --check
```

构建与单元验证：

```sh
npm --prefix frontend run build
docker run --rm -v "$PWD/backend:/src" -w /src golang:1.24-alpine sh -c 'go test ./... && go vet ./...'
node apitest/presentation_checks.mjs
```

集成验证需先读对应脚本、确认隔离配置及模拟服务已就绪：

```sh
python3 apitest/verify_manual.py --env-file tmp/check.env --project campus-repair-check
python3 apitest/verify_assistant.py
```

真实网关验收会调用实际模型并创建新的独立测试资源，镜像需先更新：

```sh
docker compose build api web
python3 apitest/verify_gateway.py
```

现有隔离辅助配置包括 `compose.verify.yaml`、`compose.knowledge-check.yaml`，以及被忽略的 `tmp/check.env`、`tmp/ui-check.compose.yaml`。不要用日常配置直接替换隔离密码；重新运行前检查脚本的实际参数和环境要求。

## 8. 本轮上传范围

本轮成果涉及 README、docs、前端主页面/样式/助手/材料/配置、后端聊天及 MCP 意图处理、测试脚本、当前 OpenSpec artifacts。

本轮包含以下新增实现文件：

- `frontend/src/ui.tsx`
- `backend/internal/app/assistant_context.go`
- `apitest/verify_gateway.py`

本文 `handoff.md` 一并上传。上传前已检查 30 个改动文件，未发现本地配置中的秘密值；OpenSpec 严格验证与差异格式检查通过。完整测试沿用第 7 节已记录结果，本轮上传未重跑全部集成测试。

## 9. 新对话可直接使用的开场指令

> 请先阅读 `/Users/jiaoyang/projects/campus-repair/handoff.md` 和该项目的 AGENTS.md，检查当前未提交改动及 OpenSpec。接下来实现“AI 对话式报修”：对话补齐信息，展示可编辑报修卡片，用户确认后复用现有 API 创建工单，返回真实编号与详情入口。先更新规约，再实施及隔离验收；保留现有视觉与自然对话成果，不让 MCP 写入，不改动日常数据，不自动合并 PR。
