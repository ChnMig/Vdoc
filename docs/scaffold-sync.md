# HTTP Service Scaffold Sync

Vdoc started from the [`go-template/http-services`](https://github.com/ChnMig/go-template/tree/main/http-services) scaffold, but it is now a product backend rather than a template clone. Scaffold changes are reviewed selectively so generic runtime improvements do not overwrite Vdoc's API, PostgreSQL/RustFS persistence, security, or deployment contracts.

## Reviewed snapshot

- Repository: `https://github.com/ChnMig/go-template.git`
- Path: `http-services/`
- Upstream commit: [`4526389ed6b432b09b18404e5826a937f2255ba4`](https://github.com/ChnMig/go-template/commit/4526389ed6b432b09b18404e5826a937f2255ba4)
- Reviewed on: 2026-08-27

The comparison covered the entrypoint, configuration, middleware, response/logging helpers, PID ownership, Make targets, dependencies, database adapters, and utility packages.

## Integrated or retained

- Explicit default, JSON, and query bind helpers now centralize Gin parameter binding while preserving each handler's existing error disclosure policy.
- Bound request objects are registered under the shared context key, but Vdoc's logging layer deliberately never serializes their values.
- Request trace IDs propagate through both Gin and standard `context.Context`.
- CORS uses a fixed method/header surface, `204` preflight responses, explicit origin allowlists, and exposed trace/download headers.
- Static serving can be disabled; proxy trust, body limits, timeouts, rate limits, and configuration values are validated before startup.
- PID files use exclusive ownership, rollback on partial writes, owner-checked removal, and are disabled under Docker supervision.
- Make verification includes formatting, vet, race tests, build checks, module tidy diff, and module checksum verification.
- The Backend environment example now states that the process does not auto-load `.env`; root Docker Compose owns workspace `.env` loading.

## Intentionally not copied

- MySQL, Redis, and generic migration adapters: Vdoc's supported persistence contract is PostgreSQL plus RustFS/S3.
- `gin.Default()` and framework default recovery/access logs: Vdoc requires its ordered `TraceID -> AccessLog -> Recovery` envelope contract.
- Wildcard CORS and implicit localhost proxy trust: self-hosted deployments must configure exact origins and trusted proxy IP/CIDR values.
- Full query, form, bound-parameter, or response-detail logging: those values can contain passwords, tokens, API keys, private documents, and share capabilities.
- Mutable config hot reload: Vdoc validates changed files but requires restart, preventing partial component updates and data races.
- UUIDv7-MD5 helpers and the standalone task-group package: no current Vdoc runtime consumer needs them, so adding unused infrastructure would increase maintenance surface without product value.
- Template MySQL migration CLI and placeholder directories: database migrations are part of Vdoc's PostgreSQL startup lifecycle.

Future syncs should compare against the immutable commit recorded above, update this snapshot only after review, and keep every rejected item rejected unless Vdoc's product contract changes.
