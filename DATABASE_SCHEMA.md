# Vdoc v0.1 PostgreSQL 数据库表设计

本文档定义 Vdoc v0.1 MVP 的数据库表设计，供产品和工程评审使用。范围覆盖用户、团队、项目、服务、契约分支、草稿、版本、Endpoint 索引、Endpoint 详情、语义 Diff、MCP Token 和审计日志。

本文固定以下设计决策：PostgreSQL 保存结构化数据；RustFS 通过 S3-compatible API 保存 Raw OpenAPI、Normalized OpenAPI 和大型 Diff 快照；PostgreSQL 只保存 RustFS object key、hash、元数据、Endpoint 索引、Endpoint 详情和 Diff Items。

## 1. 设计边界

| 项目 | v0.1 决策 |
|---|---|
| 结构化数据 | 使用 PostgreSQL 保存用户、团队、项目、角色、服务、分支、草稿、版本、Endpoint 索引、Endpoint 详情、Diff 摘要、Diff Items、MCP Token 和审计日志。 |
| 对象存储 | 使用 RustFS，通过 S3-compatible API 接入。Raw OpenAPI、Normalized OpenAPI 和较大的 Diff 快照只存对象存储。 |
| 数据库中的对象引用 | PostgreSQL 只保存 RustFS object key、hash、content type、size、etag、metadata 等引用和校验数据。 |
| Endpoint 查询 | 列表查询走 `api_endpoints`，详情查询走 `api_endpoint_details`，不读取 Raw OpenAPI。 |
| Diff 查询 | 摘要和机器可读条目存 PostgreSQL，大型完整 Diff 快照存 RustFS。 |
| MCP Token | v0.1 只做用户绑定 Token，不做 Project 绑定 Token。 |
| Merge 和 Promote | v0.1 只建模跨分支提升草稿，不设计复杂 Git 风格 merge、rebase、conflict 表。 |

## 2. 通用列和命名约定

所有业务表使用 PostgreSQL 原生类型，字段使用 `snake_case`。主键使用 UUID，建议由数据库端 `gen_random_uuid()` 生成。

| 列名 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键，默认 `gen_random_uuid()`。 |
| `created_at` | `timestamptz` | 是 | 创建时间，默认 `now()`。 |
| `updated_at` | `timestamptz` | 是 | 更新时间，默认 `now()`，由应用或触发器维护。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。适用于用户、团队、项目、成员、服务、分支、草稿和 Token 等可隐藏记录。不可变版本、Diff 和审计日志默认不软删。 |

这些列参考常见 GORM base model 的 `ID`、`CreatedAt`、`UpdatedAt`、`DeletedAt` 习惯，但落到 PostgreSQL 时使用 `uuid` 和 `timestamptz`。唯一约束如果需要忽略软删除记录，应使用 partial unique index，例如 `WHERE deleted_at IS NULL`。

## 3. 状态值、类型码和权限值

所有有限集合字段在数据库中都使用从 1 开始的整数码，不使用 text enum。建议类型为 `smallint`，数组使用 `smallint[]`。下表中的英文名称只用于代码常量、API DTO 和文档展示。

| 对象 | 字段 | 类型 | Code map |
|---|---|---|---|
| `users` | `status` | `smallint` | 1 active、2 disabled |
| `projects` | `status` | `smallint` | 1 active、2 archived |
| `project_members` | `role` | `smallint` | 1 reader、2 writer、3 admin |
| `project_members` | `status` | `smallint` | 1 active、2 disabled |
| `api_services` | `status` | `smallint` | 1 active、2 archived |
| `api_contract_branches` | `kind` | `smallint` | 1 environment、2 feature |
| `api_contract_branches` | `status` | `smallint` | 1 active、2 archived |
| `api_contract_drafts` | `status` | `smallint` | 1 draft、2 submitted、3 changes_requested、4 rejected、5 published |
| `api_contract_versions` | `status` | `smallint` | 1 published |
| `api_contract_drafts`、`api_contract_versions` | `schema_format` | `smallint` | 1 openapi-3.0、2 openapi-3.1 |
| `api_contract_drafts`、`api_contract_versions` | `source_type` | `smallint` | 1 web_upload、2 mcp_upload、3 promote |
| `api_contract_drafts`、`audit_logs` | actor type 字段 | `smallint` | 1 user、2 mcp_token、3 system |
| `api_version_diffs` | `diff_status` | `smallint` | 1 pending、2 running、3 succeeded、4 failed |
| `api_diff_items` | `severity` | `smallint` | 1 info、2 warning、3 breaking |
| `api_endpoints` | `method` | `smallint` | 见 Endpoint method code map |
| `api_diff_items` | `change_type` | `smallint` | 见 Diff change type code map |
| `mcp_tokens` | `status` | `smallint` | 1 active、2 revoked、3 expired |
| `mcp_tokens` | `scopes` | `smallint[]` | 1 api:read、2 api:draft |

Endpoint method code map 固定如下。

| Code | Method |
|---|---|
| 1 | GET |
| 2 | POST |
| 3 | PUT |
| 4 | PATCH |
| 5 | DELETE |
| 6 | OPTIONS |
| 7 | HEAD |
| 8 | TRACE |

Diff change type code map 固定如下。

| Code | 名称 | 说明 | 默认严重度码 |
|---|---|---|---|
| 1 | endpoint_added | 新增 Endpoint | 1 |
| 2 | endpoint_removed | 删除 Endpoint | 3 |
| 3 | endpoint_modified | Endpoint 结构变化 | 2 |
| 4 | request_parameter_added | 新增请求参数 | 1 或 3 |
| 5 | request_parameter_removed | 删除请求参数 | 2 |
| 6 | request_parameter_changed | 请求参数类型、位置或必填状态变化 | 2 或 3 |
| 7 | request_body_changed | 请求体变化 | 2 或 3 |
| 8 | response_field_added | 响应字段新增 | 1 |
| 9 | response_field_removed | 响应字段删除 | 3 |
| 10 | response_field_changed | 响应字段类型、格式或必填状态变化 | 2 或 3 |
| 11 | security_changed | 鉴权要求变化 | 2 或 3 |
| 12 | deprecated_changed | Deprecated 状态变化 | 1 或 2 |
| 13 | enum_value_removed | enum 可选值删除 | 3 |

项目角色权限固定为下表。SuperAdmin 是系统级兜底角色，不写入 `project_members.role`。

| 项目角色 | 权限 |
|---|---|
| `reader` | `api:read` |
| `writer` | `api:read`、`api:draft` |
| `admin` | `api:read`、`api:draft`、`api:publish`、`project:manage`、`member:manage` |
| SuperAdmin | 所有项目的所有权限 |

MCP Token 有效权限 = token scope codes 与用户在目标 Project 的角色权限取交集。SuperAdmin 可访问所有 Project。v0.1 MCP Token 可授予 `api:read` 和 `api:draft`，不能绕过人工审核直接发布版本。

## 4. Branch 和 Environment 模型

`api_contract_branches` 用来表达 Service 下的契约环境和分支。它不是代码仓库分支，而是接口契约发布轨道。

| 规则 | 说明 |
|---|---|
| 默认分支 | 每个 Service 创建时初始化 `dev`、`test`、`prod`。 |
| 可选分支 | 允许创建 `feature/*`，例如 `feature/checkout-v2`。 |
| 受保护分支 | `prod` 默认 `is_protected = true`。发布到 `prod` 必须由 Project Admin 或 SuperAdmin 审核。 |
| 唯一性 | 同一 Service 下分支名唯一，约束为 `UNIQUE (service_id, name)`。 |
| 默认查询 | 未指定 branch 时，可由产品约定默认读 `prod` 或项目选择的当前环境。数据库不隐式跨分支读取。 |
| 版本唯一性 | 契约版本按分支隔离，约束为 `UNIQUE (service_id, branch_id, version_name)`。 |

### Merge 和 Promote 流程

跨分支提升用于把源分支最新已发布版本提交到目标分支审核。v0.1 不引入复杂冲突表，只创建一条目标草稿。

| 步骤 | 数据落点 |
|---|---|
| 选择源分支 | 读取 `source_branch_id` 的最新 `published` 版本，记为 `source_version_id`。 |
| 选择目标分支 | 目标分支写入草稿的 `branch_id`。 |
| 选择目标基线 | 读取目标分支最新 `published` 版本，写入 `base_version_id`。 |
| 创建提升草稿 | 在 `api_contract_drafts` 创建记录，`branch_id` 是目标分支，`source_branch_id`、`source_version_id`、`base_version_id` 全部写入。 |
| 生成预览 | 以 `base_version_id` 对比 `source_version_id`，把摘要写入 `diff_preview_json`，大预览写 RustFS 并保存 `diff_preview_object_key`。 |
| 审核发布 | 走普通草稿审核。发布后在目标 `branch_id` 下创建不可变 `api_contract_versions`。 |

## 5. 表关系总览

| 表 | 作用 |
|---|---|
| `users` | 系统用户，含 SuperAdmin 标记。 |
| `teams` | 团队协作边界。 |
| `projects` | Team 下的产品或应用。 |
| `project_members` | 用户在 Project 内的角色。 |
| `api_services` | Project 下的后端服务。 |
| `api_contract_branches` | Service 下的环境和功能分支。 |
| `api_contract_drafts` | 待审核 OpenAPI 草稿。 |
| `api_contract_versions` | 已发布且不可变的契约版本。 |
| `api_endpoints` | 从版本解析出的 Endpoint 索引。 |
| `api_endpoint_details` | Endpoint 参数、请求体、响应体和归一化 Operation JSON。 |
| `api_version_diffs` | 版本间 Diff 摘要和 RustFS 大快照引用。 |
| `api_diff_items` | 机器可读的 Diff 条目。 |
| `mcp_tokens` | 用户绑定的 MCP Token。 |
| `audit_logs` | 审计日志。 |

## 6. 表设计

### 6.1 `users`

保存系统用户。SuperAdmin 通过 `is_super_admin` 标记。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `email` | `text` | 是 | 登录邮箱，按小写唯一。 |
| `password_hash` | `text` | 是 | 密码哈希。 |
| `display_name` | `text` | 是 | 展示名称。 |
| `is_super_admin` | `boolean` | 是 | 默认 `false`。 |
| `status` | `smallint` | 是 | 用户状态码，1 active、2 disabled。 |
| `last_login_at` | `timestamptz` | 否 | 最近登录时间。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| 状态检查 | `CHECK (status IN (1, 2))` |
| 邮箱唯一 | `UNIQUE (lower(email)) WHERE deleted_at IS NULL` |
| 查询索引 | `INDEX (status)`、`INDEX (is_super_admin)` |

### 6.2 `teams`

保存团队。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `name` | `text` | 是 | 团队名称。 |
| `slug` | `text` | 是 | 团队短标识。 |
| `description` | `text` | 否 | 团队说明。 |
| `created_by` | `uuid` | 是 | 创建人，引用 `users.id`。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| 创建人外键 | `FOREIGN KEY (created_by) REFERENCES users(id)` |
| Slug 唯一 | `UNIQUE (lower(slug)) WHERE deleted_at IS NULL` |
| 查询索引 | `INDEX (created_by)` |

### 6.3 `projects`

保存 Team 下的项目。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `team_id` | `uuid` | 是 | 所属 Team。 |
| `name` | `text` | 是 | 项目名称。 |
| `slug` | `text` | 是 | Team 内唯一短标识。 |
| `description` | `text` | 否 | 项目说明。 |
| `status` | `smallint` | 是 | 项目状态码，1 active、2 archived。 |
| `created_by` | `uuid` | 是 | 创建人。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Team 外键 | `FOREIGN KEY (team_id) REFERENCES teams(id)` |
| 创建人外键 | `FOREIGN KEY (created_by) REFERENCES users(id)` |
| 状态检查 | `CHECK (status IN (1, 2))` |
| Team 内唯一 | `UNIQUE (team_id, lower(slug)) WHERE deleted_at IS NULL` |
| 查询索引 | `INDEX (team_id, status)`、`INDEX (created_by)` |

### 6.4 `project_members`

保存用户在 Project 内的角色。MVP 不做邀请流程，成员由后台或 Project Admin 从现有系统用户手动添加。权限按 `user_id + project_id` 实时查询，JWT 不长期保存项目角色。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `project_id` | `uuid` | 是 | 所属 Project。 |
| `user_id` | `uuid` | 是 | 成员用户。 |
| `role` | `smallint` | 是 | 项目角色码，1 reader、2 writer、3 admin。 |
| `status` | `smallint` | 是 | 成员状态码，1 active、2 disabled。 |
| `added_by` | `uuid` | 是 | 添加成员的后台用户或 Project Admin。 |
| `added_at` | `timestamptz` | 是 | 添加成员时间。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Project 外键 | `FOREIGN KEY (project_id) REFERENCES projects(id)` |
| User 外键 | `FOREIGN KEY (user_id) REFERENCES users(id)` |
| 添加人外键 | `FOREIGN KEY (added_by) REFERENCES users(id)` |
| 角色检查 | `CHECK (role IN (1, 2, 3))` |
| 状态检查 | `CHECK (status IN (1, 2))` |
| 成员唯一 | `UNIQUE (project_id, user_id) WHERE deleted_at IS NULL` |
| 查询索引 | `INDEX (user_id, status)`、`INDEX (project_id, role)` |

### 6.5 `api_services`

保存项目内的后端服务，例如 `user-service`。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `project_id` | `uuid` | 是 | 所属 Project。 |
| `name` | `text` | 是 | 服务稳定名称，例如 `user-service`。 |
| `display_name` | `text` | 否 | 展示名称。 |
| `description` | `text` | 否 | 服务说明。 |
| `base_path` | `text` | 否 | 服务默认 API 前缀。 |
| `status` | `smallint` | 是 | 服务状态码，1 active、2 archived。 |
| `created_by` | `uuid` | 是 | 创建人。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Project 外键 | `FOREIGN KEY (project_id) REFERENCES projects(id)` |
| 创建人外键 | `FOREIGN KEY (created_by) REFERENCES users(id)` |
| 状态检查 | `CHECK (status IN (1, 2))` |
| Project 内服务名唯一 | `UNIQUE (project_id, lower(name)) WHERE deleted_at IS NULL` |
| 查询索引 | `INDEX (project_id, status)` |

### 6.6 `api_contract_branches`

保存 Service 下的契约环境和功能分支。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `service_id` | `uuid` | 是 | 所属 Service。 |
| `name` | `text` | 是 | 分支名。默认 `dev`、`test`、`prod`，可选 `feature/*`。 |
| `kind` | `smallint` | 是 | 分支类型码，1 environment、2 feature。 |
| `description` | `text` | 否 | 分支说明。 |
| `is_default` | `boolean` | 是 | 是否默认分支，每个 Service 最多一个。 |
| `is_protected` | `boolean` | 是 | 是否受保护。`prod` 默认 true。 |
| `status` | `smallint` | 是 | 分支状态码，1 active、2 archived。 |
| `created_by` | `uuid` | 是 | 创建人。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Service 外键 | `FOREIGN KEY (service_id) REFERENCES api_services(id)` |
| 创建人外键 | `FOREIGN KEY (created_by) REFERENCES users(id)` |
| 类型检查 | `CHECK (kind IN (1, 2))` |
| 状态检查 | `CHECK (status IN (1, 2))` |
| 分支名唯一 | `UNIQUE (service_id, name)` |
| 默认分支唯一 | `UNIQUE (service_id) WHERE is_default = true AND deleted_at IS NULL` |
| Feature 命名 | `CHECK (kind <> 2 OR name LIKE 'feature/%')` |
| 默认环境命名 | `CHECK (kind <> 1 OR name IN ('dev', 'test', 'prod'))` |
| 查询索引 | `INDEX (service_id, status)`、`INDEX (service_id, is_protected)` |

### 6.7 `api_contract_drafts`

保存 OpenAPI 草稿。Web 上传和 MCP 草稿写入都进入此表，不能直接创建正式版本。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `service_id` | `uuid` | 是 | 所属 Service。 |
| `branch_id` | `uuid` | 是 | 目标分支。普通草稿和 promote 草稿都写目标分支。 |
| `version_name` | `text` | 是 | 待发布版本名，例如 `1.2.0`。 |
| `status` | `smallint` | 是 | 草稿状态码，1 draft、2 submitted、3 changes_requested、4 rejected、5 published。 |
| `schema_format` | `smallint` | 是 | Schema 格式码，1 openapi-3.0、2 openapi-3.1。 |
| `raw_schema_object_key` | `text` | 是 | RustFS Raw OpenAPI object key。 |
| `normalized_schema_object_key` | `text` | 是 | RustFS Normalized OpenAPI object key。 |
| `raw_schema_hash` | `text` | 是 | Raw OpenAPI SHA-256。 |
| `normalized_schema_hash` | `text` | 是 | Normalized OpenAPI SHA-256。 |
| `schema_size_bytes` | `bigint` | 是 | Raw OpenAPI 字节数。 |
| `schema_metadata` | `jsonb` | 是 | content type、etag、bucket、parser version 等元数据。 |
| `changelog` | `text` | 否 | 提交说明。 |
| `source_git_commit_id` | `text` | 否 | 用户应用或代码仓库的 Git commit ID。不是 Vdoc 自身 Git commit，也不是 Vdoc 契约分支。发布时复制到版本。 |
| `source_type` | `smallint` | 是 | 草稿来源码，1 web_upload、2 mcp_upload、3 promote。 |
| `source_branch_id` | `uuid` | 否 | promote 时的源分支。 |
| `source_version_id` | `uuid` | 否 | promote 时的源分支最新已发布版本。 |
| `base_version_id` | `uuid` | 否 | 目标分支基线版本，用于 diff preview。 |
| `diff_preview_json` | `jsonb` | 否 | 小型 diff 预览摘要。 |
| `diff_preview_object_key` | `text` | 否 | 大型 diff 预览 RustFS object key。 |
| `review_comment` | `text` | 否 | 审核意见。 |
| `created_by_actor_type` | `smallint` | 是 | 创建者类型码，1 user、2 mcp_token、3 system。 |
| `created_by_user_id` | `uuid` | 是 | 草稿归属用户。MCP 草稿写 Token 所属用户。 |
| `created_by_token_id` | `uuid` | 否 | MCP 创建时的 Token。 |
| `submitted_at` | `timestamptz` | 否 | 提交审核时间。 |
| `reviewed_by` | `uuid` | 否 | 审核人。 |
| `reviewed_at` | `timestamptz` | 否 | 审核时间。 |
| `published_version_id` | `uuid` | 否 | 发布后生成的版本。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Service 外键 | `FOREIGN KEY (service_id) REFERENCES api_services(id)` |
| Branch 外键 | `FOREIGN KEY (branch_id) REFERENCES api_contract_branches(id)` |
| 源 Branch 外键 | `FOREIGN KEY (source_branch_id) REFERENCES api_contract_branches(id)` |
| 源版本外键 | `FOREIGN KEY (source_version_id) REFERENCES api_contract_versions(id)` |
| 基线版本外键 | `FOREIGN KEY (base_version_id) REFERENCES api_contract_versions(id)` |
| 创建用户外键 | `FOREIGN KEY (created_by_user_id) REFERENCES users(id)` |
| 创建 Token 外键 | `FOREIGN KEY (created_by_token_id) REFERENCES mcp_tokens(id)` |
| 审核人外键 | `FOREIGN KEY (reviewed_by) REFERENCES users(id)` |
| 已发布版本外键 | `FOREIGN KEY (published_version_id) REFERENCES api_contract_versions(id)` |
| 状态检查 | `CHECK (status IN (1, 2, 3, 4, 5))` |
| 格式检查 | `CHECK (schema_format IN (1, 2))` |
| 来源检查 | `CHECK (source_type IN (1, 2, 3))` |
| Actor 检查 | `CHECK (created_by_actor_type IN (1, 2, 3))` |
| Active 草稿唯一 | `UNIQUE (service_id, branch_id, version_name) WHERE status IN (1, 2, 3) AND deleted_at IS NULL` |
| Promote 字段检查 | `CHECK (source_type <> 3 OR (source_branch_id IS NOT NULL AND source_version_id IS NOT NULL))` |
| 查询索引 | `INDEX (service_id, branch_id, status)`、`INDEX (created_by_user_id, status)`、`INDEX (base_version_id)` |

### 6.8 `api_contract_versions`

保存已发布的不可变契约版本。发布后禁止修改 OpenAPI 对象引用、hash、Endpoint 解析结果和 Diff 基线数据。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `service_id` | `uuid` | 是 | 所属 Service。 |
| `branch_id` | `uuid` | 是 | 所属分支。 |
| `version_name` | `text` | 是 | 分支内版本名。 |
| `version_no` | `integer` | 是 | 分支内递增序号，便于排序。 |
| `status` | `smallint` | 是 | 固定 1 published。 |
| `source_draft_id` | `uuid` | 是 | 来源草稿。 |
| `source_type` | `smallint` | 是 | 来源码，1 web_upload、2 mcp_upload、3 promote。 |
| `source_branch_id` | `uuid` | 否 | promote 来源分支。 |
| `source_version_id` | `uuid` | 否 | promote 来源版本。 |
| `base_version_id` | `uuid` | 否 | 发布时目标分支基线版本。 |
| `schema_format` | `smallint` | 是 | Schema 格式码，1 openapi-3.0、2 openapi-3.1。 |
| `raw_schema_object_key` | `text` | 是 | RustFS Raw OpenAPI object key。 |
| `normalized_schema_object_key` | `text` | 是 | RustFS Normalized OpenAPI object key。 |
| `raw_schema_hash` | `text` | 是 | Raw OpenAPI SHA-256。 |
| `normalized_schema_hash` | `text` | 是 | Normalized OpenAPI SHA-256。 |
| `schema_size_bytes` | `bigint` | 是 | Raw OpenAPI 字节数。 |
| `schema_metadata` | `jsonb` | 是 | content type、etag、bucket、parser version 等元数据。 |
| `changelog` | `text` | 否 | 版本说明。 |
| `source_git_commit_id` | `text` | 否 | 从来源草稿复制的用户应用或代码仓库 Git commit ID。不是 Vdoc 自身 Git commit，也不是 Vdoc 契约分支。 |
| `endpoint_count` | `integer` | 是 | Endpoint 数量。 |
| `published_by` | `uuid` | 是 | 审核发布人。 |
| `published_at` | `timestamptz` | 是 | 发布时间。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间，发布后不再变化。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Service 外键 | `FOREIGN KEY (service_id) REFERENCES api_services(id)` |
| Branch 外键 | `FOREIGN KEY (branch_id) REFERENCES api_contract_branches(id)` |
| 来源草稿唯一 | `UNIQUE (source_draft_id)` |
| 发布人外键 | `FOREIGN KEY (published_by) REFERENCES users(id)` |
| 状态检查 | `CHECK (status = 1)` |
| 格式检查 | `CHECK (schema_format IN (1, 2))` |
| 来源检查 | `CHECK (source_type IN (1, 2, 3))` |
| 分支内版本名唯一 | `UNIQUE (service_id, branch_id, version_name)` |
| 分支内序号唯一 | `UNIQUE (service_id, branch_id, version_no)` |
| Hash 查询索引 | `INDEX (service_id, branch_id, normalized_schema_hash)` |
| 列表索引 | `INDEX (service_id, branch_id, published_at DESC)` |

### 6.9 `api_endpoints`

保存从某个契约版本解析出的 Endpoint 索引。用于列表、搜索、Diff 前置比较和 MCP 查询。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `contract_version_id` | `uuid` | 是 | 所属契约版本。 |
| `service_id` | `uuid` | 是 | 冗余 Service ID，便于查询。 |
| `branch_id` | `uuid` | 是 | 冗余 Branch ID，便于按环境查询。 |
| `method` | `smallint` | 是 | HTTP method 码，1 GET、2 POST、3 PUT、4 PATCH、5 DELETE、6 OPTIONS、7 HEAD、8 TRACE。 |
| `path` | `text` | 是 | OpenAPI path。 |
| `operation_id` | `text` | 否 | OpenAPI operationId。 |
| `summary` | `text` | 否 | 摘要。 |
| `description` | `text` | 否 | 描述。 |
| `tags` | `text[]` | 是 | 标签数组，默认空数组。 |
| `deprecated` | `boolean` | 是 | 是否废弃。 |
| `request_hash` | `text` | 是 | 请求侧结构 hash。 |
| `response_hash` | `text` | 是 | 响应侧结构 hash。 |
| `security_hash` | `text` | 否 | 安全要求 hash。 |
| `endpoint_hash` | `text` | 是 | Endpoint 级整体 hash。 |
| `sort_order` | `integer` | 是 | 展示排序。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间，版本发布后不再变化。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| 版本外键 | `FOREIGN KEY (contract_version_id) REFERENCES api_contract_versions(id)` |
| Service 外键 | `FOREIGN KEY (service_id) REFERENCES api_services(id)` |
| Branch 外键 | `FOREIGN KEY (branch_id) REFERENCES api_contract_branches(id)` |
| Method 检查 | `CHECK (method IN (1, 2, 3, 4, 5, 6, 7, 8))` |
| Endpoint 唯一 | `UNIQUE (contract_version_id, method, path)` |
| 列表索引 | `INDEX (contract_version_id, sort_order)` |
| 路径查询索引 | `INDEX (service_id, branch_id, method, path)` |
| Operation 查询索引 | `INDEX (contract_version_id, operation_id)` |
| Tags 查询索引 | `GIN INDEX (tags)` |
| Hash 查询索引 | `INDEX (contract_version_id, endpoint_hash)` |

### 6.10 `api_endpoint_details`

保存 Endpoint 的结构化详情。这里存的是解析后的必要 JSONB，不是完整 Raw OpenAPI。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `endpoint_id` | `uuid` | 是 | 所属 Endpoint。 |
| `parameters_json` | `jsonb` | 是 | path、query、header、cookie 参数。 |
| `request_body_json` | `jsonb` | 否 | 请求体 schema 和 content type。 |
| `responses_json` | `jsonb` | 是 | 响应状态码、content type 和 schema。 |
| `security_json` | `jsonb` | 否 | Endpoint 安全要求。 |
| `servers_json` | `jsonb` | 否 | OpenAPI servers 信息。 |
| `normalized_operation_json` | `jsonb` | 是 | 归一化后的 operation 结构，用于 MCP 返回和 Diff。 |
| `schema_refs_json` | `jsonb` | 否 | 引用到的 components 信息摘要。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间，版本发布后不再变化。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Endpoint 外键 | `FOREIGN KEY (endpoint_id) REFERENCES api_endpoints(id)` |
| Endpoint 唯一 | `UNIQUE (endpoint_id)` |
| JSON 查询索引 | 可按实际查询增加 `GIN INDEX (normalized_operation_json)` |

### 6.11 `api_version_diffs`

保存两个已发布版本之间的 Diff 摘要。跨分支比较和同分支比较都使用此表。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `service_id` | `uuid` | 是 | 所属 Service。 |
| `from_branch_id` | `uuid` | 是 | 来源版本所在分支。 |
| `to_branch_id` | `uuid` | 是 | 目标版本所在分支。 |
| `from_version_id` | `uuid` | 是 | 对比起点版本。 |
| `to_version_id` | `uuid` | 是 | 对比终点版本。 |
| `diff_status` | `smallint` | 是 | Diff 状态码，1 pending、2 running、3 succeeded、4 failed。 |
| `diff_object_key` | `text` | 否 | 大型完整 Diff 快照 RustFS object key。 |
| `diff_hash` | `text` | 否 | 完整 Diff SHA-256。 |
| `diff_summary_json` | `jsonb` | 是 | 机器可读摘要。 |
| `breaking_changes_json` | `jsonb` | 是 | breaking changes 摘要。 |
| `added_count` | `integer` | 是 | 新增 Endpoint 数量。 |
| `modified_count` | `integer` | 是 | 修改 Endpoint 数量。 |
| `removed_count` | `integer` | 是 | 删除 Endpoint 数量。 |
| `breaking_count` | `integer` | 是 | Breaking Change 数量。 |
| `summary_text` | `text` | 否 | 面向人类的摘要。 |
| `error_message` | `text` | 否 | Diff 失败原因。 |
| `generated_at` | `timestamptz` | 否 | Diff 完成时间。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Service 外键 | `FOREIGN KEY (service_id) REFERENCES api_services(id)` |
| 分支外键 | `FOREIGN KEY (from_branch_id) REFERENCES api_contract_branches(id)`、`FOREIGN KEY (to_branch_id) REFERENCES api_contract_branches(id)` |
| 版本外键 | `FOREIGN KEY (from_version_id) REFERENCES api_contract_versions(id)`、`FOREIGN KEY (to_version_id) REFERENCES api_contract_versions(id)` |
| 状态检查 | `CHECK (diff_status IN (1, 2, 3, 4))` |
| 版本不同 | `CHECK (from_version_id <> to_version_id)` |
| Diff 唯一 | `UNIQUE (from_version_id, to_version_id)` |
| 查询索引 | `INDEX (service_id, to_branch_id, created_at DESC)`、`INDEX (diff_status)` |

### 6.12 `api_diff_items`

保存机器可读 Diff 条目，用于页面筛选、MCP `compare_api_versions` 和 `get_change_summary`。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `diff_id` | `uuid` | 是 | 所属 Diff。 |
| `endpoint_id` | `uuid` | 否 | 目标版本中对应 Endpoint。删除 Endpoint 时可为空。 |
| `change_type` | `smallint` | 是 | 变更类型码，见 Diff change type code map。 |
| `severity` | `smallint` | 是 | 严重度码，1 info、2 warning、3 breaking。 |
| `method` | `smallint` | 否 | HTTP method 码，见 Endpoint method code map。 |
| `path` | `text` | 否 | Endpoint path。 |
| `operation_id` | `text` | 否 | operationId。 |
| `location` | `text` | 否 | 变更位置，例如 `response.200.data.name`。 |
| `old_value` | `jsonb` | 否 | 旧值。 |
| `new_value` | `jsonb` | 否 | 新值。 |
| `message` | `text` | 是 | 人类可读说明。 |
| `frontend_impact` | `text` | 否 | 面向前端的影响说明。 |
| `is_breaking` | `boolean` | 是 | 是否破坏兼容。 |
| `sort_order` | `integer` | 是 | 展示排序。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Diff 外键 | `FOREIGN KEY (diff_id) REFERENCES api_version_diffs(id)` |
| Endpoint 外键 | `FOREIGN KEY (endpoint_id) REFERENCES api_endpoints(id)` |
| Severity 检查 | `CHECK (severity IN (1, 2, 3))` |
| Breaking 一致性 | `CHECK (severity <> 3 OR is_breaking = true)` |
| 列表索引 | `INDEX (diff_id, sort_order)` |
| 严重度索引 | `INDEX (diff_id, severity)` |
| Endpoint 索引 | `INDEX (diff_id, method, path)` |
| 类型索引 | `INDEX (change_type)` |

v0.1 变更类型使用第 3 节的整数码，下表补充说明。

| Code | 名称 | 说明 | 默认严重度码 |
|---|---|---|---|
| 1 | endpoint_added | 新增 Endpoint | 1 |
| 2 | endpoint_removed | 删除 Endpoint | 3 |
| 3 | endpoint_modified | Endpoint 结构变化 | 2 |
| 4 | request_parameter_added | 新增请求参数 | 1 或 3 |
| 5 | request_parameter_removed | 删除请求参数 | 2 |
| 6 | request_parameter_changed | 请求参数类型、位置或必填状态变化 | 2 或 3 |
| 7 | request_body_changed | 请求体变化 | 2 或 3 |
| 8 | response_field_added | 响应字段新增 | 1 |
| 9 | response_field_removed | 响应字段删除 | 3 |
| 10 | response_field_changed | 响应字段类型、格式或必填状态变化 | 2 或 3 |
| 11 | security_changed | 鉴权要求变化 | 2 或 3 |
| 12 | deprecated_changed | Deprecated 状态变化 | 1 或 2 |
| 13 | enum_value_removed | enum 可选值删除 | 3 |

### 6.13 `mcp_tokens`

保存用户绑定的 MCP Token。此表不包含 `project_id`，v0.1 不设计项目绑定 Token。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `user_id` | `uuid` | 是 | Token 所属用户。 |
| `name` | `text` | 是 | Token 名称。 |
| `token_hash` | `text` | 是 | Token 哈希，用于调用鉴权匹配。 |
| `token_ciphertext` | `bytea` | 是 | 加密后的完整 Token，用于后台向所属用户展示和复制。 |
| `cipher_kid` | `text` | 是 | 加密密钥版本。 |
| `scopes` | `smallint[]` | 是 | MCP scope 码数组，1 api:read、2 api:draft。 |
| `status` | `smallint` | 是 | Token 状态码，1 active、2 revoked、3 expired。 |
| `expires_at` | `timestamptz` | 否 | 过期时间。 |
| `last_used_at` | `timestamptz` | 否 | 最近使用时间。 |
| `revoked_at` | `timestamptz` | 否 | 废弃时间。 |
| `revoked_by` | `uuid` | 否 | 废弃操作人。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 更新时间。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| User 外键 | `FOREIGN KEY (user_id) REFERENCES users(id)` |
| 废弃人外键 | `FOREIGN KEY (revoked_by) REFERENCES users(id)` |
| Token 哈希唯一 | `UNIQUE (token_hash)` |
| 状态检查 | `CHECK (status IN (1, 2, 3))` |
| 废弃字段检查 | `CHECK (status <> 2 OR revoked_at IS NOT NULL)` |
| Scope 检查 | 应保证 `scopes` 只包含 1、2。可用应用校验或 PostgreSQL 函数约束。 |
| 查询索引 | `INDEX (user_id, status)`、`INDEX (expires_at)`、`INDEX (last_used_at DESC)` |

鉴权规则固定如下。

| 步骤 | 规则 |
|---|---|
| Token 匹配 | 使用请求 Token 计算 hash，按 `token_hash` 查询 active Token。 |
| Token 状态 | `status = 1`，且 `expires_at` 为空或晚于当前时间。 |
| 用户状态 | `users.status = 1`。 |
| 项目权限 | 查询目标 Project 的 `project_members`，SuperAdmin 跳过项目成员限制。 |
| 有效权限 | token scopes 与项目角色权限取交集。 |
| 写入限制 | MCP 可创建、更新、提交草稿，但不能直接发布 `api_contract_versions`。 |
| 审计 | Token 使用、草稿写入、Token 查看、复制和废弃都写 `audit_logs`。 |

### 6.14 `audit_logs`

保存安全敏感操作和关键业务操作。审计日志只追加，不更新，不软删。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键。 |
| `actor_type` | `smallint` | 是 | Actor 类型码，1 user、2 mcp_token、3 system。 |
| `actor_user_id` | `uuid` | 否 | 操作用户。MCP 调用写 Token 所属用户。 |
| `actor_token_id` | `uuid` | 否 | MCP Token。 |
| `action` | `text` | 是 | 操作名，例如 `draft.create`、`draft.submit`、`version.publish`、`mcp_token.reveal`。 |
| `resource_type` | `text` | 是 | 资源类型。 |
| `resource_id` | `uuid` | 否 | 资源 ID。 |
| `project_id` | `uuid` | 否 | 关联 Project。 |
| `service_id` | `uuid` | 否 | 关联 Service。 |
| `metadata` | `jsonb` | 是 | 请求摘要、旧值新值摘要、IP 以外的审计上下文。 |
| `ip_address` | `inet` | 否 | 请求 IP。 |
| `user_agent` | `text` | 否 | User Agent。 |
| `request_id` | `text` | 否 | trace_id 或请求 ID。 |
| `created_at` | `timestamptz` | 是 | 创建时间。 |
| `updated_at` | `timestamptz` | 是 | 与创建时间相同，保留通用列。 |

| 约束和索引 | 定义 |
|---|---|
| 主键 | `PRIMARY KEY (id)` |
| Actor 检查 | `CHECK (actor_type IN (1, 2, 3))` |
| User 外键 | `FOREIGN KEY (actor_user_id) REFERENCES users(id)` |
| Token 外键 | `FOREIGN KEY (actor_token_id) REFERENCES mcp_tokens(id)` |
| Project 外键 | `FOREIGN KEY (project_id) REFERENCES projects(id)` |
| Service 外键 | `FOREIGN KEY (service_id) REFERENCES api_services(id)` |
| Actor 索引 | `INDEX (actor_type, actor_user_id, created_at DESC)` |
| 资源索引 | `INDEX (resource_type, resource_id, created_at DESC)` |
| 项目索引 | `INDEX (project_id, created_at DESC)` |
| 操作索引 | `INDEX (action, created_at DESC)` |
| 请求索引 | `INDEX (request_id)` |

## 7. 存储对象边界

RustFS 对象 key 的具体格式可以在实现时统一封装，但必须满足可追溯、可审计、可从数据库记录定位对象。

| 对象 | 建议 key 结构 | 数据库字段 |
|---|---|---|
| 草稿 Raw OpenAPI | `projects/{project_id}/services/{service_id}/branches/{branch_name}/drafts/{draft_id}/raw.{json|yaml}` | `api_contract_drafts.raw_schema_object_key` |
| 草稿 Normalized OpenAPI | `projects/{project_id}/services/{service_id}/branches/{branch_name}/drafts/{draft_id}/normalized.json` | `api_contract_drafts.normalized_schema_object_key` |
| 版本 Raw OpenAPI | `projects/{project_id}/services/{service_id}/branches/{branch_name}/versions/{version_id}/raw.{json|yaml}` | `api_contract_versions.raw_schema_object_key` |
| 版本 Normalized OpenAPI | `projects/{project_id}/services/{service_id}/branches/{branch_name}/versions/{version_id}/normalized.json` | `api_contract_versions.normalized_schema_object_key` |
| 草稿 Diff Preview | `projects/{project_id}/services/{service_id}/branches/{branch_name}/drafts/{draft_id}/diff-preview.json` | `api_contract_drafts.diff_preview_object_key` |
| 完整 Diff 快照 | `projects/{project_id}/services/{service_id}/diffs/{diff_id}/full.json` | `api_version_diffs.diff_object_key` |

PostgreSQL 不保存完整 Raw OpenAPI、完整 Normalized OpenAPI 或大型 Diff 快照。PostgreSQL 保存可直接查询的结构化产物，包括 endpoint indexes、endpoint details、diff summary 和 diff items。

## 8. 核心写入流程

### 8.1 OpenAPI 草稿创建

| 步骤 | 写入内容 |
|---|---|
| 接收 OpenAPI | 校验 OpenAPI 3.0 或 3.1，限制文件大小。 |
| 保存 Raw | 写入 RustFS，得到 `raw_schema_object_key` 和 `raw_schema_hash`。 |
| Normalize | 生成稳定排序后的 Normalized OpenAPI。 |
| 保存 Normalized | 写入 RustFS，得到 `normalized_schema_object_key` 和 `normalized_schema_hash`。 |
| 创建草稿 | 写入 `api_contract_drafts`，状态码为 1 draft 或 2 submitted。普通上传必须写 `branch_id`，如果请求带 `source_git_commit_id` 则一并保存。 |
| 生成预览 | 对比目标分支 `base_version_id`，写 `diff_preview_json`，大预览写 RustFS。 |
| 写审计 | 写 `audit_logs`，记录 Web 用户或 MCP Token。 |

### 8.2 草稿审核发布

| 步骤 | 写入内容 |
|---|---|
| 提交审核 | `api_contract_drafts.status = 2`，写 `submitted_at`。 |
| 要求修改 | `status = 3`，写 `review_comment`、`reviewed_by`、`reviewed_at`。 |
| 拒绝 | `status = 4`，写审核信息。 |
| 通过发布 | 创建 `api_contract_versions`，状态固定 1，并从草稿复制 `source_git_commit_id`。 |
| 解析 Endpoint | 写 `api_endpoints` 和 `api_endpoint_details`。 |
| 更新草稿 | `api_contract_drafts.status = 5`，写 `published_version_id`。 |
| 生成 Diff | 对比同分支上一个 published 版本，写 `api_version_diffs` 和 `api_diff_items`。 |
| 写审计 | 记录 `version.publish`。 |

### 8.3 Promote 到目标分支

| 步骤 | 写入内容 |
|---|---|
| 找源版本 | 从 `source_branch_id` 找最新 `published` 版本。 |
| 找目标基线 | 从目标 `branch_id` 找最新 `published` 版本。 |
| 创建草稿 | `api_contract_drafts.branch_id` 写目标分支，`source_branch_id`、`source_version_id`、`base_version_id` 写入来源和基线。 |
| Diff Preview | 以目标基线对比源版本生成预览。 |
| 审核发布 | 复用普通审核发布流程，在目标分支创建新 `api_contract_versions`。 |

## 9. 非目标

| 非目标 | 说明 |
|---|---|
| MySQL | v0.1 不使用 MySQL。 |
| Project 绑定 MCP Token | v0.1 MCP Token 不含 `project_id`，按用户绑定和权限交集判断。 |
| 复杂 Git 合并模型 | v0.1 不建 merge commit、rebase、conflict resolution 等表。 |
| 邀请工作流 | v0.1 不做邀请、待接受或加入确认流程，项目成员从现有系统用户手动添加。 |
| 多级审批流 | v0.1 只支持草稿提交、修改请求、拒绝和发布。 |
| 多协议导入 | v0.1 只面向 OpenAPI 3.x。 |

## 10. 评审检查清单

| 检查项 | 结果 |
|---|---|
| 是否覆盖所有 MVP 表 | 已覆盖 `users`、`teams`、`projects`、`project_members`、`api_services`、`api_contract_branches`、`api_contract_drafts`、`api_contract_versions`、`api_endpoints`、`api_endpoint_details`、`api_version_diffs`、`api_diff_items`、`mcp_tokens`、`audit_logs`。 |
| 是否明确 PostgreSQL 和 RustFS 边界 | 已明确 PostgreSQL 存结构化数据，RustFS 存 Raw、Normalized 和大型 Diff 快照。 |
| 是否使用整数码保存有限集合字段 | 已明确状态、角色、类型、method、severity、scope 等 DB 字段使用从 1 开始的 `smallint` 或 `smallint[]`。 |
| 是否支持分支和环境 | 已明确 `dev`、`test`、`prod`、`feature/*`、受保护 `prod` 和 `(service_id, name)` 唯一。 |
| 是否支持 branch-aware 版本唯一 | 已明确 `UNIQUE (service_id, branch_id, version_name)`。 |
| 是否支持 promote | 已明确目标草稿字段 `branch_id`、`source_branch_id`、`source_version_id`、`base_version_id` 和 diff preview。 |
| 是否记录用户代码 Git commit | 已明确 `api_contract_drafts.source_git_commit_id`，发布时复制到 `api_contract_versions.source_git_commit_id`。 |
| 是否移除邀请 MVP | 已明确没有 `invited` 状态、`invited_by` 或 `joined_at`，成员由后台或管理员从现有用户手动添加。 |
| 是否避免 Project 绑定 MCP Token | 已明确 `mcp_tokens` 不含 `project_id`。 |
| 是否明确 MCP 权限模型 | 已明确 token scopes 与用户 Project 角色权限取交集。 |
