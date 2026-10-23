# 智能校园报修助手交接

更新日期：2026-10-23。本文用于新对话续接；实际代码、Git 状态及 OpenSpec 任务是最终依据。本文不含凭据。


## 2026-10-23 现有功能稳定化（本节优先）

用户确认真实网关额度暂不能恢复，授权先稳定现有功能。本轮没有调用真实chat/embedding网关，没有新技能、提交或推送。仍在codex/conversational-repair，保留全部未提交改动。

- SSE客户端修复CRLF/UTF-8分片与生命周期/结构/ID校验，保留错误code。测试apitest/assistant_stream_checks.mjs通过，Node24需--experimental-transform-types。
- 前端失败/停止恢复原问题，编辑/重问/人工入口，完成后的历史刷新失败不误标生成失败；手机自动定位恢复按钮。真实UI检查发现“停止”切“发送”同一次点击可意外提交，已用不同key/显式button类型/preventDefault修复；单次POST证据tmp/stability-ui/stop-http.json。
- 普通及业务结果最终复核/保存后才发布正文，撤销/配置变更无delta及失败缓冲；MCP参数拒绝null/非字符串/重复键。普通聊天没有新字段不再替代有效草稿。
- 新运行器apitest/verify_stability.py检查实际mock依赖，只运行独立draft-check，测试前内存快照配置与模式、finally恢复；修复embedding故障注入宿主写文件时序不稳。最终六组通过：助手230、草稿157、场景214、失败38、MCP46、人工97；日志tmp/stability。Go/vet、前端构建、presentation/解析器、strict/diff/秘密扫描通过。
- 18083/18086均已更新，三个后端及web镜像一致性检查通过，ready=200、入口no-store；日常七表数量未变，未在日常发测试消息。UI截图tmp/stability-ui，mock模式已恢复ok，用户原标签表单未改。
- OpenSpec **66/81**：18章四项完成；16.4及17.2等真实验收仍未完成，等额度恢复后才能继续，不能以本轮模拟结果替代。


## 2026-10-23 日常部署与额度阻碍（最高优先级）

用户再次截图报告失败。此前“日常已更新”判断有遗漏：只build api/web，MCP/worker仍用Compose自动命名的旧镜像，位置/日期参数被拒绝；绑定本身正常。现compose.yaml为API/worker/MCP指定同一campus-repair-api:latest，三容器与目标镜像ID已实际一致；新增apitest/verify_backend_deployment.py防止再次漏更。

日常MCP地点+recent查询已成功（研发楼近7天实际0条），无业务写入。脚本apitest/probe_live_mcp.py只读现有绑定，签名凭据经stdin给临时内部客户端，不输出秘密；证据tmp/live-mcp-deployment-probe.json。五类业务记录数量未变。

当前仍有外部阻碍：真实模型网关HTTP429且额度不足。原页面已实测显示准确提示，截图tmp/deployment-fix/live-quota.png。已新增网关错误分类和固定阶段脱敏日志；Go/vet及失败专项38项通过，JSON转义中文额度错误也覆盖。不自动重试，不改密钥、不购买额度。

不能声称聊天查询已完全恢复；16.4重开，17.2保持未完成，等网关额度恢复后在用户原页面完成端到端成功验收。17.1/17.3已验证，当前62/77。原75项隔离真实网关证据仍为历史结果，不替代当前日常可用性。全部改动仍未提交/推送。


## 2026-10-23 语义查询修正（最新）

本节优先于下文历史。仍在codex/conversational-repair，未提交/推送。用户指出“研发楼最近有什么故障”没查数据却答无记录；本轮已接原生function calling单次计划，由MCP真实执行并用服务端结果生成业务结论。不是通用Agent循环，不能将三项完整技能标为完成。

- 新增semantic_query.go及测试：完整组装流式工具参数，强制单次固定只读函数或continue_conversation；未知/写工具、身份参数、非法范围拒绝，无自动重试。兼容旧进度/任务绑定，Host/Server授权及10秒/3次/120秒预算保留。模型未配置保留旧关键词降级。
- query_work_orders增加location/keyword/date_from/date_to，period扩展近7天/昨天/本周/上周/本月。字面LOCATE参数绑定，日期含起止日最多366天。返回description，卡片显示位置、现象与实际范围。
- 有效草稿补充上下文和上一实际日期/前移7天范围提供给计划器，不发幂等键。查询/草稿可切换；明确草稿起始仍走原提取器。多类别仍明确澄清。
- 普通模型正文全量缓冲检查危险步骤/常见无证据业务断言，再输出；失败缓冲不落可见历史。phase继续反馈。防护是常见断言检测，不证明所有自然文本正确。meta.tool仍是兼容初步提示，实际工具见查询done.tool及MCP审计。
- 真实专项75项通过，证据tmp/campus-repair-gateway-abb9b57e-check-semantic.json（隔离容器停止、演示卷保留）。失败专项28、助手224断言、草稿152、人工97及Go/vet/前端/presentation/strict/秘密扫描通过。新脚本verify_semantic_gateway.py、verify_semantic_failures.py；UI截图tmp/semantic-query。实际复跑问题与修正见docs/verification.md最新节。
- 日常18083已更新最终镜像，ready=200，七表业务数量与升级前一致；没有在日常发送测试对话。最终多场景214、MCP协议46和失败专项28通过。OpenSpec为61/74，16章四项已验证，其余13项保持未完成。
- mock_chat.py增加原生计划fixture；verify_assistant.py按额外计划请求及全量缓冲契约更新。旧场景fixture同步新日期能力；不把mock作为语义理解证据。


## 2026-10-23 多场景对话最新状态

本节优先于后续历史记录。当前分支仍为 `codex/conversational-repair`；两轮改动均尚未提交/推送，保留全部未提交内容。

- 修复管理员“我现在有多少工单要处理”泛化拒答；三角色可查授权待办/未结束数量、类别/状态/完整编号/今天筛选，并用“那照明的呢”“只看今天”“全部类别”继续追问。数量按完整授权集，列表最多5条，不拿截断列表长度当总量。
- 第四个固定只读MCP工具 `query_work_orders`，仅queue/status/category/number/period参数，Host/Server均核验身份/组织/归属/绑定。迁移6为种子助手一次性绑定并更新配置版本；解绑后重启不恢复，自定义助手手动绑定。旧工具和材料绑定、助手启停不变。
- 今天以北京时间自然日：管理员/报修人按创建时间，维修人员按当前指派最新assign/reassign事件。暂只支持今天或不限日期、单一类别；上周/多个类别等未支持条件明确追问，不默认为全量。
- 结果快照在 `assistant_business_results` 与完成消息同事务保存。历史标识快照、工单卡片重新检查可见性，改派后移除原人员无权链接。普通话题后不继承旧筛选，失败不发布新上下文。
- 管理员人员建议用真实候选和两种负载排序，缺类别追问，可连续换类别；直接派单/改状态仍只给人工确认入口，不写业务。
- 前端按角色给欢迎语/示例/输入提示，显示实际助手名和phase阶段；卡片可打开真实详情、快捷追问仅填输入框；失败可主动重问并保留具体错误。查询完成先展示结论与总量，避免长卡片自动滚到底部隐藏结论。
- 原有对话式报修仍工作，草稿保护、幂等及确认提交未被新路由抢占。
- 最终验证：多场景213、助手224断言、草稿152、人工97、MCP协议46、完整真实网关102通过；Go/vet、前端构建、strict/格式/秘密扫描及三角色/390px手机实际浏览器检查通过。详见 `docs/verification.md` 最后章节。
- 日常18083已更新，ready=200；七组业务数量与升级前一致。未在日常环境提问造测试记录。隔离仍用18086及 `tmp/draft-check.env`/`tmp/draft-mock`，不要输出凭据。
- 新增核心文件：`backend/internal/app/business_query.go`、`business_query_test.go`、`frontend/src/BusinessResultCard.tsx`、`apitest/verify_scenarios.py`。真实证据 `tmp/campus-repair-gateway-29f17578-check.json`，界面证据 `tmp/assistant-scenarios/`。
- OpenSpec现为 **57/70**，15章五项已实现并验证。8.1–8.3及7.5仍未完整完成，`skill=none`，没有原生tool_calls或通用Agent循环。下一步仍是三项技能配置/受控路由、安全/派单辅助的完整验收，以及综合审计、恢复与公网交付；不要将本轮基础场景优化当作全部MVP已完成。

## 2026-10-23 上一轮对话式报修状态

本节优先于下文2026-10-22交接记录。用户已授权并完成“AI对话式报修”。当前分支 `codex/conversational-repair`，从干净main新建；本轮改动尚未提交或推送，未操作PR。不要覆盖未提交改动。

- 规约与AGENTS已明确：AI整理草稿，MCP保持只读；只有报修人主动确认后由正常业务接口创建。
- 报修人说“宿舍灯坏了，帮我报修”，服务端通过真实模型提取三字段并追问缺项；对话内提供可编辑卡片，可保存、取消、刷新后重开历史恢复。严格JSON/字段来源校验，泛称宿舍不能作为具体位置；失败或停止不发布新卡片。
- 私有 `repair_drafts` 表关联完整助手消息，新增GET/PUT `/api/conversations/{id}/drafts/{message}`。新卡片替代旧active卡片；编辑校验version，同内容保留键，实际修改才换键。
- `POST /api/work-orders` 保持人工接口兼容，卡片确认增加 `draft_message_id`/`draft_version` 和草稿返回的幂等键。复用创建事务并原子关联草稿提交结果，重试返回原工单；过期幂等记录也不使已提交草稿重复建单。
- 对话展示真实编号及详情入口；保存和输入框Enter不建单，只有激活确认按钮才创建。失败保留输入并核对服务端结果，离开/切换/刷新保护未保存编辑。
- 核心新增：`backend/internal/app/drafts.go`、`drafts_test.go`、`frontend/src/RepairDraftCard.tsx`、`apitest/verify_drafts.py`、`compose.draft-check.yaml`。
- 最终验证：草稿专项152项、原助手226断言、人工97项、真实网关完整78项、Go测试/vet、前端构建、OpenSpec strict及格式/秘密扫描通过；桌面/390px手机实际验证，详情字段真实一致。
- 日常18083已更新到本轮版本，ready=200；更新前后账号/工单/事件/维修记录/材料/会话/消息数量不变。新增隔离环境 `campus-repair-draft-check` 端口18086，随机凭据在忽略文件 `tmp/draft-check.env`；不要输出。模拟状态独立在 `tmp/draft-mock`。
- 证据见 `docs/verification.md` 最新2026-10-23章节，最终真实网关证据 `tmp/campus-repair-gateway-33517c9b-check.json`，截图 `tmp/conversational-repair/`。
- OpenSpec为 **52/65**：新增14章五项及8.6已验证勾选。8.1–8.3仍未完成，因为三项可配置技能、完整安全/派单辅助未做；`skill=none`仍适用于基础对话与本次草稿，不声称原生tool_calls已经支持。

下一步建议补齐三项技能配置/角色白名单路由及安全、派单辅助，同时完成7.5工具限额/10秒超时/协议异常验收；之后才是9章综合审计统计和10章最终恢复、公网部署。每项先核对规约及已有实现，不自动归档整个MVP。

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

**2026-10-22时仅完成方案讨论；2026-10-23已完成规约、实现及隔离验收，以本文顶部最新状态为准。**

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

以下为2026-10-22的历史快照；当前以顶部52/65为准。OpenSpec 变更：`build-campus-repair-mvp`，当时复选框 **46/60**。这是任务计数，不代表完成质量或工作量百分比。

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

## 5. 对话式报修的历史实施建议（已按顶部状态实施）

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

## 9. 历史开场指令（本项现已完成，不要重复实施）

> 请先阅读 `/Users/jiaoyang/projects/campus-repair/handoff.md` 和该项目的 AGENTS.md，检查当前未提交改动及 OpenSpec。接下来实现“AI 对话式报修”：对话补齐信息，展示可编辑报修卡片，用户确认后复用现有 API 创建工单，返回真实编号与详情入口。先更新规约，再实施及隔离验收；保留现有视觉与自然对话成果，不让 MCP 写入，不改动日常数据，不自动合并 PR。
