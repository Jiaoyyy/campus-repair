# 知识库运行与恢复

管理员在“知识库”上传、检索和核对本组织材料。支持 UTF-8 `.txt` / `.md`，单文件最多 10 MiB；标题必填，并明确选择是否为演示资料。原文按上传字节保存，下载需要 Bearer，不提供公共文件地址。报修人和维修人员通过角色助手访问已绑定材料，不能绕过绑定直接使用管理检索。

## 索引状态

| 状态 | 含义与处理 |
| --- | --- |
| 等待索引 | 材料、正文与 MySQL 任务已保存，等待 worker |
| 正在索引 | worker 已领取任务；每 5 秒续租，租约为 30 秒 |
| 索引完成 | 本次关键词与向量都已成功，并原子发布 generation |
| 索引失败 | 原文件仍可查看、下载；检查服务配置后点“重试索引” |

首次嵌入失败时可以发布关键词版本，页面明确显示失败原因和“关键词可用”。失败重建不会取代旧发布版本；因此“索引失败”和“已有可用版本”可能同时出现。任务排队/处理中重建返回 409。

切片保留原字符，包括空白、换行和 BOM；每片最多 800 个 Unicode 字符，相邻片重叠 80。接口范围使用从 0 开始、左闭右开的 `[start,end)`；页面换算为从 1 开始的字符编号。emoji 按 Unicode 码点计，不按 UTF-8 字节或 JavaScript UTF-16 长度计。

## 嵌入配置

只在本项目 `.env` 设置服务端参数，不复制其他项目凭据：

- `EMBEDDING_BASE_URL`：兼容网关前缀，例如以 `/v1` 结尾，服务端追加 `/embeddings`。
- `EMBEDDING_API_KEY`、`EMBEDDING_MODEL`、`EMBEDDING_DIMENSION`：由部署者提供；维度必须与模型输出一致。
- `EMBEDDING_COLLECTION`：集合修订名，默认 `repair_v1`。

参数不齐时人工业务和关键词检索仍能运行，向量/混合检索明确返回 503。完成配置后执行 `docker compose up -d api worker`，再在材料页面重试。尚未配置本项目真实网关；模拟测试不能证明实际模型质量或网关可用。

集合名包含模型与维度指纹，不混用向量空间。变更模型/维度或修订名后必须显式重建每份材料；未重建时向量检索返回不可用，关键词仍可用。旧发布版本在新版本成功后切换。旧向量与失败构建的向量通过 MySQL `vector_cleanup_jobs` 持久任务删除，依赖恢复后重试；Qdrant payload 只有组织、材料与 generation，正文始终从 MySQL 按权限回读。

## 检索与原文

关键词使用 MySQL ngram FULLTEXT；向量调用网关与 Qdrant；混合方式对授权且仍为当前版本的两路结果按 `1/(60+rank)` 做 RRF，最多返回四个片段。空数组表示没有命中；503 表示依赖或索引不可用，不能解释成没有资料。

管理检索面向本组织全部已发布材料。带 `agent_id` 的检索复核组织、角色、启用和材料绑定；不带该参数仍仅供管理员管理。模型实际引用和原文访问另外绑定本人会话，详见 [助手说明](assistant.md)。页面“定位原文”复核材料 generation 和片段字符范围，版本已变化则提示重新检索。检索片段不是 AI 已使用的引用。

## 任务恢复

正常运行 `docker compose up -d worker`。进程被强杀后，任务保留 processing，租约最长 30 秒失效后自动重领；同一 generation 重用切片 ID 和向量点 ID。只有持有有效租约的 worker 能发布，崩溃重领不会重复产生有效发布。

若持续失败，先检查 `docker compose ps` 和 `docker compose logs --tail=50 worker`，再检查数据库、网关与 Qdrant。运行日志不输出原文或模型响应。恢复依赖后由管理员点重试；不要删除原文、修改已发布 generation 或删除数据卷。大材料分批提交切片，避免长事务阻断续租。

文件写入成功后才提交材料/任务/审计事务；数据库失败会清理文件。进程恰在文件落盘与事务提交之间退出可能留下无引用文件，不能把它当成已上传材料。备份时仍须暂停写入并同一时点保存数据库与 uploads 卷；完整备份恢复演练属于后续交付任务。

## 隔离验证

先构建镜像并按验收文档创建独立 `tmp/check.env`，然后执行：

```sh
mkdir -p tmp/knowledge-mock
printf 'ok' > tmp/knowledge-mock/mode
docker compose -p campus-repair-check --env-file tmp/check.env -f compose.yaml -f compose.verify.yaml -f compose.knowledge-check.yaml up -d --no-build
python3 apitest/verify_knowledge.py
```

脚本只允许 `*-check` 环境，使用模拟嵌入和真实 MySQL/Qdrant；会强杀/重建隔离 worker、暂停隔离 Qdrant、创建边界大小材料，保留测试卷。故障脚本运行期间不要在同一环境做手工验收或切换 mock 状态。
