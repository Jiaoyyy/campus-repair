# 第一阶段接口

接口已实现登录与人工工单。知识库、智能体、MCP 尚未实现；GET /api/capabilities 明确返回 false。

## 通用规则

除 POST /api/login 和 /health/* 外均须 Authorization: Bearer 令牌。角色和组织取服务端账号，客户端传入身份字段不生效。成功响应为 JSON；失败为 error.code、message、request_id，并返回 X-Request-ID。无 Cookie 登录，令牌 12 小时有效，登出即撤销。

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
