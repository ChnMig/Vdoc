# Vdoc

Languages: [English](README.md) | [简体中文](README.zh-CN.md)

AI-friendly API contract hub with OpenAPI versioning, semantic diff, and MCP.

Vdoc helps AI/vibe-coding teams keep backend API changes, frontend integration work, and AI agent context in sync.

## What Is Vdoc?

Vdoc is an API contract collaboration platform for fast-moving teams. It treats OpenAPI as the source of truth, stores every published contract as an immutable version, computes semantic diffs between versions, and exposes contract knowledge to both humans and AI agents.

The long-term goal is not to build another Swagger UI clone. Vdoc focuses on the workflow that often breaks in AI-assisted development:

```text
Backend uploads or AI submits an OpenAPI draft through MCP
        -> A human reviewer approves it and Vdoc creates an immutable version
        -> Vdoc parses endpoint contracts
        -> Vdoc computes semantic diff against the previous version
        -> Vdoc marks breaking changes
        -> Frontend or AI queries changes and endpoint details
        -> Frontend updates integration code with context
```

## Current Status

This repository contains the Go/Gin backend for Vdoc v0.1.

API documentation:

- Human-readable guide: [docs/api/API.md](docs/api/API.md)
- Machine-readable OpenAPI spec: [docs/api/openapi.yaml](docs/api/openapi.yaml)

Implemented in v0.1:

- Versioned route tree under `/api/v1`
- Public register/login and private JWT routes
- SuperAdmin user lifecycle, teams, projects, members, services, and branches
- OpenAPI draft upload, review, and immutable version publishing
- Raw and normalized schema retrieval for drafts and published versions
- Endpoint index queries and semantic API diff summaries
- MCP token lifecycle and JSON-RPC MCP read and draft tools
- Unified JSON response envelope with `trace_id` and `timestamp`
- Request tracing, structured access logs, panic recovery, CORS, security headers, body-size limit, and rate-limit middleware
- Viper-based configuration with `VDOC_` environment variables
- Makefile targets for build, test, lint, format, and cross-platform packaging

Not in v0.1:

- Direct MCP publish tools, including `publish_api_schema` and `publish_api_version`
- Code generation and frontend integration helpers

## Product Concepts

| Concept | Meaning |
|---|---|
| Team | A collaboration boundary for people and projects. |
| Project | A product or app owned by a team. |
| Service | A backend service inside a project, such as `user-service` or `order-service`. |
| Contract Branch / Environment | A Vdoc contract track under a service, such as `dev`, `test`, `prod`, or `feature/*`. `prod` is protected by default. |
| Contract Version | An immutable OpenAPI snapshot for a service. |
| Endpoint Index | A structured database index of paths, methods, parameters, request bodies, responses, tags, and operation IDs. |
| Semantic Diff | Contract-aware comparison between two versions, not raw text diff. |
| Breaking Change | A change that can break frontend consumers, such as field removal, type change, new required parameter, or endpoint removal. |
| MCP Token | A user-bound AI tool token that users can view, copy, generate, and revoke in the backend; effective permissions come from token scopes plus the user's role on the target project. |

## MVP Workflow

1. SuperAdmin creates system members, teams, and projects.
2. SuperAdmin assigns the initial Project Admin.
3. Project Admin manually adds existing system users and assigns project-level roles.
4. Project Admin creates a service.
5. Writer uploads, or AI submits an OpenAPI 3.x draft through MCP for a target branch.
6. Vdoc validates and stores the raw schema.
7. Project Admin approves the draft and Vdoc creates an immutable contract version.
8. Vdoc parses an endpoint index for fast query and display.
9. Vdoc compares the new version with the previous one.
10. Vdoc stores a change summary and breaking-change list.
11. Frontend developers and AI agents query endpoint details, diffs, and summaries.

## v0.1 Scope

Implemented in the current v0.1 backend:

- System-level `SuperAdmin`; project-level roles: `Reader`, `Writer`, `Admin`, where Writer submits drafts and Admin reviews/publishes them
- OpenAPI 3.x upload through Web API, plus MCP draft submission and updates
- Manual project member adds from existing system users, without invitation workflow in MVP
- Service contract branches/environments with `dev`, `test`, protected `prod`, optional `feature/*`, and promote-to-target-draft flow
- Immutable contract versions per service
- Human-reviewed publication for OpenAPI drafts
- Endpoint list and endpoint detail query
- Version comparison with semantic diff
- Breaking-change summary
- MCP read and draft tools first, direct publish tools later

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
list_services
list_api_versions
get_latest_schema
get_endpoint_detail
compare_api_versions
get_change_summary
```

Draft tools for v0.1:

```text
create_api_version_draft
update_api_version_draft
submit_api_version_draft
get_api_version_draft
```

Direct publish tools are not available in v0.1:

```text
publish_api_schema
publish_api_version
```

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
  - Service branch / environment management
  - OpenAPI upload
  - Draft review and publication
  - Contract version creation
  - Permission checks
  - Diff query
  - MCP token management

MCP Server
  - AI contract lookup
  - AI version diff lookup
  - AI OpenAPI draft submit/update
  - AI frontend change summaries

Diff Engine
  - OpenAPI parse
  - Schema normalization
  - Contract model extraction
  - Semantic diff
  - Breaking-change rules

Storage
  - PostgreSQL for users, teams, projects, services, branches, drafts, versions, endpoint indexes, diff summaries, audit logs, and token security metadata
  - RustFS or any S3-compatible object storage for raw and normalized OpenAPI snapshots and large diff snapshots when `storage.enabled=true`
  - In-memory compatibility store for local development and tests when `database.enabled=false`
```

## Repository Structure

```text
vdoc/
├── main.go                  # Server lifecycle, CLI flags, config, logging, graceful shutdown
├── Makefile                 # Build, run, test, format, lint, verify
├── api/                     # Gin setup, middleware, response envelope, versioned routes
├── common/                  # Shared DTOs and common types
├── config/                  # Viper config loading, defaults, hot reload, safety checks
├── db/                      # GORM PostgreSQL client, migrations, and Vdoc repository
├── domain/                  # Domain models, repository interfaces, and health state
├── services/                # Vdoc service facade, object storage integration, OpenAPI parsing, and diff logic
├── static/                  # Static asset placeholder
└── utils/                   # JWT, logging, context keys, PID file, IDs, crypto helpers
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

The full v0.1 route list is maintained in [docs/api/API.md](docs/api/API.md) and [docs/api/openapi.yaml](docs/api/openapi.yaml). The implemented surfaces include public health/auth/docs/MCP routes and private identity, user, team, project, member, service, branch, draft, contract, endpoint, diff, and MCP token routes.

Responses use a project envelope. HTTP status is currently always `200`; semantic success or failure is represented by the JSON `code` and `status` fields.

## Configuration

Configuration is loaded from `config.yaml`, defaults, and `VDOC_` environment variables.

Examples:

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

When `database.enabled=true`, Vdoc connects to PostgreSQL during startup, creates its runtime tables, and loads existing state. Connection or migration failure aborts startup instead of silently falling back to memory. When `storage.enabled=true`, raw and normalized OpenAPI schemas are written to RustFS or any S3-compatible object storage; the bucket is created automatically when missing.

Config file lookup order:

1. Program directory
2. Working directory
3. `/etc/vdoc/`

## Documentation

- [Roadmap and improvements](IMPROVEMENTS.md)
- [中文 README](README.zh-CN.md)
- [中文路线图](IMPROVEMENTS.zh-CN.md)

## Contributing

Issues and pull requests are welcome. Since the project is early, please keep changes aligned with the MVP scope: OpenAPI contracts, immutable versions, endpoint indexes, semantic diff, and MCP integration.

## License

[MIT License](LICENSE)
