# 路线图与改进计划

语言: [English](IMPROVEMENTS.md) | [简体中文](IMPROVEMENTS.zh-CN.md)

本文档记录 Vdoc 的产品路线图和后端改进计划。

## 当前 v0.1 后端基础

当前仓库提供 Vdoc v0.1 的 Go/Gin 后端：

- HTTP 服务生命周期和优雅关闭
- 基于 Viper 和 `VDOC_` 环境变量的配置加载
- JWT 配置启动安全校验
- Zap 结构化日志和独立 Gin 日志
- 基于 `trace_id` 的请求追踪
- 统一响应包裹
- CORS、安全响应头、请求体大小限制、Recovery、限流中间件
- 公开注册/登录、私有 JWT 路由、MCP Token 生命周期、JSON-RPC MCP 查询和草稿 tools
- SuperAdmin 用户生命周期、Team、Project、Member、Service、Branch、OpenAPI 草稿、人工审核发布、Endpoint 索引、语义 Diff 和审计日志
- `database.enabled=true` 时通过 GORM/PostgreSQL 和规范化表持久化
- `storage.enabled=true` 时通过 RustFS 或 S3-compatible 对象存储保存 raw/normalized schema 快照
- 健康检查接口和测试

`database.enabled=false` 时仍保留内存 store 供本地开发和测试使用；`database.enabled=true` 且数据库初始化失败时会直接启动失败，不会静默退回内存模式。

## v0.1 核心链路

当前后端已经验证一条核心链路：

```text
后端上传或 AI 通过 MCP 提交 OpenAPI 草稿
        -> 人工审核后 Vdoc 创建版本
        -> Vdoc 解析接口契约
        -> Vdoc 计算语义 Diff
        -> 前端或 AI 查询变化
        -> 前端更新对接代码
```

## 1. Team、Project 和角色模型

v0.1 已经实现系统级超级管理员和项目级协作；复杂组织级 RBAC 暂缓到产品真正需要时再做。

初始模型：

```text
System
  -> User
       - SuperAdmin: 系统级用户、项目、成员和发布兜底管理

Team
  -> Project
       -> ProjectMember
            - Reader: api:read
            - Writer: api:read + api:draft
            - Admin: api:read + api:draft + api:publish + project:manage + member:manage
```

用户可以加入多个 Project，并在不同 Project 中拥有不同角色。Writer 只能创建、更新和提交草稿；发布必须由 Project Admin 或 SuperAdmin 执行。JWT 只保存必要用户身份，项目权限按 `user_id + project_id` 查询。MVP 成员从现有系统用户手动添加，不做邀请流程。

## 2. Service 和契约版本

每个 Project 可以包含多个 Service。每个 Service 管理自己的 OpenAPI 版本。

规则：

- 已发布的契约版本不可变。
- 上传变化后的 schema 会先创建草稿，审核通过后创建新版本。
- Service 下有契约分支和环境，支持 `dev`、`test`、受保护 `prod` 和可选 `feature/*`。
- Promote 会在目标分支创建草稿，并复用普通审核发布流程。
- Raw OpenAPI 必须保留，便于审计、下载和未来重新处理。
- Normalized OpenAPI 用于稳定 hash 和比较。

## 3. OpenAPI 上传流水线

目标处理流程：

```text
接收 OpenAPI YAML/JSON
  -> 读取目标 branch_id 和可选 source_git_commit_id
  -> 校验 OpenAPI 3.x
  -> 保存 raw schema
  -> 规范化 schema
  -> 计算 raw 和 normalized hash
  -> 检测无变化上传
  -> 创建 contract draft
  -> 人工审核通过
  -> 创建 contract version
  -> 解析 endpoint index
  -> 调度 semantic diff
```

v0.1 在启用对象存储时使用 RustFS 或其他 S3-compatible 存储保存 raw schema、normalized schema 和较大的 diff 快照。PostgreSQL 只保存 object key、hash 和元数据。

## 4. Endpoint Index

不要每次查询都读取大型 Raw OpenAPI 文件，而是解析成结构化索引。

索引数据包括：

- HTTP method 和 path
- Operation ID
- Tags
- Parameters
- Request body schema
- Response schemas
- Required fields
- Deprecated 状态
- Endpoint 级 hash

## 5. Semantic Diff

Vdoc 比较接口契约，而不是比较原始 JSON 文本。

初始 diff 范围：

- 新增 endpoint
- 删除 endpoint
- 修改 endpoint
- 请求参数变化
- 请求体变化
- 响应字段变化
- 安全要求变化
- Deprecated 状态变化

初始 breaking-change 规则：

- 删除 endpoint
- 新增必填参数
- 参数类型变化
- 参数位置变化
- 请求体新增必填字段
- 响应字段删除
- 响应字段类型变化
- enum 删除可选值

## 6. Change Summary

同时保存机器可读的 diff items 和人类可读的摘要。

摘要示例：

```text
user-service 1.0.0 -> 1.1.0

新增接口：0
删除接口：0
修改接口：1
Breaking changes：2

GET /api/users/{id}
- response.name deleted [breaking]
- response.age string -> number [breaking]
- response.email added [info]
```

## 7. MCP 集成

MCP 是 Vdoc 面向 AI 的核心集成入口。

先做查询和草稿工具：

```text
list_projects
list_services
list_api_versions
get_latest_schema
get_endpoint_detail
compare_api_versions
get_change_summary
create_api_version_draft
update_api_version_draft
submit_api_version_draft
get_api_version_draft
```

后续再做直接发布工具：

```text
publish_api_schema
publish_api_version
```

安全要求：

- v0.1 MCP Token 默认绑定用户，不绑定单个 Project，方便用户在 MCP 客户端中全局配置。
- MCP tool 有效权限 = token scopes 与用户在目标 Project 的 ProjectMember 角色权限交集；SuperAdmin 可兜底访问所有 Project。
- `api:read` token 不能创建或更新草稿。
- `api:draft` token 只有在用户具备目标项目 Writer/Admin/SuperAdmin 权限时才能提交草稿，但不能发布 schema。
- 发布必须由 Project Admin 或 SuperAdmin 通过具备 `api:publish` 的人工审核动作触发。
- 用户可以在后台查看和复制自己 active 状态的完整 MCP Token，也可以生成新 token 并废弃旧 token。
- 后端使用 `token_hash` 做调用鉴权，使用加密保存的 `token_ciphertext` 支持后台展示。
- 草稿写入、token 查看/复制、token 废弃、token 使用和发布操作必须可审计。
- MCP tools 绝不能返回原始密钥；只有后台 token 管理接口可以向 token 所属用户返回完整 token。
- 项目绑定机器人/CI Token 放到 v0.2 评估。

## 8. 存储方向

推荐分层：

```text
PostgreSQL
  - users, teams, projects, members, permissions
  - services and version metadata
  - endpoint index
  - diff result and change summary
  - status、type、method、severity、scope 等有限集合字段的整数码

RustFS Object Storage
  - raw OpenAPI snapshots
  - normalized OpenAPI snapshots
  - full diff snapshots
  - optional compressed schema AST

Redis / Queue
  - parse jobs
  - diff jobs
  - later codegen jobs
```

## 9. 后续 Skill 支持

MCP 提供工具能力。未来可以通过 Skill 告诉 AI Agent 如何正确使用这些工具。

可能的 Skill 工作流：

- 后端或 AI 提交更新后的 API 契约草稿。
- 审核人查看草稿 diff preview 后发布。
- 前端比较两个 API 版本。
- 前端请求某个 endpoint 的 TypeScript 类型和请求函数。
- AI 识别 breaking changes 可能影响的前端文件。

## 当前非目标

- 复杂组织和计费模型
- GraphQL、gRPC、Postman、YApi、Apifox 导入
- 完整 SDK/codegen 平台
- 自动修改前端仓库
- 复杂多级审批流程
- 公共 SaaS 多租户硬化

## 成功标准

如果满足以下条件，MVP 就有价值：

1. 后端开发能在 1 分钟内提交 OpenAPI 草稿，Project Admin 能在审核后发布版本。
2. Vdoc 能展示相较上一版本发生了什么变化。
3. Vdoc 能识别常见 breaking changes。
4. 前端能通过 Web UI 或 MCP 查询接口详情和版本差异。
5. AI Agent 能通过 MCP 提交 OpenAPI 草稿，并明确等待人工审核发布。
6. AI Agent 能基于 MCP 返回结果生成或更新前端对接代码。
