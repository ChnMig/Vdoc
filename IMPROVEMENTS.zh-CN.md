# 路线图与改进计划

语言: [English](IMPROVEMENTS.md) | [简体中文](IMPROVEMENTS.zh-CN.md)

本文档记录 Vdoc 的产品路线图和后端改进计划。

## 当前后端基础

当前仓库提供了偏生产可用的 Go/Gin 后端脚手架：

- HTTP 服务生命周期和优雅关闭
- 基于 Viper 和 `VDOC_` 环境变量的配置加载
- JWT 配置启动安全校验
- Zap 结构化日志和独立 Gin 日志
- 基于 `trace_id` 的请求追踪
- 统一响应包裹
- CORS、安全响应头、请求体大小限制、Recovery、限流中间件
- 健康检查接口和测试

这些基础设施已经可以支撑后续业务开发，但它本身还不是完整的 API Contract Hub。

## MVP 优先级

MVP 需要验证一条核心链路：

```text
后端或 AI 上传 OpenAPI
        -> Vdoc 创建版本
        -> Vdoc 解析接口契约
        -> Vdoc 计算语义 Diff
        -> 前端或 AI 查询变化
        -> 前端更新对接代码
```

## 1. Team、Project 和角色模型

先实现项目级协作，不要一开始做组织级复杂 RBAC。

初始模型：

```text
Team
  -> Project
       -> ProjectMember
            - Reader: api:read
            - Writer: api:read + api:write
            - Admin: api:read + api:write + project:manage + member:manage
```

## 2. Service 和契约版本

每个 Project 可以包含多个 Service。每个 Service 管理自己的 OpenAPI 版本。

规则：

- 已发布的契约版本不可变。
- 上传变化后的 schema 会创建新版本。
- Raw OpenAPI 必须保留，便于审计、下载和未来重新处理。
- Normalized OpenAPI 用于稳定 hash 和比较。

## 3. OpenAPI 上传流水线

目标处理流程：

```text
接收 OpenAPI YAML/JSON
  -> 校验 OpenAPI 3.x
  -> 保存 raw schema
  -> 规范化 schema
  -> 计算 raw 和 normalized hash
  -> 检测无变化上传
  -> 创建 contract version
  -> 解析 endpoint index
  -> 调度 semantic diff
```

MVP 可以先使用本地文件系统保存 raw schema，后续再切换到 S3/MinIO 兼容对象存储。

## 4. Endpoint Index

不要每次查询都读取大型 Raw OpenAPI 文件，而是解析成结构化索引。

索引数据应包括：

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

Vdoc 应该比较接口契约，而不是比较原始 JSON 文本。

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
- response.avatar added [info]
```

## 7. MCP 集成

MCP 是 Vdoc 面向 AI 的核心集成入口。

先做只读工具：

```text
list_projects
list_services
list_api_versions
get_latest_schema
get_endpoint_detail
compare_api_versions
get_change_summary
```

后续再做写入工具：

```text
publish_api_schema
create_api_version_draft
update_api_version_draft
publish_api_version
```

安全要求：

- Token 必须绑定项目范围。
- 只读 token 不能发布 schema。
- 写操作必须可审计。
- MCP tools 绝不能返回原始密钥。

## 8. 存储方向

推荐分层：

```text
PostgreSQL
  - users, teams, projects, members, permissions
  - services and version metadata
  - endpoint index
  - diff result and change summary

Object Storage
  - raw OpenAPI snapshots
  - normalized OpenAPI snapshots
  - optional compressed schema AST

Redis / Queue
  - parse jobs
  - diff jobs
  - later codegen jobs
```

## 9. 后续 Skill 支持

MCP 提供工具能力。未来可以通过 Skill 告诉 AI Agent 如何正确使用这些工具。

可能的 Skill 工作流：

- 后端发布更新后的 API 契约。
- 前端比较两个 API 版本。
- 前端请求某个 endpoint 的 TypeScript 类型和请求函数。
- AI 识别 breaking changes 可能影响的前端文件。

## 当前非目标

- 复杂组织和计费模型
- GraphQL、gRPC、Postman、YApi、Apifox 导入
- 完整 SDK/codegen 平台
- 自动修改前端仓库
- 重审批流程
- 公共 SaaS 多租户硬化

## 成功标准

如果满足以下条件，MVP 就有价值：

1. 后端开发能在 1 分钟内发布一个 OpenAPI 版本。
2. Vdoc 能展示相较上一版本发生了什么变化。
3. Vdoc 能识别常见 breaking changes。
4. 前端能通过 Web UI 或 MCP 查询接口详情和版本差异。
5. AI Agent 能基于 MCP 返回结果生成或更新前端对接代码。
