# Vdoc v0.1 工程实现计划

本文档把主 PRD 的 v0.1 MVP 拆成可执行的 Go/Gin 后端工程计划。主需求来源见 `PRD.md`。

## 1. 范围

v0.1 必须跑通：

```text
用户登录
  -> 系统超级管理员 / 成员
  -> Team / Project / Role
  -> Service
  -> Branch / Environment
  -> OpenAPI 上传并创建草稿
  -> 人工审核发布 Contract Version
  -> Endpoint Index / Detail
  -> Semantic Diff
  -> Breaking Change
  -> MCP 查询 / OpenAPI 草稿提交
  -> 人工审核发布
  -> Vdoc Skill
```

v0.1 不做：

- MCP 直接发布 OpenAPI，放到 v0.2。
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

建议新增业务代码位置：

```text
api/app/v1/open/auth/          # 注册 / 登录 / 公开认证入口
api/app/v1/private/identity/   # 当前用户、会话信息
api/app/v1/private/team/       # Team API
api/app/v1/private/project/    # Project、Member、Role API
api/app/v1/private/service/    # Service API
api/app/v1/private/contract/   # OpenAPI 上传和版本 API
api/app/v1/private/contract_draft/ # OpenAPI 草稿、审核和发布 API
api/app/v1/private/endpoint/   # Endpoint 查询 API
api/app/v1/private/diff/       # Diff 查询 API
api/app/v1/private/mcp_token/  # MCP Token 管理 API
api/app/v1/open/mcp/           # MCP endpoint，使用 MCP Token 鉴权
common/                        # API DTO、分页、枚举、ID 类型
config/                        # DB / storage / OpenAPI / MCP 配置
db/                            # DB 连接、迁移、repository
domain/<module>/               # 领域模型、领域错误、纯业务规则
services/<module>/             # 应用服务编排
utils/                         # token hash、schema hash、文件存储等基础工具
skills/vdoc/                   # Vdoc Skill
```

## 3. 数据与存储决策

MVP 使用 PostgreSQL 保存结构化数据。Raw OpenAPI、Normalized OpenAPI 和较大的 Diff 快照统一保存到 RustFS；后端通过 S3-compatible API 接入 RustFS，并在数据库中只保存 object key、hash 和元数据。

实现前需要确认并落地：

1. PostgreSQL DSN、连接池和 migration 运行方式。
2. repository 目录规范。
3. RustFS endpoint、bucket、access key、secret key、region 和 TLS 配置。
4. OpenAPI 单文件大小限制，默认沿用 `config.MaxBodySize`。

建议优先级：

| 阶段 | 决策 | 默认建议 |
|---|---|---|
| DB 访问 | PostgreSQL + Go 数据访问方案 | `pgx`、`database/sql` + `sqlc`、GORM 三选一，优先保证表结构清晰和测试可独立跑 |
| migration | 独立迁移工具或 Go 内置 runner | 先保证 `make test` 可独立跑 |
| 对象存储 | RustFS + S3-compatible client | Raw/Normalized OpenAPI 和大 Diff 快照都走 RustFS |
| 异步任务 | 进程内 worker | 后续适配 Redis/Queue |

## 4. 数据模型顺序

第一批：身份和协作边界。

```text
users                # 含系统级 is_super_admin / 状态
teams
projects
project_members      # Project 内 Admin / Writer / Reader
```

第二批：API 契约核心。

```text
api_services
api_contract_branches
api_contract_drafts
api_contract_versions
api_endpoints
api_endpoint_details
```

第三批：Diff 和 AI 集成。

```text
api_version_diffs
api_diff_items
mcp_tokens
audit_logs
```

关键约束：

- `api_contract_versions` 已发布后不可变。
- MCP 写入只能落到 `api_contract_drafts`，不能直接创建 `api_contract_versions`。
- 草稿状态、角色、分支类型、schema 格式、source type、actor type、HTTP method、diff 状态、severity、diff change type、MCP scopes 等有限集合字段都使用从 1 开始的整数码，DB 层用 `smallint` 或 `smallint[]`，不使用 text enum。
- OpenAPI 上传字段必须包含 `branch_id`，可选包含 `source_git_commit_id`。该字段表示用户应用或代码仓库的 Git commit ID，不是 Vdoc 自身 Git commit，也不是 Vdoc 契约分支。
- 发布时把 `api_contract_drafts.source_git_commit_id` 复制到 `api_contract_versions.source_git_commit_id`。
- 同一 `service_id + branch_id` 下 `version_name` 唯一。
- `prod` 分支默认受保护，发布必须由 Project Admin 或 SuperAdmin 审核。
- `raw_schema_hash` 和 `normalized_schema_hash` 都要保存。
- `api_endpoints` 以 `contract_version_id + method + path` 唯一。
- `mcp_tokens.user_id` 绑定用户，不绑定单个 Project；保存 `token_hash` 用于鉴权匹配，同时保存加密的 `token_ciphertext` 用于后台查看和复制。
- MCP tool 有效权限 = token scope codes 与用户在目标 Project 的角色权限交集；SuperAdmin 可兜底访问所有 Project。
- v0.1 不做项目绑定机器人/CI Token，作为 v0.2 扩展。
- 所有项目级资源必须带 `project_id` 或可追溯到 `project_id`。
- JWT 只保存必要用户身份，项目角色按 `user_id + project_id` 实时查询。
- Writer 只能创建、更新和提交草稿；发布必须由 Project Admin 或 SuperAdmin 执行。

## 5. API 路由顺序

所有私有业务 API 默认走 `/api/v1/private`，需要 JWT。

```text
POST /api/v1/open/auth/register
POST /api/v1/open/auth/login
GET  /api/v1/private/identity/me

GET   /api/v1/private/system/users
POST  /api/v1/private/system/users
PATCH /api/v1/private/system/users/:user_id

POST /api/v1/private/teams
GET  /api/v1/private/teams
GET  /api/v1/private/teams/:team_id

POST /api/v1/private/projects
GET  /api/v1/private/projects
GET  /api/v1/private/projects/:project_id
POST /api/v1/private/projects/:project_id/members
PATCH /api/v1/private/projects/:project_id/members/:user_id/role

POST /api/v1/private/projects/:project_id/services
GET  /api/v1/private/projects/:project_id/services
GET  /api/v1/private/projects/:project_id/services/:service_id
GET  /api/v1/private/projects/:project_id/services/:service_id/branches
POST /api/v1/private/projects/:project_id/services/:service_id/branches
PATCH /api/v1/private/projects/:project_id/services/:service_id/branches/:branch_id

GET  /api/v1/private/projects/:project_id/services/:service_id/contracts
GET  /api/v1/private/projects/:project_id/services/:service_id/contracts/:version_id

POST /api/v1/private/projects/:project_id/services/:service_id/contract-drafts
GET  /api/v1/private/projects/:project_id/services/:service_id/contract-drafts
GET  /api/v1/private/projects/:project_id/services/:service_id/contract-drafts/:draft_id
PATCH /api/v1/private/projects/:project_id/services/:service_id/contract-drafts/:draft_id
POST /api/v1/private/projects/:project_id/services/:service_id/contract-drafts/:draft_id/submit
POST /api/v1/private/projects/:project_id/services/:service_id/contract-drafts/:draft_id/approve
POST /api/v1/private/projects/:project_id/services/:service_id/contract-drafts/:draft_id/request-changes
POST /api/v1/private/projects/:project_id/services/:service_id/contract-drafts/:draft_id/reject
POST /api/v1/private/projects/:project_id/services/:service_id/contract-drafts/promote

GET  /api/v1/private/projects/:project_id/services/:service_id/contracts/:version_id/endpoints
GET  /api/v1/private/projects/:project_id/services/:service_id/contracts/:version_id/endpoints/:endpoint_id

POST /api/v1/private/projects/:project_id/services/:service_id/diffs
GET  /api/v1/private/projects/:project_id/services/:service_id/diffs/:diff_id
GET  /api/v1/private/projects/:project_id/services/:service_id/diffs/:diff_id/summary

POST /api/v1/private/mcp-tokens
GET  /api/v1/private/mcp-tokens
GET  /api/v1/private/mcp-tokens/:token_id
POST /api/v1/private/mcp-tokens/:token_id/revoke
GET  /api/v1/private/system/users/:user_id/mcp-tokens
POST /api/v1/private/system/users/:user_id/mcp-tokens/:token_id/revoke
```

MCP endpoint v0.1 查询和草稿写入：

```text
POST /api/v1/open/mcp
```

MCP tool 规划：

| Tool | v0.1 | Scope |
|---|---|---|
| `list_projects` | 必做 | `api:read` |
| `list_services` | 必做 | `api:read` |
| `list_api_versions` | 必做 | `api:read` |
| `get_latest_schema` | 可选 | `api:read` |
| `get_endpoint_detail` | 必做 | `api:read` |
| `compare_api_versions` | 必做 | `api:read` |
| `get_change_summary` | 必做 | `api:read` |
| `create_api_version_draft` | 必做 | `api:draft` |
| `update_api_version_draft` | 必做 | `api:draft` |
| `submit_api_version_draft` | 必做 | `api:draft` |
| `get_api_version_draft` | 必做 | `api:read` |
| `publish_api_schema` | v0.2 | `api:publish` |

## 6. 里程碑

### Milestone 0：工程底座确认

目标：让业务开发前的基础设施可用。

交付：

- DB 连接配置和健康检查扩展。
- migration 运行方式。
- repository/service/handler 分层约定。
- 私有路由 JWT 中间件挂载方式。
- 测试数据库或 repository mock 策略。

验收：

```text
make test
make lint
```

### Milestone 1：身份、Team、Project、Role

目标：项目级协作模型可用。

交付：

- `users`、`teams`、`projects`、`project_members`。
- 系统级 SuperAdmin 与普通成员账号。
- 注册/登录或最小可用用户创建方式。
- JWT 登录态。
- Reader / Writer / Admin 项目级权限判断。
- Team、Project、Member API，其中 Member API 只从现有系统用户手动添加成员，不做邀请状态。

验收：

- SuperAdmin 可以创建系统成员、Team / Project，并指定 Project Admin。
- Project Admin 可以从现有系统用户添加成员并分配角色。
- Reader 不能写入项目资源。
- Writer 可以执行 `api:draft`，但不能执行 `api:publish`。
- Project Admin 可以审核并发布 Contract Version。

### Milestone 2：Service、Contract Draft 与 Contract Version

目标：能按分支上传 OpenAPI 草稿，并在人工审核后保存不可变 OpenAPI 版本。

交付：

- `api_services`、`api_contract_branches`、`api_contract_drafts`、`api_contract_versions`。
- Service 创建时初始化 `dev`、`test`、`prod` 分支，`prod` 默认 protected。
- OpenAPI JSON/YAML 上传和草稿创建接口，入参包含 `branch_id`，可选包含 `source_git_commit_id`。
- OpenAPI 3.x 基础校验。
- Raw schema 保存。
- Normalized schema 生成和 hash。
- 重复上传相同 normalized hash 返回 No Changes。
- 草稿提交审核、退回修改、拒绝和批准发布接口。
- 跨分支 Promote 创建目标分支草稿，记录 `source_branch_id`、`source_version_id` 和 `base_version_id`，再走普通审核发布。

验收：

- Writer 上传合法 OpenAPI 后在目标 `branch_id` 下生成草稿。
- 非法 OpenAPI 返回明确错误。
- 同一内容重复上传不创建新版本。
- 审核通过后生成不可变 Contract Version。
- 发布后版本唯一性按 `service_id + branch_id + version_name` 保证。
- `source_git_commit_id` 从草稿复制到发布版本。
- Reader 可以查看版本列表和详情。

### Milestone 3：Endpoint Index 和详情

目标：前端和 AI 可以按版本查询接口契约。

交付：

- `api_endpoints`、`api_endpoint_details`。
- 解析 method、path、operationId、summary、tags、deprecated。
- 解析 parameters、request body、responses、schema fields。
- Endpoint 列表和详情 API。
- 按 path 搜索 endpoint。

验收：

- 上传后自动生成 endpoint index。
- Reader 可以查看 endpoint 列表。
- Reader 可以查看 endpoint 详情。
- `get_endpoint_detail` 所需数据完整可用。

### Milestone 4：Semantic Diff 和 Breaking Change

目标：两个 Contract Version 可以生成结构化语义 Diff。

交付：

- `api_version_diffs`、`api_diff_items`。
- Endpoint added / removed / modified。
- Query/path/header param added / removed / type changed。
- Request body field added / removed / type changed / required changed / enum changed。
- Response status code added / removed。
- Response field added / removed / type changed / required changed / enum changed。
- Breaking rules 标记 severity。
- Diff summary 和 frontend impact。

验收：

- 可以比较同一 Service 任意两个版本。
- 删除 endpoint 标记 breaking。
- 新增必填请求参数标记 breaking。
- 删除响应字段标记 breaking。
- 响应字段类型变化标记 breaking。
- 新增可选响应字段标记 info。

### Milestone 5：MCP Token、MCP 查询和草稿提交

目标：AI Agent 可以通过 MCP 查询真实接口契约和版本变化，也可以提交或更新 OpenAPI 草稿。

交付：

- `mcp_tokens`，默认绑定 `user_id`。
- `token_hash` + 加密 `token_ciphertext` 存储，支持后台查看和复制完整 token。
- Token 状态使用整数码，保存 `revoked_at`。
- scopes 使用整数码数组，校验 token 所属用户在目标 Project 下的角色权限。
- 查询 MCP tools。
- `api:draft` MCP tools。
- MCP tool 错误响应规范。
- 审计日志记录 token 使用，至少记录查询和草稿写入行为。

验收：

- 用户可以创建新的 MCP Token。
- 用户可以在后台查看和复制自己 active 状态的完整 MCP Token。
- 用户可以废弃自己的旧 MCP Token，废弃后不可恢复使用。
- SuperAdmin 可以废弃任意用户的 MCP Token。
- 无效、过期、废弃 token 不能访问 MCP。
- `api:read` token 可以调用查询工具，但只能访问 token 所属用户有权限的项目。
- `api:draft` token 只有在 token 所属用户具备目标项目 Writer/Admin/SuperAdmin 权限时，才可以创建、更新和提交草稿。
- MCP 不能直接发布 Contract Version。
- `get_endpoint_detail` 不存在时返回 not found，不编造字段。
- `compare_api_versions` 返回 summary 和 diff items。
- `get_change_summary` 区分 must-handle 和 optional。

### Milestone 6：Vdoc Skill

目标：AI 有稳定工作流指导，不依赖提示词猜接口。

交付：

```text
skills/vdoc/SKILL.md
skills/vdoc/templates/frontend-change-summary.md
skills/vdoc/templates/endpoint-integration.md
skills/vdoc/examples/compare-versions-example.md
skills/vdoc/examples/endpoint-query-example.md
```

验收：

- Skill 明确要求以 Vdoc MCP 为事实来源。
- 生成代码前必须调用 `get_endpoint_detail`。
- 分析版本变化前必须调用 `compare_api_versions`。
- 提交 OpenAPI 时必须走 draft tools，并提示需要人工审核发布。
- 输出必须区分 must-handle 和 optional。
- 禁止暴露 MCP token。

## 7. 实现顺序

推荐按垂直切片推进，每个切片包含：migration、domain、repository、service、handler、route、test。

| 顺序 | 切片 | 依赖 |
|---|---|---|
| 1 | DB 和分层约定 | 当前脚手架 |
| 2 | identity + JWT 登录 | DB |
| 3 | team/project/member/role | identity |
| 4 | service CRUD | project 权限 |
| 5 | contract draft + schema storage | service CRUD |
| 6 | OpenAPI normalize/hash | contract draft |
| 7 | draft review + contract publish | normalized contract draft |
| 8 | endpoint parse/query | contract version |
| 9 | semantic diff | endpoint index |
| 10 | breaking rules + summary | diff items |
| 11 | MCP token | identity + project 权限 |
| 12 | MCP read + draft tools | endpoint/diff/mcp token/contract draft |
| 13 | Vdoc Skill | MCP tools |

## 8. 测试策略

每个模块至少覆盖：

- domain 规则测试。
- service 编排测试。
- repository 测试，使用测试数据库或可替换接口。
- handler 测试，使用 `gin.SetMode(gin.TestMode)` 和 `httptest`。
- 权限测试，覆盖 Reader / Writer / Admin。
- 上传和 diff 的表驱动测试。

重点测试用例：

```text
OpenAPI 上传
- JSON 成功
- YAML 成功
- 非 OpenAPI 失败
- OpenAPI 版本不支持失败
- 重复上传 No Changes

Contract Draft
- AI/MCP 创建草稿成功
- 更新 draft 成功
- submitted 草稿可以 request changes
- approved 草稿发布为不可变 Contract Version
- MCP 不能直接发布版本

Endpoint 解析
- path/query/header 参数
- required 字段
- request body schema
- response schema
- enum

Diff
- endpoint added / removed
- request required added
- request field type changed
- response field removed
- response field type changed
- response field added optional

MCP
- invalid token
- read token success
- read token cannot call draft tool
- draft token can create/update/submit draft only when the token owner has project draft permission
- draft token cannot publish version
- endpoint not found
- version not found
```

## 9. 验证命令

每个里程碑完成后至少运行：

```bash
make fmt
make lint
make test
```

发布前运行：

```bash
make verify
```

涉及 HTTP 行为时增加最小手工验证：

```text
1. 启动服务。
2. 登录获取 JWT。
3. 创建 Team / Project / Service。
4. 上传 openapi-v1.yaml。
5. 上传 openapi-v2.yaml。
6. 查询 endpoint detail。
7. 查询 diff summary。
8. 使用用户级 MCP `api:read` token 调用查询 tool。
9. 使用用户级 MCP `api:draft` token 在有 Writer/Admin 权限的项目中创建并提交草稿。
10. 在后台审核草稿并发布版本。
```

## 10. 风险和处理

| 风险 | 处理 |
|---|---|
| 一开始做太完整的 RBAC | 只做项目级 Reader / Writer / Admin |
| OpenAPI 解析规则过重 | 先覆盖 PRD 的 MVP diff 范围 |
| Diff 准确性不足 | 内部 Diff Item 模型稳定，后续可接 oasdiff/openapi-diff |
| MCP 写入带来安全风险 | v0.1 只允许写草稿，发布必须人工审核；`api:draft` 与 `api:publish` 分离 |
| RustFS 部署或 S3 协议差异 | 用 storage interface 隔离 RustFS S3-compatible client 和业务层，配置 endpoint/bucket/credentials |
| AI 编造接口字段 | Skill 强制使用 MCP 返回作为事实来源 |

## 11. v0.2 入口

v0.1 完成后再进入：

- 可选 `publish_api_schema` MCP 直接发布（默认关闭）。
- 内置 AI 审核建议。
- 写入操作审计增强。
- Docker Compose 完善，包含 PostgreSQL 和 RustFS。
- 前端变更摘要增强。
- Skill 示例增强。
- Draft / Review / Publish 多级流程增强。
