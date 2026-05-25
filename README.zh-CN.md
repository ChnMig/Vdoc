# Vdoc

语言: [English](README.md) | [简体中文](README.zh-CN.md)

面向 AI 协作开发的 API 契约协作平台，支持 OpenAPI 版本管理、语义 Diff 和 MCP。

Vdoc 帮助使用 AI/Vibe Coding 的团队同步后端接口变更、前端对接代码和 AI Agent 上下文。

## Vdoc 是什么？

Vdoc 是一个面向快速迭代团队的 API 契约协作平台。它以 OpenAPI 作为接口协作的事实来源，把每次发布的接口文档保存为不可变版本，计算版本之间的语义差异，并把接口知识同时暴露给人和 AI Agent。

Vdoc 的目标不是再做一个 Swagger UI，而是解决 AI 辅助开发里经常断掉的接口同步链路：

```text
后端上传或 AI 通过 MCP 提交 OpenAPI 草稿
        -> 人工审核后 Vdoc 创建不可变版本
        -> Vdoc 解析接口契约
        -> Vdoc 与上一版本做语义 Diff
        -> Vdoc 标记 breaking changes
        -> 前端或 AI 查询变更和接口详情
        -> 前端带着上下文更新对接代码
```

## 当前状态

这个仓库当前是 Vdoc v0.1 的 Go/Gin 后端。

API 文档：

- 人类可读指南：[docs/api/API.md](docs/api/API.md)
- 机器可读 OpenAPI 规格：[docs/api/openapi.yaml](docs/api/openapi.yaml)

v0.1 已经实现：

- `/api/v1` 版本化路由树
- 公开注册、登录和私有 JWT 路由
- SuperAdmin 用户生命周期、Team、Project、Member、Service 和 Branch
- OpenAPI 草稿上传、审核和不可变版本发布
- 草稿和已发布版本的 raw、normalized schema 查询
- Endpoint 索引查询和语义 API Diff 摘要
- MCP Token 生命周期和 JSON-RPC MCP 查询、草稿 tools
- 带 `trace_id` 和 `timestamp` 的统一 JSON 响应包裹
- 请求追踪、结构化访问日志、panic recovery、CORS、安全响应头、请求体大小限制、限流中间件
- 基于 Viper 的配置加载和 `VDOC_` 环境变量覆盖
- 用于构建、测试、检查、格式化和跨平台打包的 Makefile

v0.1 不包含：

- 直接发布类 MCP tools，包括 `publish_api_schema` 和 `publish_api_version`
- 代码生成和前端对接辅助

## 产品概念

| 概念 | 含义 |
|---|---|
| Team | 团队协作边界。 |
| Project | 团队下的产品或应用。 |
| Service | 项目内的后端服务，例如 `user-service` 或 `order-service`。 |
| Contract Branch / Environment | Service 下的契约发布轨道，例如 `dev`、`test`、`prod` 或 `feature/*`，其中 `prod` 默认受保护。 |
| Contract Version | 某个服务的一次不可变 OpenAPI 快照。 |
| Endpoint Index | 从 OpenAPI 解析出的结构化索引，包括路径、方法、参数、请求体、响应、标签和 operationId。 |
| Semantic Diff | 面向接口契约的版本比较，而不是原始文本 diff。 |
| Breaking Change | 可能破坏前端消费方的变更，例如字段删除、类型变化、新增必填参数、接口删除。 |
| MCP Token | 用户绑定的 AI 工具访问 token；后台可查看、复制、生成和废弃，权限由 token scopes 与用户在目标 Project 的角色共同决定。 |

## MVP 使用流程

1. SuperAdmin 创建系统成员、Team 和 Project。
2. SuperAdmin 指定 Project Admin。
3. Project Admin 从现有系统用户中手动添加成员并分配项目级角色。
4. Project Admin 创建 Service。
5. Writer 上传，或 AI 通过 MCP 向目标分支提交 OpenAPI 3.x 草稿。
6. Vdoc 校验并保存 Raw Schema。
7. Project Admin 人工审核通过后，Vdoc 创建不可变 Contract Version。
8. Vdoc 解析 Endpoint Index，用于快速查询和展示。
9. Vdoc 将新版本与上一版本做语义 Diff。
10. Vdoc 保存变更摘要和 breaking-change 列表。
11. 前端开发和 AI Agent 查询接口详情、版本差异和摘要。

## v0.1 范围

当前 v0.1 后端已经实现：

- 系统级 `SuperAdmin`；项目级角色：`Reader`、`Writer`、`Admin`，其中 Writer 只能提交草稿，Admin 负责审核发布
- 通过 Web API 上传 OpenAPI 3.x，AI 可通过 MCP 提交和更新草稿
- MVP 不做邀请流程，项目成员从现有系统用户手动添加
- Service 契约分支和环境，支持 `dev`、`test`、受保护 `prod`、可选 `feature/*`，以及 promote 到目标分支草稿
- 每个 Service 下的不可变接口契约版本
- OpenAPI 草稿人工审核后发布
- 接口列表和接口详情查询
- 版本比较和语义 Diff
- Breaking-change 摘要
- 先做 MCP 查询和草稿 tools，直接发布 tools 后续再做

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
list_services
list_api_versions
get_latest_schema
get_endpoint_detail
compare_api_versions
get_change_summary
```

草稿工具（v0.1）：

```text
create_api_version_draft
update_api_version_draft
submit_api_version_draft
get_api_version_draft
```

v0.1 不提供直接发布工具：

```text
publish_api_schema
publish_api_version
```

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
  - Service 分支 / 环境管理
  - OpenAPI 上传
  - 草稿审核和发布
  - 接口契约版本生成
  - 权限校验
  - Diff 查询
  - MCP token 管理

MCP Server
  - AI 查询接口契约
  - AI 查询版本 Diff
  - AI 提交/更新 OpenAPI 草稿
  - AI 获取前端变更摘要

Diff Engine
  - OpenAPI parse
  - Schema normalize
  - Contract model extraction
  - Semantic diff
  - Breaking-change rules

Storage
  - PostgreSQL 保存用户、团队、项目、服务、分支、草稿、版本、Endpoint 索引、Diff 摘要、审计日志和 token 安全元数据
  - `storage.enabled=true` 时，RustFS 或任意 S3-compatible 对象存储保存 Raw / Normalized OpenAPI 快照和大 Diff 快照
  - `database.enabled=false` 时，本地开发和测试使用内存兼容 store
```

## 仓库结构

```text
vdoc/
├── main.go                  # 服务生命周期、CLI 参数、配置、日志、优雅关闭
├── Makefile                 # 构建、运行、测试、格式化、检查、验证
├── api/                     # Gin 初始化、中间件、响应包裹、版本化路由
├── common/                  # 共享 DTO 和通用类型
├── config/                  # Viper 配置加载、默认值、热重载、安全校验
├── db/                      # GORM PostgreSQL 客户端、迁移和 Vdoc repository
├── domain/                  # 领域模型、repository 接口和健康状态
├── services/                # Vdoc 服务 facade、对象存储接入、OpenAPI 解析和 Diff 逻辑
├── static/                  # 静态资源占位
└── utils/                   # JWT、日志、context key、PID 文件、ID、加密工具
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

完整 v0.1 路由列表维护在 [docs/api/API.md](docs/api/API.md) 和 [docs/api/openapi.yaml](docs/api/openapi.yaml)。当前已经实现公开 health/auth/docs/MCP 路由，以及私有 identity、user、team、project、member、service、branch、draft、contract、endpoint、diff 和 MCP token 路由。

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

- [Roadmap and improvements](IMPROVEMENTS.md)
- [English README](README.md)
- [中文路线图](IMPROVEMENTS.zh-CN.md)

## 贡献

欢迎提交 Issue 和 Pull Request。项目仍在早期阶段，请优先围绕 MVP 范围贡献：OpenAPI 契约、不可变版本、Endpoint 索引、语义 Diff 和 MCP 集成。

## 许可证

[MIT License](LICENSE)
