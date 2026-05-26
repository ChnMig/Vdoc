# Vdoc v0.1 PostgreSQL 数据库表设计

本文档定义 Vdoc v0.1 MVP 的 Project Document 持久化设计。Project 下直接管理多类型文档，文档类型至少包括 OpenAPI 和 Markdown。PostgreSQL 保存结构化数据、索引和对象引用；RustFS 通过 S3-compatible API 保存 Raw、Normalized、Stable 快照以及大型 Diff 快照。

## 1. 设计边界

| 项目 | v0.1 决策 |
|---|---|
| 结构化数据 | 使用 PostgreSQL 保存用户、团队、项目、项目成员、文档、文档分支、草稿、版本、Endpoint 索引、Endpoint 详情、Diff 摘要、Diff Items、MCP Token 和审计日志。 |
| 对象存储 | 使用 RustFS/S3-compatible API 保存 Raw、Normalized、Stable 内容快照和大型 Diff。 |
| 数据库中的对象引用 | PostgreSQL 只保存 object key、hash、content type、size、etag、metadata 等引用和校验数据。 |
| Endpoint 查询 | OpenAPI Endpoint 列表走 `api_endpoints`，详情走 `api_endpoint_details`，不读取 Raw 快照。 |
| Diff 查询 | 摘要和机器可读条目存 PostgreSQL，大型完整 Diff 快照存 RustFS。 |
| MCP Token | v0.1 只做用户绑定 Token，不做 Project 绑定 Token。 |
| Merge 和 Promote | v0.1 建模跨分支提升草稿，不设计复杂 Git 风格 merge、rebase、conflict 表。 |

## 2. 通用列和命名约定

所有业务表使用 PostgreSQL 原生类型，字段使用 `snake_case`。主键使用 UUID，建议由数据库端 `gen_random_uuid()` 生成。

| 列名 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | `uuid` | 是 | 主键，默认 `gen_random_uuid()`。 |
| `created_at` | `timestamptz` | 是 | 创建时间，默认 `now()`。 |
| `updated_at` | `timestamptz` | 是 | 更新时间，默认 `now()`。 |
| `deleted_at` | `timestamptz` | 否 | 软删除时间。适用于用户、团队、项目、成员、文档、分支、草稿和 Token。不可变版本、Diff 和审计日志默认不软删。 |

唯一约束如果需要忽略软删除记录，使用 partial unique index，例如 `WHERE deleted_at IS NULL`。

## 3. 状态值、类型码和权限值

有限集合字段都使用从 1 开始的整数码，不使用 text enum。共享业务码由 `common/vdoc` 维护，DB 表名和 check 约束常量由 `db/pgdb/vdoc/constants.go` 维护。

| 对象 | 字段 | 类型 | Code map |
|---|---|---|---|
| `users` | `status` | `smallint` | 1 active、2 disabled |
| `projects` | `status` | `smallint` | 1 active、2 archived |
| `project_members` | `role` | `smallint` | 1 reader、2 writer、3 admin |
| `project_members` | `status` | `smallint` | 1 active、2 disabled |
| `documents` | `document_type` | `smallint` | 1 openapi、2 markdown |
| `documents` | `status` | `smallint` | 1 active、2 archived |
| `document_branches` | `kind` | `smallint` | 1 environment、2 feature |
| `document_branches` | `status` | `smallint` | 1 active、2 archived |
| `document_drafts` | `status` | `smallint` | 1 draft、2 submitted、3 changes_requested、4 rejected、5 published |
| `document_versions` | `status` | `smallint` | 1 published |
| `document_drafts`、`document_versions` | `document_format` | `smallint` | 1 openapi-3.0、2 openapi-3.1、3 markdown |
| `document_drafts`、`document_versions` | `source_type` | `smallint` | 1 web_upload、2 mcp_upload、3 promote、4 web_edit |
| `document_version_diffs` | `diff_status` | `smallint` | 1 pending、2 running、3 succeeded、4 failed |
| `document_diff_items` | `severity` | `smallint` | 1 info、2 warning、3 breaking |

## 4. 表清单

| 表 | 说明 |
|---|---|
| `schema_migrations` | 已应用 migration 记录。 |
| `users` | 用户账户。 |
| `teams` | 团队。 |
| `projects` | Project 协作边界。 |
| `project_members` | Project 成员和角色。 |
| `documents` | Project 下的文档元数据。字段包括 `name`、`document_type`、`relative_path`、`description`、`status`、`created_by`。不存储路径之外的第二套名称身份。 |
| `document_branches` | 文档环境/功能分支。 |
| `document_drafts` | 待审核文档草稿，快照 `relative_path` 以保证审计可追溯。 |
| `document_versions` | 已发布不可变版本，快照 `relative_path` 以保证历史稳定。 |
| `api_endpoints` | OpenAPI 文档版本解析出的 Endpoint 索引。 |
| `api_endpoint_details` | Endpoint 参数、请求体、响应、安全和规范化 operation JSON。 |
| `document_version_diffs` | 文档版本 Diff 摘要和大对象引用。 |
| `document_diff_items` | 机器可读 Diff 条目。 |
| `mcp_tokens` | MCP Token 哈希、密文和状态。 |
| `audit_logs` | 审计日志。 |
| `vdoc_schema_objects` | RustFS/S3 对象引用表，记录 raw、normalized、stable 和 diff snapshot 的对象 key/hash/元数据。 |

## 5. 关键约束

| 约束 | 说明 |
|---|---|
| `documents_project_name_active_uidx` | 同一 Project 下 active 文档名称唯一。 |
| `documents_project_relative_path_active_uidx` | 同一 Project 下 active 文档 `relative_path` 唯一。 |
| `document_branches_document_name_uidx` | 同一文档下分支名唯一。 |
| `document_branches_default_uidx` | 同一文档只允许一个 active 默认分支。 |
| `document_drafts_active_version_uidx` | 同一文档、分支、版本名只允许一个 active 草稿。 |
| `document_versions_document_branch_version_name_uidx` | 同一文档、分支、版本名唯一。 |
| `document_versions_document_branch_version_no_uidx` | 同一文档、分支、版本序号唯一。 |
| `api_endpoints_version_method_path_uidx` | 同一文档版本下 method + path 唯一。 |
| `document_version_diffs_versions_uidx` | 同一 from/to 版本组合只生成一条 Diff 摘要。 |
| `document_diff_items_breaking_consistency_check` | breaking severity 必须对应 `is_breaking = true`。 |

## 6. 对象快照字段

`document_drafts` 和 `document_versions` 均保存：

| 字段 | 说明 |
|---|---|
| `raw_schema_object_key`、`raw_schema_hash` | 原始上传内容快照。Markdown 使用原始 Markdown 文本。 |
| `normalized_schema_object_key`、`normalized_schema_hash` | OpenAPI 规范化内容；Markdown 可与 raw 或 stable 策略保持一致。 |
| `stable_schema_object_key`、`stable_schema_hash` | 审核/发布稳定快照，供后续纯文档 diff 或 canonical 内容查询使用。 |
| `source_git_commit_id` | 草稿或版本来源 Git commit，可为空。 |
| `relative_path` | 文档路径快照，保证文档后续重命名后历史记录仍可审计。 |

## 7. Endpoint 与 Diff 关系

`api_endpoints.document_version_id` 指向 `document_versions.id`，`api_endpoints.document_id` 指向 `documents.id`。Endpoint 表只对 OpenAPI 文档版本有数据，Markdown 文档版本不写 Endpoint 索引。

`document_version_diffs` 使用 `from_version_id`、`to_version_id` 指向 `document_versions.id`。OpenAPI Diff 可填充 Endpoint 级语义条目；Markdown Diff 可保存纯文件 Diff 摘要和对象引用。
