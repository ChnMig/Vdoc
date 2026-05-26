# Roadmap and Improvements

Languages: [English](IMPROVEMENTS.md) | [简体中文](IMPROVEMENTS.zh-CN.md)

This document tracks the product roadmap and backend improvement plan for Vdoc.

## Current v0.1 Backend Foundation

The repository currently provides the Vdoc v0.1 Go/Gin backend:

- HTTP server lifecycle and graceful shutdown
- Config loading through Viper and `VDOC_` environment variables
- Startup safety checks for JWT configuration
- Structured logging with Zap and separate Gin logs
- Request tracing through `trace_id`
- Unified response envelope
- CORS, security headers, request body limit, recovery, and rate limiting
- Public auth, private JWT routes, MCP token lifecycle, JSON-RPC MCP read and draft tools
- SuperAdmin user lifecycle, teams, projects, members, documents, document branches, OpenAPI and Markdown drafts, reviewed publishing, endpoint indexes, semantic diffs, Markdown diffs, and audit logs
- GORM/PostgreSQL persistence with normalized tables when `database.enabled=true`
- RustFS or S3-compatible object storage for raw/normalized schema snapshots when `storage.enabled=true`
- Health endpoint and tests

The in-memory store remains available for local development and tests when `database.enabled=false`; database startup fails instead of silently falling back when `database.enabled=true` cannot initialize.

## v0.1 Core Workflow

The current backend validates one typed document workflow:

```text
Backend uploads or AI submits an OpenAPI or Markdown draft through MCP
        -> A human reviewer approves it and Vdoc creates a version
        -> Vdoc parses OpenAPI endpoints when applicable
        -> Vdoc computes OpenAPI semantic diff or Markdown text diff
        -> Frontend or AI queries changes and reviewed document content
        -> Frontend updates integration code or project knowledge
```

## 1. Team, Project, and Role Model

v0.1 implements a system-level super administrator plus project-level collaboration. Organization-wide RBAC remains deferred until the product needs it.

Initial model:

```text
System
  -> User
       - SuperAdmin: system-level fallback management for users, projects, members, and publication

Team
  -> Project
       -> ProjectMember
            - Reader: api:read
            - Writer: api:read + api:draft
            - Admin: api:read + api:draft + api:publish + project:manage + member:manage
```

A user can join multiple projects with different project roles. Writer can create, update, and submit drafts only; publication must be performed by Project Admin or SuperAdmin. JWT stores only necessary user identity, while project permissions are resolved by `user_id + project_id`. MVP members are manually added from existing system users, without an invitation flow.

## 2. Project Documents and Versioning

Each project can contain multiple typed documents. v0.1 supports OpenAPI API documents and Markdown documents. The `relative_path` field is the only stored path identity for a document.

Rules:

- A published document version is immutable.
- Uploading changed content first creates a draft; approval creates the new version.
- Documents have branches/environments: `dev`, `test`, protected `prod`, and optional `feature/*`.
- Promote creates a draft on the target branch, then uses the same review and publication flow.
- Raw content is preserved for audit, download, and future reprocessing.
- OpenAPI stores normalized snapshots for stable hashing and semantic comparison.
- Markdown stores stable snapshots for plain file diff and latest document lookup.

## 3. OpenAPI Upload Pipeline

Target processing flow:

```text
Receive OpenAPI YAML/JSON
  -> Read target branch_id and optional source_git_commit_id
  -> Validate OpenAPI 3.x
  -> Store raw schema
  -> Normalize schema
  -> Compute raw and normalized hashes
  -> Detect no-change uploads
  -> Create document draft
  -> Human approval
  -> Create document version
  -> Parse endpoint index
  -> Schedule semantic diff
```

v0.1 uses RustFS or another S3-compatible object store for raw, normalized, stable, and larger diff snapshots when storage is enabled. PostgreSQL stores object keys, hashes, and metadata.

## 4. Endpoint Index

Do not query large raw OpenAPI files for every read request. Parse a structured index.

Index data includes:

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

Vdoc compares OpenAPI structures, not raw JSON text. Markdown documents use plain file diff instead of endpoint-level semantic rules.

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
apis/petstore.yaml 1.0.0 -> 1.1.0

Added endpoints: 0
Removed endpoints: 0
Modified endpoints: 1
Breaking changes: 2

GET /api/users/{id}
- response.name deleted [breaking]
- response.age string -> number [breaking]
- response.email added [info]
```

## 7. MCP Integration

MCP is the core AI integration surface.

Read and draft tools first:

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

Direct publish tools are not part of v0.1. Human Admin or SuperAdmin review publishes versions.

Security requirements:

- v0.1 MCP tokens are user-bound, not project-bound, so users can configure one token in their MCP client.
- Effective MCP tool permissions are `token.scopes` intersected with the token owner's ProjectMember role on the target project; SuperAdmin can fall back to all projects.
- `api:read` tokens cannot create or update drafts.
- `api:draft` tokens can submit drafts only when the user has Writer/Admin/SuperAdmin permission on the target project, and cannot publish schemas.
- Publication must be triggered by a Project Admin or SuperAdmin human action with `api:publish`.
- Token creation returns a one-time copyable MCP token value. List, get, and revoke responses are redacted.
- Users can view redacted active MCP tokens in the backend, generate new tokens, and revoke old tokens.
- The backend uses `token_hash` for call authentication and encrypted `token_ciphertext` for backend display.
- Draft writes, token reveal/copy, token revocation, token use, and publish actions must be auditable.
- Raw secrets must never be returned by MCP tools; only backend token-management APIs may return the full token to its owner.
- Project-bound robot/CI tokens are deferred to v0.2 evaluation.

## 8. Storage Direction

Recommended layers:

```text
PostgreSQL
  - users, teams, projects, members, permissions
  - documents, branches, drafts, and version metadata
  - endpoint index
  - diff result and change summary
  - integer codes for finite status, type, method, severity, and scope fields

RustFS Object Storage
  - raw OpenAPI and Markdown snapshots
  - normalized OpenAPI snapshots
  - stable Markdown snapshots
  - full diff snapshots
  - optional compressed schema AST

Redis / Queue
  - parse jobs
  - diff jobs
  - later codegen jobs
```

## 9. Later Skill Support

MCP provides tool capabilities. A future Skill can teach AI agents the preferred workflow.

Possible Skill workflows:

- Backend or AI submits an updated OpenAPI or Markdown document draft.
- Reviewers publish after checking the draft diff preview.
- Frontend compares two API versions.
- Frontend asks for endpoint-specific TypeScript types and request functions.
- AI identifies frontend files likely affected by breaking changes.
- AI fetches the latest Markdown guide or compares two Markdown document versions.

## Non-Goals for Now

- Complex organization and billing model
- GraphQL, gRPC, Postman, YApi, or Apifox import
- Complete SDK/codegen platform
- Automatic frontend repository edits
- Complex multi-step approval workflow
- Public SaaS multi-tenancy hardening

## Success Criteria

The MVP is useful if:

1. Backend developers can submit an OpenAPI or Markdown draft within one minute, and Project Admins can publish after review.
2. Vdoc can show what changed since the previous version.
3. Vdoc can identify common breaking changes.
4. Frontend developers can query endpoint details, Markdown content, and version diffs through Web UI or MCP.
5. AI agents can submit OpenAPI and Markdown drafts through MCP and clearly wait for human approval.
6. AI agents can use MCP responses to generate or update frontend integration code and project guidance.
