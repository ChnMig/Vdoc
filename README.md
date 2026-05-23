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

This repository currently contains the Go/Gin backend foundation for Vdoc.

Implemented today:

- Gin HTTP server scaffold
- Versioned route tree under `/api/v1`
- Health endpoint: `GET /api/v1/open/health`
- Unified JSON response envelope with `trace_id` and `timestamp`
- Request tracing, structured access logs, panic recovery, CORS, security headers, body-size limit, and rate-limit middleware
- Viper-based configuration with `VDOC_` environment variables
- JWT helpers and startup safety checks for insecure keys
- Makefile targets for build, test, lint, format, and cross-platform packaging

Not implemented yet:

- Team, project, member, and role management
- OpenAPI upload and storage
- Contract versioning
- Endpoint index parsing
- Semantic API diff
- MCP tools
- Code generation and frontend integration helpers

## Product Concepts

| Concept | Meaning |
|---|---|
| Team | A collaboration boundary for people and projects. |
| Project | A product or app owned by a team. |
| Service | A backend service inside a project, such as `user-service` or `order-service`. |
| Contract Version | An immutable OpenAPI snapshot for a service. |
| Endpoint Index | A structured database index of paths, methods, parameters, request bodies, responses, tags, and operation IDs. |
| Semantic Diff | Contract-aware comparison between two versions, not raw text diff. |
| Breaking Change | A change that can break frontend consumers, such as field removal, type change, new required parameter, or endpoint removal. |
| MCP Token | A user-bound AI tool token that users can view, copy, generate, and revoke in the backend; effective permissions come from token scopes plus the user's role on the target project. |

## MVP Workflow

1. SuperAdmin creates system members, teams, and projects.
2. SuperAdmin assigns the initial Project Admin.
3. Project Admin invites members and assigns project-level roles.
4. Project Admin or Writer creates a service.
5. Backend developer uploads, or AI submits an OpenAPI 3.x draft through MCP.
6. Vdoc validates and stores the raw schema.
7. Project Admin approves the draft and Vdoc creates an immutable contract version.
8. Vdoc parses an endpoint index for fast query and display.
9. Vdoc compares the new version with the previous one.
10. Vdoc stores a change summary and breaking-change list.
11. Frontend developers and AI agents query endpoint details, diffs, and summaries.

## MVP Scope

Planned for the first usable version:

- System-level `SuperAdmin`; project-level roles: `Reader`, `Writer`, `Admin`, where Writer submits drafts and Admin reviews/publishes them
- OpenAPI 3.x upload through Web API, plus MCP draft submission and updates
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

## Planned MCP Tools

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

Direct publish tools later:

```text
publish_api_schema
publish_api_version
```

## Architecture Direction

```text
Web App
  - Team / project / member / role management
  - API documentation view
  - Version list
  - Semantic diff view
  - Breaking-change summary

API Server
  - Project management
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
  - PostgreSQL for metadata, endpoint indexes, and diff summaries
  - RustFS for raw and normalized OpenAPI snapshots and large diff snapshots
  - Redis or queue for parse, diff, and later codegen jobs
```

## Repository Structure

```text
vdoc/
├── main.go                  # Server lifecycle, CLI flags, config, logging, graceful shutdown
├── Makefile                 # Build, run, test, format, lint, verify
├── api/                     # Gin setup, middleware, response envelope, versioned routes
├── common/                  # Shared DTO/type placeholder
├── config/                  # Viper config loading, defaults, hot reload, safety checks
├── db/                      # Data-access placeholder
├── domain/health/           # Current health-domain example
├── services/                # Application-service placeholder
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

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/open/health` | Service health and readiness status. |

Responses use a project envelope. HTTP status is currently always `200`; semantic success or failure is represented by the JSON `code` and `status` fields.

## Configuration

Configuration is loaded from `config.yaml`, defaults, and `VDOC_` environment variables.

Examples:

```bash
export VDOC_SERVER_PORT=9090
export VDOC_JWT_KEY="$(openssl rand -base64 32)"
export VDOC_LOG_LEVEL=info
```

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
