# Vdoc v0.1 E2E Tests

This package drives the v0.1 backend through real Gin handlers and the `services/vdoc` service layer. The default path is in-memory and is safe for normal `go test ./...` and `make verify` runs.

## Quick Run

```sh
./scripts/vdoc-e2e.sh help
go test ./... -run TestVdocV01EndToEnd -count=1 -v
go test ./... -run TestVdocV01FailureMatrix -count=1 -v
./scripts/vdoc-e2e.sh all
```

The happy path writes `.sisyphus/evidence/task-17-e2e-happy-path.json`. The failure matrix writes `.sisyphus/evidence/task-17-failure-matrix.txt`. Evidence includes IDs, counts, statuses, and summaries only; it must not include JWTs, MCP token secrets, passwords, storage credentials, Authorization headers, or raw OpenAPI schema bodies.

## Live PostgreSQL And RustFS/S3

Live persistence is opt-in. Default tests do not connect to PostgreSQL or object storage. Use a disposable test database because the live E2E setup resets the PostgreSQL `public` schema before migrations.

For the normal local closure path, start from the workspace root:

```sh
scripts/vdoc-local-bootstrap.sh
docker compose --env-file .env up -d --build
cd Vdoc && go run ./tools/vdoc-demo-seed
```

The demo seed is optional. It creates local data for demos and smoke checks after the backend is healthy.

On a fresh Docker Compose volume, Postgres runs `scripts/postgres-init-e2e-db.sh` through `/docker-entrypoint-initdb.d/` and creates the disposable live E2E database. `VDOC_TEST_POSTGRES_DB` defaults to `vdoc_e2e`. Postgres init scripts only run when the volume is first initialized. If you keep an existing `postgres-data` volume, create the disposable database manually or reset only disposable local state with `docker compose down -v` before starting with a fresh volume.

Required live data variables:

```sh
export VDOC_TEST_DATABASE_DSN="postgres://vdoc:<password>@127.0.0.1:5432/vdoc_e2e?sslmode=disable"
export VDOC_TEST_STORAGE_ENDPOINT="127.0.0.1:9000"
export VDOC_TEST_STORAGE_BUCKET="vdoc-e2e"
export VDOC_TEST_STORAGE_ACCESS_KEY="<access-key>"
export VDOC_TEST_STORAGE_SECRET_KEY="<secret-key>"
```

Optional variables:

```sh
export VDOC_TEST_STORAGE_REGION="us-east-1"
export VDOC_TEST_STORAGE_USE_SSL=false
export VDOC_TEST_STORAGE_PATH_STYLE=true
```

Run the live path with:

```sh
./scripts/vdoc-e2e.sh live-check
./scripts/vdoc-e2e.sh live
```

`live-check` validates the same required variables and exits before starting Go tests. Missing variables exit with code `2` and list only the missing `VDOC_TEST_*` names.

If the root Docker Compose stack is already running, the script can derive the live test variables from the root Compose env file without starting Docker Compose:

```sh
./scripts/vdoc-e2e.sh live-compose --env-file ../.env --check-only
./scripts/vdoc-e2e.sh live-compose --env-file ../.env
```

`live-compose` reads `VDOC_POSTGRES_USER`, `VDOC_POSTGRES_PASSWORD`, `VDOC_POSTGRES_HOST_PORT`, `VDOC_RUSTFS_HOST_PORT`, and `VDOC_STORAGE_*`, derives the host-side PostgreSQL DSN and RustFS endpoint, and then uses the same live E2E path. It does not use `VDOC_POSTGRES_DB` as the live E2E database because that is the application database. `VDOC_TEST_POSTGRES_DB` defaults to disposable `vdoc_e2e`; override it if needed:

```sh
VDOC_TEST_POSTGRES_DB=vdoc_ci_e2e ./scripts/vdoc-e2e.sh live-compose --env-file ../.env --check-only
```

The live E2E setup resets the selected database's `public` schema before migrations, so never point `VDOC_TEST_POSTGRES_DB` or `VDOC_TEST_DATABASE_DSN` at an application database. Scripted live modes refuse application database reuse before Go tests start. `--check-only` validates the derived environment without running Go tests. The success output lists variable names plus the non-secret test database name; it does not print raw credentials. Do not copy raw JWTs, MCP tokens, DB passwords, storage secrets, or `Authorization` header values into evidence files, logs, screenshots, or issues.

When invoked through `./scripts/vdoc-e2e.sh live` or `./scripts/vdoc-e2e.sh live-compose`, missing required live variables fail fast before Go tests start. If the Go test is invoked directly without the script, missing live variables are reported as skipped tests. `testing.Short()` also skips the live test explicitly.

For direct Go test invocation, also set `VDOC_E2E_LIVE=1`; the script sets it automatically for `live` and `live-compose`.

After E2E, run the workspace release dry-run from the root as the local gate:

```sh
scripts/vdoc-release-dry-run.sh --list
scripts/vdoc-release-dry-run.sh
```

The dry-run does not publish packages or deploy services.
