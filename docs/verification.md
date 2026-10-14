# 第一阶段验收

日期：2026-10-14（Asia/Shanghai）。范围：运行基础、登录授权、人工工单，任务 1.1 至 4.5。AI、知识库、MCP、公网与最终备份恢复尚未验收，不代表整个 MVP 已完成。

## 实际结果

| 验证 | 结果 |
| --- | --- |
| 前端 npm run build，TypeScript + Vite | 通过 |
| npm 安装依赖审计 | 0 个已报告漏洞 |
| Go go test ./...、go vet ./... | 通过；含 15 条合法转换、越权/组织/版本/终态和输入校验 |
| API 集成 verify_manual.py | 97 项检查通过 |
| 创建重试/同键不同内容/五请求并发创建 | 同一工单、内容冲突 409、无重复工单 |
| 两次管理员并发派单 | 一个成功，一个 409；无重复有效指派 |
| 补充重审、改派、接单、记录、完成、重处理、关闭、取消与驳回 | 通过，历史保留 |
| 匿名、伪造令牌、Cookie 单独请求、同组织他人、跨组织、原人员改派后访问 | 按规约拒绝 |
| 到期、账号禁用、非法角色、登出重放与五次失败限流 | 按规约 401/429 |
| 审计插入故障（隔离库临时触发器） | 503，工单版本/状态/事件未变化，事务回滚 |
| 浏览器三角色流程、刷新恢复、登录失效、确认关闭、退出 | 通过，浏览器无未处理异常 |
| 桌面 1440px、手机 390px | 页面检查通过，手机无整页横向溢出 |
| 数据库暂停 | live=200，ready=503；恢复数据库后就绪恢复 |
| 普通停止与重建 | 账号、工单、版本、事件、处理、审计保持一致，种子未复制 |
| 配置秘密扫描 | 本地密码/会话密钥未出现在前端产物和 API/Web 日志 |

浏览器截图来自虚构测试工单，不包含密码或令牌。工作目录 tmp/ui 中保存桌面、手机及登录截图，不纳入个人贡献记录。

## 可复现隔离环境

先按 README 构建日常项目镜像。为测试创建独立 `.env` 配置，使用独立随机密码和端口 18084，保存为 `tmp/check.env`；不要把日常配置复制后用于故障测试。

```sh
docker compose -p campus-repair-check --env-file tmp/check.env -f compose.yaml -f compose.verify.yaml up -d --no-build
python3 apitest/verify_manual.py --env-file tmp/check.env --project campus-repair-check
```

测试的 SQL 故障/到期/禁用操作仅允许在名称以 `-check` 结尾的指定环境执行；schema 与卷独立。`verify_manual.py` 结束会触发登录限流；浏览器测试前可在隔离环境重启 API 清空单实例限流：

```sh
docker compose -p campus-repair-check --env-file tmp/check.env -f compose.yaml -f compose.verify.yaml restart api
```

浏览器测试需要可用的 Playwright 模块及 Google Chrome：

```sh
PLAYWRIGHT_MODULE=/你的/playwright/模块路径 node apitest/verify_ui.cjs
python3 apitest/verify_runtime.py
```

UI 脚本默认读 tmp/check.env，TEST_ENV 可指定测试配置；截图和脱敏前错误详情只写入被忽略的 tmp，终端不打印密码/令牌。运行验证脚本会停止和重建 **campus-repair-check** 环境，但不会删除卷或触碰日常 campus-repair。

验收后可 `docker compose -p campus-repair-check --env-file tmp/check.env -f compose.yaml -f compose.verify.yaml down` 停止隔离容器；保留卷以便复查。

## 边界与后续

当前仅本机预览 http://127.0.0.1:18083，不是公网交付。Qdrant 已纳入独立运行配置但尚未写入业务向量；worker/MCP 仅编译入口，运行会明确退出，不假装实现协议。后续从任务 5.1 开始实施知识库与异步索引。

## 视觉与内容逻辑优化验收

2026-10-14 完成任务 11.1 至 11.6，业务主流程与权限边界保持原约束。

- 前端构建、Go 测试和静态检查通过；新增角色队列规则测试通过。
- `verify_views.py` 的 57 项检查通过：角色待办、超过 20 条时仍准确的全量概览、队列筛选、非法队列、组织隔离及审计名称/编号。
- 原有 `verify_manual.py` 的 97 项接口回归仍通过。
- `presentation_checks.mjs` 通过：等待工单没有取消主动作，维修处理中默认提交结果，终态无主动作，重处理进度、审计名称和成功反馈准确。
- 在应用内浏览器检查报修人列表：待审核说明等待管理员；位置搜索进入详情再返回仍保留；无待办与无筛选匹配分别提示。
- 隔离环境经浏览器执行管理员审核/派单、维修人员接单/完成、报修人确认关闭，验证主要/次要操作、维修结果与终态只读详情。
- 主动暂停隔离 API，确认列表显示读取失败、数量为“—”和重试按钮，不冒充零工单；恢复后重试正常。
- 1280px 桌面与 390px 手机布局检查：手机使用工单卡片；结果、报修信息、操作区、历史依次排列，无整页横向溢出；测试后恢复浏览器默认尺寸。

本次验收截图保存在被忽略的 `tmp/ui-final`；未发布日常工单内容到 GitHub，未填写个人贡献记录。浏览器操作仅改动隔离测试工单，日常工单未改变。
