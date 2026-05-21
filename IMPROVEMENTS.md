# Roadmap and Improvements

Languages: [English](IMPROVEMENTS.md) | [简体中文](IMPROVEMENTS.zh-CN.md)

This document tracks the product roadmap and backend improvement plan for Vdoc.

## Current Backend Foundation

The repository currently provides a production-oriented Go/Gin backend scaffold:

- HTTP server lifecycle and graceful shutdown
- Config loading through Viper and `VDOC_` environment variables
- Startup safety checks for JWT configuration
- Structured logging with Zap and separate Gin logs
- Request tracing through `trace_id`
- Unified response envelope
- CORS, security headers, request body limit, recovery, and rate limiting
- Health endpoint and tests

This foundation is ready for product-domain work, but it is not yet the API contract hub itself.

## MVP Priorities

The MVP should validate one core workflow:

```text
Backend or AI uploads OpenAPI
        -> Vdoc creates a version
        -> Vdoc parses endpoint contracts
        -> Vdoc computes semantic diff
        -> Frontend or AI queries changes
        -> Frontend updates integration code
```

## 1. Team, Project, and Role Model

Implement project-level collaboration first. Avoid organization-wide RBAC until the product needs it.

Initial model:

```text
Team
  -> Project
       -> ProjectMember
            - Reader: api:read
            - Writer: api:read + api:write
            - Admin: api:read + api:write + project:manage + member:manage
```

## 2. Service and Contract Versioning

Each project can contain multiple services. Each service owns its OpenAPI versions.

Rules:

- A published contract version is immutable.
- Uploading a changed schema creates a new version.
- Raw OpenAPI is preserved for audit, download, and future reprocessing.
- Normalized OpenAPI is stored for stable hashing and comparison.

## 3. OpenAPI Upload Pipeline

Target processing flow:

```text
Receive OpenAPI YAML/JSON
  -> Validate OpenAPI 3.x
  -> Store raw schema
  -> Normalize schema
  -> Compute raw and normalized hashes
  -> Detect no-change uploads
  -> Create contract version
  -> Parse endpoint index
  -> Schedule semantic diff
```

MVP can start with local filesystem storage for raw schemas, then move to S3/MinIO-compatible object storage.

## 4. Endpoint Index

Do not query large raw OpenAPI files for every read request. Parse a structured index.

Index data should include:

- HTTP method and path
- Operation ID
- Tags
- Parameters
- Request body schema
- Response schemas
- Required fields
- Deprecation state
- Endpoint-level hashes

## 5. Semantic Diff

Vdoc should compare API contracts, not raw JSON text.

Initial diff scope:

- Added endpoint
- Removed endpoint
- Modified endpoint
- Request parameter changes
- Request body changes
- Response field changes
- Security requirement changes
- Deprecated state changes

Initial breaking-change rules:

- Endpoint removal
- Required parameter addition
- Parameter type change
- Parameter location change
- Required request-body field addition
- Response field removal
- Response field type change
- Enum value removal

## 6. Change Summary

Store machine-readable diff items and human-readable summaries.

Example summary:

```text
user-service 1.0.0 -> 1.1.0

Added endpoints: 0
Removed endpoints: 0
Modified endpoints: 1
Breaking changes: 2

GET /api/users/{id}
- response.name deleted [breaking]
- response.age string -> number [breaking]
- response.avatar added [info]
```

## 7. MCP Integration

MCP is the core AI integration surface.

Read tools first:

```text
list_projects
list_services
list_api_versions
get_latest_schema
get_endpoint_detail
compare_api_versions
get_change_summary
```

Write tools later:

```text
publish_api_schema
create_api_version_draft
update_api_version_draft
publish_api_version
```

Security requirements:

- Tokens must be scoped by project.
- Read tokens cannot publish schemas.
- Write actions must be auditable.
- Raw secrets must never be returned by MCP tools.

## 8. Storage Direction

Recommended layers:

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

## 9. Later Skill Support

MCP provides tool capabilities. A future Skill can teach AI agents the preferred workflow.

Possible Skill workflows:

- Backend publishes an updated API contract.
- Frontend compares two API versions.
- Frontend asks for endpoint-specific TypeScript types and request functions.
- AI identifies frontend files likely affected by breaking changes.

## Non-Goals for Now

- Complex organization and billing model
- GraphQL, gRPC, Postman, YApi, or Apifox import
- Complete SDK/codegen platform
- Automatic frontend repository edits
- Heavy approval workflow
- Public SaaS multi-tenancy hardening

## Success Criteria

The MVP is useful if:

1. Backend developers can publish an OpenAPI version within one minute.
2. Vdoc can show what changed since the previous version.
3. Vdoc can identify common breaking changes.
4. Frontend developers can query endpoint details and version diffs through Web UI or MCP.
5. AI agents can use MCP responses to generate or update frontend integration code.
