# Vdoc

语言: [English](README.md) | [简体中文](README.zh-CN.md)

面向 AI 协作开发的文档协作中心，支持 OpenAPI API 文档、Markdown 纯文档、语义 Diff 和 MCP。

Vdoc 帮助使用 AI/Vibe Coding 的团队同步接口变更、Markdown 项目知识、前端对接代码和 AI Agent 上下文。

## Vdoc 是什么？

Vdoc 是一个面向快速迭代团队的文档协作平台。Project 直接管理多类型 Document，包括 OpenAPI API 文档和 Markdown 纯文档。Vdoc 把每次发布的文档保存为不可变版本，对 OpenAPI 做语义 Diff，对 Markdown 做文本 Diff，并把已审核的文档知识同时暴露给人和 AI Agent。

Vdoc 的目标不是再做一个 Swagger UI，而是解决 AI 辅助开发里经常断掉的文档同步链路：

```text
后端上传或 AI 通过 MCP 提交 OpenAPI 或 Markdown 草稿
        -> 人工审核后 Vdoc 创建不可变版本
        -> Vdoc 在适用时解析 OpenAPI Endpoint
        -> Vdoc 计算 OpenAPI 语义 Diff 或 Markdown 文本 Diff
        -> Vdoc 在适用时标记 breaking changes
        -> 前端或 AI 查询变更、接口详情或 Markdown 内容
        -> 前端带着上下文更新对接代码和项目知识
```

## 当前状态

这个仓库当前是 Vdoc v0.1 的 Go/Gin 后端。

API 文档：

- 人类可读指南：[docs/api/API.md](docs/api/API.md)
- 机器可读 OpenAPI 规格：[docs/api/openapi.yaml](docs/api/openapi.yaml)

v0.1 已经实现：

- `/api/v1` 版本化路由树
- 公开注册、登录和私有 JWT 路由
- SuperAdmin 用户生命周期、Team、Project、Member、Document 和 Document Branch
- OpenAPI 和 Markdown 草稿上传、审核和不可变版本发布
- 草稿和已发布版本的 raw、normalized、stable 内容查询
- Endpoint 索引查询、OpenAPI 语义 Diff 摘要和 Markdown 文件 Diff
- MCP Token 生命周期和 JSON-RPC MCP 查询、草稿 tools
- 带 `trace_id` 和 `timestamp` 的统一 JSON 响应包裹
- 请求追踪、结构化访问日志、panic recovery、CORS、安全响应头、请求体大小限制、限流中间件
- 基于 Viper 的配置加载和 `VDOC_` 环境变量覆盖
- 用于构建、测试、检查、格式化和跨平台打包的 Makefile

v0.1 不包含：

- 直接发布类 MCP tools
- 代码生成和前端对接辅助

## 产品概念

| 概念 | 含义 |
|---|---|
| Team | 团队协作边界。 |
| Project | 团队下的产品或应用。 |
| Document | Project 下的多类型文档。v0.1 支持 OpenAPI API 文档和 Markdown 纯文档。 |
| Document Type | `1` 表示 OpenAPI，`2` 表示 Markdown。 |
| Relative Path | 文档在 Project 内的路径身份，例如 `apis/petstore.yaml` 或 `docs/runbook.md`。 |
| Document Branch / Environment | 文档发布轨道，例如 `dev`、`test`、`prod` 或 `feature/*`，其中 `prod` 默认受保护。 |
| Document Version | 某个多类型文档的一次不可变快照。 |
| Endpoint Index | 从 OpenAPI 解析出的结构化索引，包括路径、方法、参数、请求体、响应、标签和 operationId。 |
| Semantic Diff | 面向接口契约的版本比较，而不是原始文本 diff。 |
| Breaking Change | 可能破坏前端消费方的变更，例如字段删除、类型变化、新增必填参数、接口删除。 |
| MCP Token | 用户绑定的 AI 工具访问 token。创建时返回一次性可复制 token 值，后续列表和详情响应会脱敏；权限由 token scopes 与用户在目标 Project 的角色共同决定。 |

## MVP 使用流程

1. SuperAdmin 创建系统成员、Team 和 Project。
2. SuperAdmin 指定 Project Admin。
3. Project Admin 从现有系统用户中手动添加成员并分配项目级角色。
4. Project Admin 创建 OpenAPI 或 Markdown Document，并填写 `relative_path`。
5. Writer 通过 Web API 上传，或 AI 通过 MCP 向目标文档分支提交草稿。
6. Vdoc 校验并保存 raw 内容，以及 normalized 或 stable 快照。
7. Project Admin 人工审核通过后，Vdoc 创建不可变 Document Version。
8. Vdoc 为 OpenAPI 文档解析 Endpoint Index，用于快速查询和展示。
9. Vdoc 将新版本与上一版本做 OpenAPI 语义 Diff 或 Markdown 文本 Diff。
10. Vdoc 保存变更摘要和适用的 breaking-change 列表。
11. 前端开发和 AI Agent 查询接口详情、Markdown 内容、版本差异和摘要。

## v0.1 范围

当前 v0.1 后端已经实现：

- 系统级 `SuperAdmin`；项目级角色：`Reader`、`Writer`、`Admin`，其中 Writer 只能提交草稿，Admin 负责审核发布
- 通过 Web API 上传 OpenAPI 3.x 和 Markdown，AI 可通过 MCP 提交和更新草稿
- MVP 不做邀请流程，项目成员从现有系统用户手动添加
- 文档分支和环境，支持 `dev`、`test`、受保护 `prod`、可选 `feature/*`，以及 promote 到目标分支草稿
- 每个多类型文档下的不可变版本
- OpenAPI 和 Markdown 草稿人工审核后发布
- 接口列表和接口详情查询
- 版本比较、OpenAPI 语义 Diff 和 Markdown 文件 Diff
- Breaking-change 摘要
- 仅提供 MCP 查询和草稿 tools；版本发布必须走人工审核

第一版暂不做：

- 复杂组织级 RBAC
- GraphQL、gRPC、Postman、YApi、Apifox 导入
- 完整 SDK 生成平台
- 自动修改用户前端仓库
- 复杂多级审批流

## v0.1 MCP Tools

查询工具：

```text
list_projects
list_documents
list_api_versions
get_latest_schema
get_endpoint_detail
compare_api_versions
get_change_summary
get_latest_doc
compare_doc_versions
```

草稿工具（v0.1）：

```text
create_api_version_draft
update_api_version_draft
submit_api_version_draft
get_api_version_draft
create_doc_draft
update_doc_draft
submit_doc_draft
get_doc_draft
```

v0.1 不提供直接发布工具。版本发布必须由 Admin 或 SuperAdmin 人工审核触发。

## 后端架构

```text
Web App
  - 团队 / 项目 / 成员 / 角色管理
  - API 文档展示
  - 版本列表
  - 语义 Diff 展示
  - Breaking-change 摘要

API Server
  - 项目管理
  - 文档和文档分支管理
  - OpenAPI 和 Markdown 上传
  - 草稿审核和发布
  - Document Version 生成
  - 权限校验
  - Diff 查询
  - MCP token 管理

MCP Server
  - AI 查询 OpenAPI 和 Markdown 文档
  - AI 查询版本 Diff
  - AI 提交/更新 OpenAPI 和 Markdown 草稿
  - AI 获取前端变更摘要

Diff Engine
  - OpenAPI parse
  - Schema normalize
  - Contract model extraction
  - Semantic diff
  - Breaking-change rules

Storage
  - PostgreSQL 保存用户、团队、项目、文档、分支、草稿、版本、Endpoint 索引、Diff 摘要、审计日志和 token 安全元数据
  - `storage.enabled=true` 时，RustFS 或任意 S3-compatible 对象存储保存 raw、normalized、stable 和大 Diff 快照
  - `database.enabled=false` 时，本地开发和测试使用内存兼容 store
```

## 仓库结构

```text
vdoc/
├── main.go                  # 服务生命周期、CLI 参数、配置、日志、优雅关闭
├── Makefile                 # 构建、运行、测试、格式化、检查、验证
├── api/                     # 传输层：Gin 路由、中间件、请求/响应 DTO、领域错误映射
├── common/                  # 跨模块共享业务语义：枚举、常量、跨模块 DTO、事件定义
├── config/                  # Viper 配置加载、默认值、热重载、安全校验
├── db/                      # 持久化适配层：GORM/PostgreSQL 模型、查询、迁移、RustFS/S3 适配
├── domain/                  # 业务规则层：领域模型、状态流转、领域错误、repository/storage ports
├── services/                # 长驻服务和后台任务层，只放 cron、worker、consumer 生命周期管理
├── static/                  # 静态资源占位
└── utils/                   # JWT、日志、context key、PID 文件、ID、加密等业务无关基础设施
```

## 快速开始

### 环境要求

- Go 1.25+
- Make

### 配置

```bash
cp config.yaml.example config.yaml
```

运行服务前需要设置强 JWT 密钥：

```bash
export VDOC_JWT_KEY="$(openssl rand -base64 32)"
```

也可以直接编辑 `config.yaml`。不要提交真实 `config.yaml` 或密钥。

### 运行

```bash
make dev
```

健康检查：

```bash
curl http://127.0.0.1:8080/api/v1/open/health
```

### 常用命令

```bash
make help
make build
make run
make dev
make test
make fmt
make lint
make verify
make clean
make build CROSS=1
```

## 当前 API

完整 v0.1 路由列表维护在 [docs/api/API.md](docs/api/API.md) 和 [docs/api/openapi.yaml](docs/api/openapi.yaml)。当前已经实现公开 health/auth/docs/MCP 路由，以及私有 identity、user、team、project、member、document、branch、draft、version、endpoint、diff 和 MCP token 路由。

当前响应使用统一包裹。HTTP status 固定为 `200`，业务成功或失败由 JSON 中的 `code` 和 `status` 表达。

## 配置

配置来自 `config.yaml`、默认值和 `VDOC_` 环境变量。

示例：

```bash
export VDOC_SERVER_PORT=9090
export VDOC_JWT_KEY="$(openssl rand -base64 32)"
export VDOC_LOG_LEVEL=info
export VDOC_DATABASE_ENABLED=true
export VDOC_DATABASE_DSN="postgres://vdoc:vdoc@127.0.0.1:5432/vdoc?sslmode=disable"
export VDOC_STORAGE_ENABLED=true
export VDOC_STORAGE_ENDPOINT="127.0.0.1:9000"
export VDOC_STORAGE_BUCKET="vdoc"
export VDOC_STORAGE_ACCESS_KEY="rustfs-access-key"
export VDOC_STORAGE_SECRET_KEY="rustfs-secret-key"
```

配置 `database.enabled=true` 后，服务启动时会连接 PostgreSQL、自动创建 Vdoc 运行表并加载已有状态；连接失败会直接启动失败，避免静默退回本地内存模式。配置 `storage.enabled=true` 后，OpenAPI raw / normalized schema 会写入 RustFS 或任意 S3-compatible 对象存储；bucket 不存在时会自动创建。

配置文件查找顺序：

1. 程序目录
2. 工作目录
3. `/etc/vdoc/`

## 文档

- [Product PRD](../PRD.md)
- [Implementation plan](../IMPLEMENTATION_PLAN.md)
- [Database schema design](../DATABASE_SCHEMA.md)
- [Roadmap and improvements](../IMPROVEMENTS.md)
- [English README](README.md)
- [中文路线图](../IMPROVEMENTS.zh-CN.md)

## 贡献

欢迎提交 Issue 和 Pull Request。项目仍在早期阶段，请优先围绕 MVP 范围贡献：OpenAPI 和 Markdown 文档、不可变版本、Endpoint 索引、语义 Diff、Markdown Diff 和 MCP 集成。

## 许可证

[MIT License](LICENSE)
