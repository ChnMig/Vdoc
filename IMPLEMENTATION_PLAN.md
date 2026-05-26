# Vdoc v0.1 工程实现计划

本文档把主 PRD 的 v0.1 MVP 拆成 Go/Gin 后端工程计划。主需求来源见 `PRD.md`。当前工程实现以 Project 下的多类型 Document 为准，不再以项目下的后端服务作为产品模型。

## 1. 范围

v0.1 必须跑通：

```text
用户登录
  -> 系统超级管理员 / 成员
  -> Team / Project / Role
  -> Project Document
  -> Document Branch / Environment
  -> OpenAPI 或 Markdown 上传并创建草稿
  -> 人工审核发布 Document Version
  -> OpenAPI Endpoint Index / Detail
  -> OpenAPI Semantic Diff 或 Markdown 文件 Diff
  -> Breaking Change 摘要（OpenAPI）
  -> MCP 查询 / OpenAPI 与 Markdown 草稿提交
  -> Vdoc Skill
```

v0.1 不做：

- MCP 绕过人工审核发布正式版本。
- 项目成员邀请流程，v0.1 由后台或 Project Admin 从现有系统用户手动加入项目。
- 复杂组织级 RBAC、多级审批流、通知、PR Bot、SDK/codegen 平台。
- GraphQL、gRPC、Postman、YApi、Apifox 导入。
- 自动修改前端仓库或字段级前端代码影响分析。
- 完整 Web 前端，只保证 HTTP API 支撑后续页面。

## 2. 当前工程约束

当前仓库是单 Go module：`vdoc`。

必须沿用：

- HTTP 框架：Gin。
- 配置：Viper，环境变量前缀 `VDOC_`。
- 日志：Zap，保留 dev/release 分流和 Gin 独立日志。
- 路由注册链：`api.InitApi -> app.RegisterRoutes -> v1.RegisterRoutes -> open/private -> module`。
- API 响应：统一 JSON envelope，语义结果放在 body 的 `code/status/message/detail`。
- 中间件顺序：`TraceID -> AccessLog -> Recovery -> optional IPRateLimit -> SecurityHeaders -> BodySizeLimit -> CORS`。
- JWT：使用 `utils/authentication`，JWT 只放必要身份标识。
- 测试：Go table-driven tests；修改全局配置的测试必须恢复状态。

当前业务代码位置：

```text
api/app/v1/open/auth/          # 注册 / 登录 / 公开认证入口
api/app/v1/open/mcp/           # MCP endpoint，使用 MCP Token 鉴权
api/app/v1/private/identity/   # 当前用户、会话信息
api/app/v1/private/systemuser/ # 系统用户管理
api/app/v1/private/team/       # Team API
api/app/v1/private/project/    # Project API
api/app/v1/private/member/     # Project Member / Role API
api/app/v1/private/document/   # Project Document API
api/app/v1/private/branch/     # Document Branch API
api/app/v1/private/draft/      # OpenAPI 与 Markdown 草稿、审核和发布 API
api/app/v1/private/version/    # Document Version API
api/app/v1/private/endpoint/   # OpenAPI Endpoint 查询 API
api/app/v1/private/diff/       # OpenAPI 与 Markdown Diff 查询 API
api/app/v1/private/mcptoken/   # MCP Token 管理 API
common/                        # 共享业务语义和枚举
db/                            # DB 连接、迁移、repository、对象存储适配
domain/<module>/               # 领域模型、领域错误、纯业务规则
services/                      # 长驻 cron、worker、consumer 生命周期层
utils/                         # token hash、schema hash、文件存储等基础工具
skills/vdoc/                   # Vdoc Skill
```

## 3. 数据与存储决策

MVP 使用 PostgreSQL 保存结构化数据。Raw、Normalized、Stable 内容快照和较大的 Diff 快照统一保存到 RustFS 或 S3-compatible 对象存储；后端在数据库中只保存 object key、hash、content type、size、etag 和 metadata。

Project 下直接管理多类型 Document。Document 通用字段包括：

- `name`
- `document_type`
- `relative_path`
- `description`
- `status`

`relative_path` 是文档在 Project 内的路径身份；不要引入第二套持久化路径/名称身份。

文档类型：

- `1` OpenAPI：校验 OpenAPI 3.x，生成 normalized snapshot、Endpoint Index、Endpoint Detail、语义 Diff 和 Breaking Change 摘要。
- `2` Markdown：校验 `.md` 内容，生成 raw/stable snapshot，支持最新内容查询和纯文件 Diff。

## 4. HTTP API 路由方向

公开路由：

```text
POST /api/v1/open/auth/register
POST /api/v1/open/auth/login
GET  /api/v1/open/health
GET  /api/v1/open/docs/openapi.yaml
POST /api/v1/open/mcp
```

私有路由按 Document 组织：

```text
GET  /api/v1/private/identity/me
GET  /api/v1/private/system/users
POST /api/v1/private/system/users
PATCH /api/v1/private/system/users/:user_id
GET  /api/v1/private/system/users/:user_id/mcp-tokens
POST /api/v1/private/system/users/:user_id/mcp-tokens/:token_id/revoke

GET  /api/v1/private/teams
POST /api/v1/private/teams
GET  /api/v1/private/teams/:team_id
PATCH /api/v1/private/teams/:team_id
POST /api/v1/private/teams/:team_id/archive

GET  /api/v1/private/projects
POST /api/v1/private/projects
GET  /api/v1/private/projects/:project_id
PATCH /api/v1/private/projects/:project_id
POST /api/v1/private/projects/:project_id/archive
GET  /api/v1/private/projects/:project_id/members
POST /api/v1/private/projects/:project_id/members
PATCH /api/v1/private/projects/:project_id/members/:user_id/role
DELETE /api/v1/private/projects/:project_id/members/:user_id

GET  /api/v1/private/projects/:project_id/documents
POST /api/v1/private/projects/:project_id/documents
GET  /api/v1/private/projects/:project_id/documents/:document_id
PATCH /api/v1/private/projects/:project_id/documents/:document_id
POST /api/v1/private/projects/:project_id/documents/:document_id/archive

GET  /api/v1/private/projects/:project_id/documents/:document_id/branches
POST /api/v1/private/projects/:project_id/documents/:document_id/branches
GET  /api/v1/private/projects/:project_id/documents/:document_id/branches/:branch_id
PATCH /api/v1/private/projects/:project_id/documents/:document_id/branches/:branch_id
POST /api/v1/private/projects/:project_id/documents/:document_id/branches/:branch_id/archive

GET  /api/v1/private/projects/:project_id/documents/:document_id/drafts
POST /api/v1/private/projects/:project_id/documents/:document_id/drafts
GET  /api/v1/private/projects/:project_id/documents/:document_id/drafts/:draft_id
PATCH /api/v1/private/projects/:project_id/documents/:document_id/drafts/:draft_id
GET  /api/v1/private/projects/:project_id/documents/:document_id/drafts/:draft_id/content/:content_kind
POST /api/v1/private/projects/:project_id/documents/:document_id/drafts/:draft_id/submit
POST /api/v1/private/projects/:project_id/documents/:document_id/drafts/:draft_id/approve
POST /api/v1/private/projects/:project_id/documents/:document_id/drafts/:draft_id/request-changes
POST /api/v1/private/projects/:project_id/documents/:document_id/drafts/:draft_id/reject
POST /api/v1/private/projects/:project_id/documents/:document_id/drafts/promote

GET  /api/v1/private/projects/:project_id/documents/:document_id/versions
GET  /api/v1/private/projects/:project_id/documents/:document_id/versions/:version_id
GET  /api/v1/private/projects/:project_id/documents/:document_id/versions/:version_id/content/:content_kind
GET  /api/v1/private/projects/:project_id/documents/:document_id/versions/:version_id/endpoints
GET  /api/v1/private/projects/:project_id/documents/:document_id/versions/:version_id/endpoints/:endpoint_id
POST /api/v1/private/projects/:project_id/documents/:document_id/diffs
GET  /api/v1/private/projects/:project_id/documents/:document_id/diffs/:diff_id
GET  /api/v1/private/projects/:project_id/documents/:document_id/diffs/:diff_id/summary

GET  /api/v1/private/mcp-tokens
POST /api/v1/private/mcp-tokens
GET  /api/v1/private/mcp-tokens/:token_id
POST /api/v1/private/mcp-tokens/:token_id/revoke
```

## 5. MCP v0.1 工具

MCP 只提供查询和草稿写入能力。正式版本发布必须由 Admin 或 SuperAdmin 通过后台审核动作触发。

```text
list_projects
list_documents
list_api_versions
get_latest_schema
get_endpoint_detail
compare_api_versions
get_change_summary
create_api_version_draft
update_api_version_draft
submit_api_version_draft
get_api_version_draft
get_latest_doc
compare_doc_versions
create_doc_draft
update_doc_draft
submit_doc_draft
get_doc_draft
```

MCP Token 创建响应返回一次性可复制 token 值；列表、详情、废弃响应必须脱敏。MCP tools 绝不能返回 JWT、MCP token 或 Authorization header。

## 6. 验收测试方向

- API 文档和 OpenAPI spec 必须覆盖 Gin 注册路由。
- OpenAPI spec 的 MCP tool enum 必须与实现保持一致。
- 文档测试必须阻止回退到旧的项目模型、旧路径、旧 ID 字段、旧工具名和持久化路径副本字段。
- Markdown 文档流必须覆盖草稿、提交、审核、发布、latest lookup、纯文件 Diff 和 no-change 检测。
- 原型分层 audit 必须继续阻止 transport/domain/background runtime 直接依赖 DB 细节。

## 7. 里程碑

| 阶段 | 目标 |
|---|---|
| 1 | 清理旧模型残留，建立共享语义和错误码 |
| 2 | PostgreSQL schema、migration、repository 和对象快照 |
| 3 | Project Document、Branch、Draft、Version 领域流 |
| 4 | OpenAPI 上传、normalize、hash、Endpoint Index 和语义 Diff |
| 5 | Markdown 上传、stable snapshot、latest lookup 和文件 Diff |
| 6 | Private REST routes、OpenAPI spec 和 API docs |
| 7 | MCP tools、token scope、审计和 Vdoc Skill |
| 8 | 文档、schema docs、测试和最终验证 |
