# Vdoc v0.1 E2E Tests

This package drives the v0.1 backend through real Gin handlers and the `services/vdoc` service layer. The default path is in-memory and is safe for normal `go test ./...` and `make verify` runs.

## Quick Run

```sh
go test ./... -run TestVdocV01EndToEnd -count=1 -v
go test ./... -run TestVdocV01FailureMatrix -count=1 -v
./scripts/vdoc-e2e.sh all
```

The happy path writes `.sisyphus/evidence/task-17-e2e-happy-path.json`. The failure matrix writes `.sisyphus/evidence/task-17-failure-matrix.txt`. Evidence includes IDs, counts, statuses, and summaries only; it must not include JWTs, MCP token secrets, passwords, storage credentials, Authorization headers, or raw OpenAPI schema bodies.

## Live PostgreSQL And RustFS/S3

Live persistence is opt-in. Default tests do not connect to PostgreSQL or object storage. Use a disposable test database because the live E2E setup resets the PostgreSQL `public` schema before migrations.

Required variables:

```sh
export VDOC_E2E_LIVE=1
export VDOC_TEST_DATABASE_DSN="postgres://vdoc:vdoc@127.0.0.1:5432/vdoc_e2e?sslmode=disable"
export VDOC_TEST_STORAGE_ENDPOINT="127.0.0.1:9000"
export VDOC_TEST_STORAGE_BUCKET="vdoc-e2e"
export VDOC_TEST_STORAGE_ACCESS_KEY="rustfs-access-key"
export VDOC_TEST_STORAGE_SECRET_KEY="rustfs-secret-key"
```

Optional variables:

```sh
export VDOC_TEST_STORAGE_REGION="us-east-1"
export VDOC_TEST_STORAGE_USE_SSL=false
export VDOC_TEST_STORAGE_PATH_STYLE=true
```

Run the live path with:

```sh
./scripts/vdoc-e2e.sh live
```

If `VDOC_E2E_LIVE=1` or any required `VDOC_TEST_*` variable is missing, the live test is skipped with a message naming the missing variables and how to enable it. `testing.Short()` also skips the live test explicitly.
