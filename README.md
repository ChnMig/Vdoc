# Vdoc

Languages: [English](README.md) | [简体中文](README.zh-CN.md)

AI-friendly API contract hub with OpenAPI versioning, semantic diff, and MCP.

Vdoc helps AI/vibe-coding teams keep backend API changes, frontend integration work, and AI agent context in sync.

## What Is Vdoc?

Vdoc is an API contract collaboration platform for fast-moving teams. It treats OpenAPI as the source of truth, stores every published contract as an immutable version, computes semantic diffs between versions, and exposes contract knowledge to both humans and AI agents.

The long-term goal is not to build another Swagger UI clone. Vdoc focuses on the workflow that often breaks in AI-assisted development:

```text
Backend or AI publishes OpenAPI
        -> Vdoc creates an immutable version
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
| MCP Token | A scoped token used by AI tools to query or publish API contracts through MCP. |

## MVP Workflow

1. Admin creates a team.
2. Admin creates a project.
3. Admin invites members and assigns project-level roles.
4. Admin or backend developer creates a service.
5. Backend developer or AI uploads an OpenAPI 3.x document.
6. Vdoc validates and stores the raw schema.
7. Vdoc creates an immutable contract version.
8. Vdoc parses an endpoint index for fast query and display.
9. Vdoc compares the new version with the previous one.
10. Vdoc stores a change summary and breaking-change list.
11. Frontend developers and AI agents query endpoint details, diffs, and summaries.

## MVP Scope

Planned for the first usable version:

- Project-level roles: `Reader`, `Writer`, `Admin`
- OpenAPI 3.x upload through Web API and later MCP
- Immutable contract versions per service
- Endpoint list and endpoint detail query
- Version comparison with semantic diff
- Breaking-change summary
- Read-only MCP tools first, write tools later

Explicitly out of scope for the first MVP:

- Complex organization-wide RBAC
- GraphQL, gRPC, Postman, YApi, or Apifox import
- Full SDK generation platform
- Automatic modification of frontend repositories
- Complex approval workflows

## Planned MCP Tools

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
  - Contract version creation
  - Permission checks
  - Diff query
  - MCP token management

MCP Server
  - AI contract lookup
  - AI version diff lookup
  - AI OpenAPI publish/update
  - AI frontend change summaries

Diff Engine
  - OpenAPI parse
  - Schema normalization
  - Contract model extraction
  - Semantic diff
  - Breaking-change rules

Storage
  - PostgreSQL for metadata, endpoint indexes, and diff summaries
  - Object storage for raw and normalized OpenAPI snapshots
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

- [Product requirements](PRD.md)
- [Roadmap and improvements](IMPROVEMENTS.md)
- [中文 README](README.zh-CN.md)
- [中文路线图](IMPROVEMENTS.zh-CN.md)

## Contributing

Issues and pull requests are welcome. Since the project is early, please keep changes aligned with the MVP scope: OpenAPI contracts, immutable versions, endpoint indexes, semantic diff, and MCP integration.

## License

[MIT License](LICENSE)
