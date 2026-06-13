# Vdoc

Languages: [English](README.md) | [简体中文](README.zh-CN.md)

AI-friendly document collaboration hub with OpenAPI docs, Markdown docs, semantic diff, and MCP.

Vdoc helps AI/vibe-coding teams keep API changes, Markdown project knowledge, frontend integration work, and AI agent context in sync.

## What Is Vdoc?

Vdoc is a document collaboration platform for fast-moving teams. A Project owns typed Documents, including OpenAPI API docs and Markdown docs. Vdoc stores every published document as an immutable version, computes OpenAPI semantic diffs or Markdown text diffs, and exposes reviewed document knowledge to both humans and AI agents.

The long-term goal is not to build another Swagger UI clone. Vdoc focuses on the workflow that often breaks in AI-assisted development:

```text
Backend uploads or AI submits an OpenAPI or Markdown draft through MCP
        -> A human reviewer approves it and Vdoc creates an immutable version
        -> Vdoc parses OpenAPI endpoints when applicable
        -> Vdoc computes OpenAPI semantic diff or Markdown text diff
        -> Vdoc marks breaking changes when applicable
        -> Frontend or AI queries changes, endpoint details, or Markdown content
        -> Frontend updates integration code and project knowledge with context
```

## Current Status

This repository contains the Go/Gin backend for Vdoc v0.1.

API documentation:

- Human-readable guide: [docs/api/API.md](docs/api/API.md)
- Machine-readable OpenAPI spec: [docs/api/openapi.yaml](docs/api/openapi.yaml)

Pilot and release documentation lives at the workspace root:

- [../PILOT_RUNBOOK.md](../PILOT_RUNBOOK.md)
- [../RELEASE_DEPLOY.md](../RELEASE_DEPLOY.md)

Implemented in v0.1:

- Versioned route tree under `/api/v1`
- Public register/login and private JWT routes
- SuperAdmin user lifecycle, teams, projects, members, documents, and document branches
- OpenAPI and Markdown draft upload, review, and immutable version publishing
- Raw, normalized, and stable content retrieval for drafts and published versions
- Endpoint index queries, semantic OpenAPI diff summaries, and Markdown file diffs
- MCP token lifecycle and JSON-RPC MCP read and draft tools
- Admin API support for product onboarding, workflow guidance, developer-portal endpoint browsing, document content, and diff review
- Unified JSON response envelope with `trace_id` and `timestamp`
- Request tracing, structured access logs, panic recovery, CORS, security headers, body-size limit, and rate-limit middleware
- Viper-based configuration with `VDOC_` environment variables
- Makefile targets for build, test, lint, format, and cross-platform packaging

Not in v0.1:

- Direct MCP publish tools
- Code generation and frontend integration helpers
- Commercial operations features such as billing, notification center, integration marketplace, and organization-level tenant administration

## Product Concepts

| Concept | Meaning |
|---|---|
| Team | A collaboration boundary for people and projects. |
| Project | A product or app owned by a team. |
| Document | A typed project document. v0.1 supports OpenAPI API docs and Markdown docs. |
| Document Type | `1` means OpenAPI. `2` means Markdown. |
| Relative Path | The project-local path identity for a document, such as `apis/petstore.yaml` or `docs/runbook.md`. |
| Document Branch / Environment | A Vdoc document track, such as `dev`, `test`, `prod`, or `feature/*`. `prod` is protected by default. |
| Document Version | An immutable snapshot for one typed document. |
| Endpoint Index | A structured database index of paths, methods, parameters, request bodies, responses, tags, and operation IDs. |
| Semantic Diff | Contract-aware comparison between two versions, not raw text diff. |
| Breaking Change | A change that can break frontend consumers, such as field removal, type change, new required parameter, or endpoint removal. |
| MCP Token | A user-bound AI tool token. Creation returns the one-time copyable token value, while later list/get responses are redacted. Effective permissions come from token scopes plus the user's role on the target project. |

## MVP Workflow

1. SuperAdmin creates system members, teams, and projects.
2. SuperAdmin assigns the initial Project Admin.
3. Project Admin manually adds existing system users and assigns project-level roles.
4. Project Admin creates an OpenAPI or Markdown Document with a `relative_path`.
5. Writer uploads through Web API, or AI submits a draft through MCP for a target document branch.
6. Vdoc validates and stores raw content plus normalized or stable snapshots.
7. Project Admin approves the draft and Vdoc creates an immutable document version.
8. Vdoc parses an endpoint index for OpenAPI documents.
9. Vdoc compares the new version with the previous one using OpenAPI semantic diff or Markdown text diff.
10. Vdoc stores a change summary and breaking-change list when applicable.
11. Frontend developers and AI agents query endpoint details, Markdown content, diffs, and summaries.

## v0.1 Scope

Implemented in the current v0.1 backend:

- System-level `SuperAdmin`; project-level roles: `Reader`, `Writer`, `Admin`, where Writer submits drafts and Admin reviews/publishes them
- OpenAPI 3.x and Markdown upload through Web API, plus MCP draft submission and updates
- Manual project member adds from existing system users, without invitation workflow in MVP
- Document branches/environments with `dev`, `test`, protected `prod`, optional `feature/*`, and promote-to-target-draft flow
- Immutable versions per typed document
- Human-reviewed publication for OpenAPI and Markdown drafts
- Endpoint list and endpoint detail query
- Version comparison with semantic OpenAPI diff or Markdown file diff
- Breaking-change summary
- MCP read and draft tools only; human review publishes versions

Explicitly out of scope for the first MVP:

- Complex organization-wide RBAC
- GraphQL, gRPC, Postman, YApi, or Apifox import
- Full SDK generation platform
- Automatic modification of frontend repositories
- Complex multi-step approval workflows

## MCP Tools In v0.1

Read tools:

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

Draft tools for v0.1:

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

Direct publish tools are not available in v0.1. Human Admin or SuperAdmin review publishes versions.

Installable agent-facing assets are kept outside this backend repository: the MCP stdio adapter lives in `../Vdoc-mcp/`, and the workflow skill lives in `../Vdoc-skill/`.

## Backend Architecture

```text
Web App
  - Team / project / member / role management
  - API documentation view
  - Version list
  - Semantic diff view
  - Breaking-change summary

API Server
  - Project management
  - Document and document branch management
  - OpenAPI and Markdown upload
  - Draft review and publication
  - Document version creation
  - Permission checks
  - Diff query
  - MCP token management

MCP Server
  - AI OpenAPI and Markdown document lookup
  - AI version diff lookup
  - AI OpenAPI and Markdown draft submit/update
  - AI frontend change summaries

Diff Engine
  - OpenAPI parse
  - Schema normalization
  - Contract model extraction
  - Semantic diff
  - Breaking-change rules

Storage
  - PostgreSQL for users, teams, projects, documents, branches, drafts, versions, endpoint indexes, diff summaries, audit logs, and token security metadata
  - RustFS or any S3-compatible object storage for raw, normalized, stable, and large diff snapshots when `storage.enabled=true`
  - In-memory compatibility store for local development and tests when `database.enabled=false`
```

## Repository Structure

```text
vdoc/
├── main.go                  # Server lifecycle, CLI flags, config, logging, graceful shutdown
├── Makefile                 # Build, run, test, format, lint, verify
├── api/                     # Transport layer: Gin routes, middleware, request/response DTOs, error mapping
├── common/                  # Shared business semantics: enums, constants, DTOs used across modules, events
├── config/                  # Viper config loading, defaults, hot reload, safety checks
├── db/                      # Persistence adapters: GORM/PostgreSQL models, queries, migrations, RustFS/S3 adapters
├── domain/                  # Business rules, state transitions, domain errors, repository and storage ports
├── services/                # Long running cron, worker, and consumer lifecycle tasks
├── static/                  # Static asset placeholder
└── utils/                   # Business neutral infrastructure helpers for JWT, logging, IDs, crypto, paths
```

## Quick Start

### Requirements

- Go 1.25+
- Make

### Configure

```bash
cp config.yaml.example config.yaml
```

Set a strong JWT key before running the service:

```bash
export VDOC_JWT_KEY="$(openssl rand -base64 32)"
```

You can also edit `config.yaml` directly. Do not commit real `config.yaml` files or secrets.

### Run

```bash
make dev
```

Health check:

```bash
curl http://127.0.0.1:8080/api/v1/open/health
```

### Common Commands

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

## Current API

The full v0.1 route list is maintained in [docs/api/API.md](docs/api/API.md) and [docs/api/openapi.yaml](docs/api/openapi.yaml). The implemented surfaces include public health/auth/docs/MCP routes and private identity, user, team, project, member, document, branch, draft, version, endpoint, diff, and MCP token routes.

Responses use a project envelope. HTTP status is currently always `200`; semantic success or failure is represented by the JSON `code` and `status` fields.

## Configuration

Configuration is loaded from `config.yaml`, defaults, and `VDOC_` environment variables.

Examples:

```bash
export VDOC_SERVER_PORT=9090
export VDOC_JWT_KEY="$(openssl rand -base64 32)"
export VDOC_LOG_LEVEL=info
export VDOC_INITIAL_ADMIN_EMAIL="admin@example.com"
export VDOC_INITIAL_ADMIN_NAME="Vdoc Admin"
export VDOC_INITIAL_ADMIN_PASSWORD="<initial-admin-password>"
export VDOC_DATABASE_ENABLED=true
export VDOC_DATABASE_DSN="postgres://vdoc:<password>@127.0.0.1:5432/vdoc?sslmode=disable"
export VDOC_STORAGE_ENABLED=true
export VDOC_STORAGE_ENDPOINT="127.0.0.1:9000"
export VDOC_STORAGE_BUCKET="vdoc"
export VDOC_STORAGE_ACCESS_KEY="<access-key>"
export VDOC_STORAGE_SECRET_KEY="<secret-key>"
```

When `initial_admin.email` and `initial_admin.password` are configured, Vdoc creates that SuperAdmin account only if the loaded user table is empty. The password is bcrypt-hashed before persistence. When `database.enabled=true`, Vdoc connects to PostgreSQL during startup, creates its runtime tables, and loads existing state. Connection or migration failure aborts startup instead of silently falling back to memory. When `storage.enabled=true`, raw and normalized OpenAPI schemas are written to RustFS or any S3-compatible object storage; the bucket is created automatically when missing.

If a SuperAdmin password is lost, run the built binary against the configured PostgreSQL database without starting the HTTP server:

```bash
./vdoc --resetadmin admin@example.com "<new-admin-password>"
```

The target user must already exist, be active, and be a SuperAdmin. The new password follows the same strength rule as `initial_admin.password`.

Config file lookup order:

1. Program directory
2. Working directory
3. `/etc/vdoc/`

## Documentation

- [Product PRD](../PRD.md)
- [Implementation plan](../IMPLEMENTATION_PLAN.md)
- [Database schema design](../DATABASE_SCHEMA.md)
- [Roadmap and improvements](../IMPROVEMENTS.md)
- [中文 README](README.zh-CN.md)
- [中文路线图](../IMPROVEMENTS.zh-CN.md)

## Contributing

Issues and pull requests are welcome. Since the project is early, please keep changes aligned with the MVP scope: OpenAPI and Markdown documents, immutable versions, endpoint indexes, semantic diff, Markdown diff, and MCP integration.

## License

[MIT License](LICENSE)
