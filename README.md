# 校园报修与智能派单平台

焦阳、高慧莹共同维护的独立学期项目。教师已认可选题；当前完成项目初始化和 MVP 规约，业务代码尚未开发。

## 项目范围

一个校区，照明、电器、给排水三类故障。报修人、维修人员、后勤管理员共同完成提交、审核派单、维修处理、确认关闭的工单闭环。AI 用于信息补全、带出处的知识查询和派单建议，不能自动更改工单。

技术方案：React + TypeScript、Go、MySQL、Qdrant、Nginx、Docker Compose，Bearer 令牌认证。五项 AI 能力为大语言模型、智能体、知识库与 RAG、技能、MCP。

## 规约入口

- [提案](openspec/changes/build-campus-repair-mvp/proposal.md)
- [设计](openspec/changes/build-campus-repair-mvp/design.md)
- [需求与验收场景](openspec/changes/build-campus-repair-mvp/specs/)
- [实施任务](openspec/changes/build-campus-repair-mvp/tasks.md)
- [协作约定](docs/collaboration.md)

当前变更的 delta specs 是首版需求来源；实施验收并归档后再同步为主规约。当前不提供应用启动命令，避免把规划当成已实现系统。

安装 OpenSpec CLI 后可在仓库根目录查看与校验：

```sh
openspec status --change build-campus-repair-mvp
openspec validate build-campus-repair-mvp --strict
```

## 实施顺序

1. 认证授权、三角色页面和人工工单闭环。
2. 知识库、异步索引、智能体与技能、MCP 工具。
3. 自动化测试、审计、备份恢复、公网部署与演示证据。

## 课程依据

- [学期项目说明](https://devops.hello1023.com/项目介绍.html)：完整互联网应用、五项 AI 能力、规约与验收证据、公网交付。
- [分组要求](https://devops.hello1023.com/index.html#requirements)：共同仓库，覆盖规约、开发与验收；两人组须兼任并记录贡献。

本项目的角色、工单状态和技术方案为小组的产品设计，不是课程强制的唯一实现。
