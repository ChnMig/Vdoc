# HTTP Service Scaffold Sync

Vdoc started from the [`go-template/http-services`](https://github.com/ChnMig/go-template/tree/main/http-services) scaffold, but it is now a product backend rather than a template clone. Scaffold changes are reviewed selectively so generic runtime improvements do not overwrite Vdoc's API, PostgreSQL/RustFS persistence, security, or deployment contracts.

## Reviewed snapshot

- Repository: `https://github.com/ChnMig/go-template.git`
- Path: `http-services/`
- Upstream commit: [`f8ab23762a9ddff028bf309f5f7ceb5de7685eb5`](https://github.com/ChnMig/go-template/commit/f8ab23762a9ddff028bf309f5f7ceb5de7685eb5)
- Reviewed on: 2026-09-28
- Previous snapshot: [`4526389ed6b432b09b18404e5826a937f2255ba4`](https://github.com/ChnMig/go-template/commit/4526389ed6b432b09b18404e5826a937f2255ba4), reviewed on 2026-08-27

The comparison covered the entrypoint, configuration, middleware, response/logging helpers, PID ownership, Make targets, dependencies, database adapters, and utility packages.

## September 2026 delta

Two upstream commits changed `http-services/` since the previous snapshot:

- [`0216ab6`](https://github.com/ChnMig/go-template/commit/0216ab67a706784890c36aae08b14bcbe6f59699): added cryptographically random, unpadded Base64URL strings. The helper and length/encoding/invalid-size tests are integrated. Existing MCP and document-share token formats stay compatible.
- [`f8ab237`](https://github.com/ChnMig/go-template/commit/f8ab23762a9ddff028bf309f5f7ceb5de7685eb5): improved request logging and recovery. Vdoc integrates centralized request metadata, standard-context trace fallback with an injected base logger, and clearing stale parameters before rebinding. Connection-abort panics now record a `CANCELLED` outcome without a server-error envelope.

The abort path deliberately propagates `http.ErrAbortHandler` to net/http after the access-log outcome is set. Swallowing that sentinel would let Gin write an empty successful response when the request should terminate. Ordinary panics retain Vdoc's HTTP 200 plus `INTERNAL` envelope, stack reporting, and middleware order.

Vdoc already emits one request-scoped warning through its unified response helpers for parameter validation and JWT rejection. This behavior is retained instead of adding duplicate diagnostics containing raw parser errors. Upstream's raw-body collector, full query/form/bound-parameter logging, and corresponding value-disclosure tests are excluded. Vdoc tests cover successful binding, failed rebinding, safe metadata, credential redaction, unread bodies on authentication rejection, and real HTTP abort behavior. Operational guidance is in [troubleshooting/request-logging.md](troubleshooting/request-logging.md).

This delta does not change dependencies, database migrations, or REST/MCP response contracts for completed requests. The README files explicitly identify the scaffold repository and subdirectory.

## Integrated or retained

- Explicit default, JSON, and query bind helpers now centralize Gin parameter binding while preserving each handler's existing error disclosure policy.
- Bound request objects are registered under the shared context key, but Vdoc's logging layer deliberately never serializes their values.
- Request trace IDs propagate through both Gin and standard `context.Context`.
- CORS uses a fixed method/header surface, `204` preflight responses, explicit origin allowlists or opt-in `*`, and exposed trace/download headers. The standalone deployment uses `*` without credentialed cookies; API authentication is unchanged.
- Static serving can be disabled; proxy trust, body limits, timeouts, rate limits, and configuration values are validated before startup.
- PID files use exclusive ownership, rollback on partial writes, owner-checked removal, and are disabled under Docker supervision.
- Make verification includes formatting, vet, race tests, build checks, module tidy diff, and module checksum verification.
- The Backend environment example now states that the process does not auto-load `.env`; root Docker Compose owns workspace `.env` loading.

## Intentionally not copied

- MySQL, Redis, and generic migration adapters: Vdoc's supported persistence contract is PostgreSQL plus RustFS/S3.
- `gin.Default()` and framework default recovery/access logs: Vdoc requires its ordered `TraceID -> AccessLog -> Recovery` envelope contract.
- Implicit localhost proxy trust: self-hosted deployments must configure trusted proxy IP/CIDR values. Wildcard CORS is now an explicit supported deployment setting, independently of proxy trust.
- Full query, form, bound-parameter, or response-detail logging: those values can contain passwords, tokens, API keys, private documents, and share capabilities.
- Mutable config hot reload: Vdoc validates changed files but requires restart, preventing partial component updates and data races.
- UUIDv7-MD5 helpers and the standalone task-group package: no current Vdoc runtime consumer needs them, so adding unused infrastructure would increase maintenance surface without product value.
- Template MySQL migration CLI and placeholder directories: database migrations are part of Vdoc's PostgreSQL startup lifecycle.

Future syncs should compare against the immutable commit recorded above, update this snapshot only after review, and keep every rejected item rejected unless Vdoc's product contract changes.
