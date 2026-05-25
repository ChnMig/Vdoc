# Vdoc v0.1 API Guide

This guide is the human-readable companion to the machine spec at [openapi.yaml](openapi.yaml). It follows the checked-in OpenAPI contract and the current Gin routes for Vdoc v0.1.

The examples use raw `Authorization` header values. Do not prefix JWTs or MCP tokens with `Bearer`.

## Base URLs

| Surface | Base |
|---|---|
| Public REST | `/api/v1/open` |
| Private REST | `/api/v1/private` |
| OpenAPI document | `/api/v1/open/docs/openapi.yaml` |
| MCP JSON-RPC | `/api/v1/open/mcp` |

## Authentication

Public auth starts with register/login. Private REST calls use the JWT returned in the Vdoc envelope. MCP calls use an MCP token created through the private REST API.

```sh
API_BASE="${API_BASE:-http://127.0.0.1:8080}"
PASSWORD="sample-password-change-me"

REGISTER_RESPONSE=$(curl -sS "$API_BASE/api/v1/open/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"email":"docs-admin@example.test","name":"Docs Admin","password":"sample-password-change-me"}')

ADMIN_USER_ID=$(printf '%s' "$REGISTER_RESPONSE" | jq -r '.detail.user.id')
JWT=$(printf '%s' "$REGISTER_RESPONSE" | jq -r '.detail.token')

LOGIN_RESPONSE=$(curl -sS "$API_BASE/api/v1/open/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"docs-admin@example.test","password":"sample-password-change-me"}')

JWT=$(printf '%s' "$LOGIN_RESPONSE" | jq -r '.detail.token')
```

Use the JWT on private routes:

```sh
curl -sS "$API_BASE/api/v1/private/identity/me" \
  -H "Authorization: $JWT"
```

Use the MCP token on MCP JSON-RPC:

```sh
curl -sS "$API_BASE/api/v1/open/mcp" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":"tools","method":"tools/list"}'
```

## Vdoc Response Envelope

REST handlers return HTTP 200 for both success and application errors. The semantic result is inside the JSON body.

| Field | Meaning |
|---|---|
| `code` | Semantic status code, such as `200`, `400`, `401`, `403`, `404`, `409`, `429`, or `500`. |
| `status` | Semantic status text, such as `OK`, `INVALID_ARGUMENT`, `UNAUTHENTICATED`, `PERMISSION_DENIED`, `NOT_FOUND`, `ABORTED`, or `INTERNAL`. |
| `description` | Stable description for the status. |
| `message` | Optional handler message for the caller. |
| `trace_id` | Optional request trace ID from middleware. |
| `timestamp` | Unix timestamp from the response helper. |
| `detail` | Optional result body or error detail. |
| `total` | Optional list count on list endpoints. |

Success example:

```json
{
  "code": 200,
  "status": "OK",
  "description": "No error",
  "trace_id": "trace-docs-001",
  "timestamp": 1780000000,
  "detail": {
    "id": "project_123",
    "name": "Docs Project"
  }
}
```

Application error example:

```json
{
  "code": 403,
  "status": "PERMISSION_DENIED",
  "description": "Client does not have sufficient permission",
  "message": "没有权限",
  "trace_id": "trace-docs-002",
  "timestamp": 1780000001
}
```

MCP JSON-RPC also returns HTTP 200. JSON-RPC errors use `error.code` and put the mapped Vdoc status in `error.data.status`.

## Roles And Permissions

| Role | Scope | Can do |
|---|---|---|
| `SuperAdmin` | System | Register first user, create users, change user status, create teams, create projects, assign the first project admin, read every project, and oversee user MCP tokens. |
| `Reader` | Project | Read project data, services, branches, drafts, published versions, schemas, endpoints, diffs, and summaries. |
| `Writer` | Project | Everything Reader can do, plus create drafts, update drafts, and submit drafts for review. |
| `Admin` | Project | Everything Writer can do, plus manage project members, services, branches, review drafts, approve or reject drafts, request changes, promote drafts, and publish immutable versions by approval. |

MCP effective permission is the intersection of token scope and project role:

| MCP scope | Needs role | Tools enabled |
|---|---|---|
| `api:read` value `1` | Reader, Writer, Admin, or SuperAdmin | `list_projects`, `list_services`, `list_api_versions`, `get_latest_schema`, `get_endpoint_detail`, `compare_api_versions`, `get_change_summary`, `get_api_version_draft`. |
| `api:draft` value `2` | Writer, Admin, or SuperAdmin | `create_api_version_draft`, `update_api_version_draft`, `submit_api_version_draft`. |

A token with `api:draft` still fails draft writes if the token owner is only a Reader on that project. A token with `api:read` cannot write drafts even if the owner is an Admin.

v0.1 MCP does not provide direct publish tools. `publish_api_schema` and `publish_api_version` are not valid MCP tools in v0.1. Publish still requires the REST review path where an Admin approves a submitted draft.

## Route Categories

| Category | Purpose |
|---|---|
| Open | Health, register/login, OpenAPI YAML, and MCP JSON-RPC. |
| Identity | Current JWT user identity. |
| System Users | SuperAdmin user lifecycle and user MCP token oversight. |
| Teams | Team lifecycle. |
| Projects | Project lifecycle and membership. |
| Services | API service lifecycle. |
| Branches | Service contract branch lifecycle. |
| Contracts | Published contract versions and stored schemas. |
| Drafts | Draft creation, update, submission, review, and promotion. |
| Endpoints | Parsed endpoint list and detail from published versions. |
| Diffs | Semantic version comparison and summaries. |
| MCP Tokens | User MCP token lifecycle. |

## v0.1 Workflows

The scriptable smoke path covers register/login, create project/service, upload draft, submit, approve, query endpoint, compare diff, create MCP token, MCP tools/list, and MCP tools/call.

### 1. Create Team, Project, And Service

The first registered user is a SuperAdmin. Create a team, create a project, and set the same user as the first project Admin.

```sh
TEAM_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/teams" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d '{"name":"Docs Team","description":"API docs smoke team"}')
TEAM_ID=$(printf '%s' "$TEAM_RESPONSE" | jq -r '.detail.id')

PROJECT_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d "{\"team_id\":\"$TEAM_ID\",\"name\":\"Docs Project\",\"description\":\"API docs smoke project\",\"admin_user_id\":\"$ADMIN_USER_ID\"}")
PROJECT_ID=$(printf '%s' "$PROJECT_RESPONSE" | jq -r '.detail.id')

SERVICE_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d '{"name":"petstore","display_name":"Petstore","description":"Docs sample service","base_path":"/petstore"}')
SERVICE_ID=$(printf '%s' "$SERVICE_RESPONSE" | jq -r '.detail.id')

BRANCH_ID=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/branches" \
  -H "Authorization: $JWT" | jq -r '.detail[] | select(.name=="dev") | .id')
```

Service creation creates `dev`, `test`, and protected `prod` branches. Admins can add `feature/*` branches when needed.

### 2. Upload Draft, Submit, And Approve

Create a draft by posting OpenAPI 3.0 or 3.1 content as `schema_content`. `schema_kind` accepts exactly `raw` or `normalized` for draft and version schema retrieval.

```sh
SCHEMA_V1=$(cat <<'JSON'
{"openapi":"3.1.0","info":{"title":"Docs Pet API","version":"1.0.0"},"paths":{"/pets":{"get":{"operationId":"listPets","summary":"List pets","responses":{"200":{"description":"OK","content":{"application/json":{"schema":{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}}}}}}}}}}
JSON
)

DRAFT_ONE_RESPONSE=$(jq -n \
  --arg branch_id "$BRANCH_ID" \
  --arg schema_content "$SCHEMA_V1" \
  '{branch_id:$branch_id,version_name:"1.0.0",changelog:"Initial pet list",source_git_commit_id:"abc1234",schema_content:$schema_content}' | \
  curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts" \
    -H 'Content-Type: application/json' \
    -H "Authorization: $JWT" \
    -d @-)
DRAFT_ONE_ID=$(printf '%s' "$DRAFT_ONE_RESPONSE" | jq -r '.detail.id')

curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts/$DRAFT_ONE_ID/schemas/raw" \
  -H "Authorization: $JWT"

curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts/$DRAFT_ONE_ID/schemas/normalized" \
  -H "Authorization: $JWT"

curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts/$DRAFT_ONE_ID/submit" \
  -H "Authorization: $JWT" \
  -X POST

VERSION_ONE_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts/$DRAFT_ONE_ID/approve" \
  -H "Authorization: $JWT" \
  -X POST)
VERSION_ONE_ID=$(printf '%s' "$VERSION_ONE_RESPONSE" | jq -r '.detail.id')
```

Approval creates an immutable published contract version. Later edits create a new draft and a new version instead of mutating a published version.

Admins can also request changes or reject a submitted draft:

```sh
curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts/$DRAFT_ID/request-changes" \
  -H "Authorization: $JWT" \
  -X POST

curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts/$DRAFT_ID/reject" \
  -H "Authorization: $JWT" \
  -X POST
```

Promote copies the latest published schema from one branch into a draft on another branch:

```sh
PROMOTE_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts/promote" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d "{\"source_branch_id\":\"$BRANCH_ID\",\"target_branch_id\":\"$TARGET_BRANCH_ID\",\"version_name\":\"1.0.0-test\",\"changelog\":\"Promote dev to test\"}")
PROMOTED_DRAFT_ID=$(printf '%s' "$PROMOTE_RESPONSE" | jq -r '.detail.id')
```

### 3. Query Published Schemas And Endpoints

Published version schemas use the same `schema_kind` values, `raw` and `normalized`.

```sh
curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contracts/$VERSION_ONE_ID/schemas/raw" \
  -H "Authorization: $JWT"

curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contracts/$VERSION_ONE_ID/schemas/normalized" \
  -H "Authorization: $JWT"
```

Query the parsed endpoint index, then fetch endpoint detail.

```sh
ENDPOINTS_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contracts/$VERSION_ONE_ID/endpoints?path=/pets" \
  -H "Authorization: $JWT")
ENDPOINT_ID=$(printf '%s' "$ENDPOINTS_RESPONSE" | jq -r '.detail[0].id')

curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contracts/$VERSION_ONE_ID/endpoints/$ENDPOINT_ID" \
  -H "Authorization: $JWT"
```

### 4. Compare Versions And Read Diff Summary

Create another draft with a changed OpenAPI document, approve it, and compare the two published versions.

```sh
SCHEMA_V2=$(cat <<'JSON'
{"openapi":"3.1.0","info":{"title":"Docs Pet API","version":"1.1.0"},"paths":{"/pets":{"get":{"operationId":"listPets","summary":"List pets","responses":{"200":{"description":"OK","content":{"application/json":{"schema":{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}},"next_page_token":{"type":"string"}}}}}}}}},"/pets/{pet_id}":{"get":{"operationId":"getPet","summary":"Get pet","parameters":[{"name":"pet_id","in":"path","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"OK"}}}}}}
JSON
)

DRAFT_TWO_RESPONSE=$(jq -n \
  --arg branch_id "$BRANCH_ID" \
  --arg schema_content "$SCHEMA_V2" \
  '{branch_id:$branch_id,version_name:"1.1.0",changelog:"Add pet detail",schema_content:$schema_content}' | \
  curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts" \
    -H 'Content-Type: application/json' \
    -H "Authorization: $JWT" \
    -d @-)
DRAFT_TWO_ID=$(printf '%s' "$DRAFT_TWO_RESPONSE" | jq -r '.detail.id')

curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts/$DRAFT_TWO_ID/submit" \
  -H "Authorization: $JWT" \
  -X POST

VERSION_TWO_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/contract-drafts/$DRAFT_TWO_ID/approve" \
  -H "Authorization: $JWT" \
  -X POST)
VERSION_TWO_ID=$(printf '%s' "$VERSION_TWO_RESPONSE" | jq -r '.detail.id')

DIFF_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/diffs" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d "{\"from_version_id\":\"$VERSION_ONE_ID\",\"to_version_id\":\"$VERSION_TWO_ID\"}")
DIFF_ID=$(printf '%s' "$DIFF_RESPONSE" | jq -r '.detail.id')

curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/services/$SERVICE_ID/diffs/$DIFF_ID/summary" \
  -H "Authorization: $JWT"
```

Treat `is_breaking: true` or `must_handle: true` as work the frontend must handle before release. Examples include endpoint removal, new required parameters, removed response fields, type changes, and removed enum values. Optional changes, such as added endpoints, new response statuses, or added optional response fields, can be adopted later.

### 5. Create MCP Token And Call MCP

Create an MCP token from a private REST session. The raw token is returned on create and on direct token reveal, but list responses redact it.

```sh
MCP_TOKEN_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/mcp-tokens" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d '{"name":"docs-agent","scopes":[1,2]}')
MCP_TOKEN=$(printf '%s' "$MCP_TOKEN_RESPONSE" | jq -r '.detail.token')
```

List tools:

```sh
curl -sS "$API_BASE/api/v1/open/mcp" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":"tools-list","method":"tools/list"}'
```

Call a tool:

```sh
curl -sS "$API_BASE/api/v1/open/mcp" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $MCP_TOKEN" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":\"endpoint-detail\",\"method\":\"tools/call\",\"params\":{\"name\":\"get_endpoint_detail\",\"arguments\":{\"project_id\":\"$PROJECT_ID\",\"service_id\":\"$SERVICE_ID\",\"version_id\":\"$VERSION_ONE_ID\",\"endpoint_id\":\"$ENDPOINT_ID\"}}}"
```

The v0.1 MCP tool set is:

| Tool | Scope | Purpose |
|---|---|---|
| `list_projects` | `api:read` | List visible projects. |
| `list_services` | `api:read` | List services in a project. |
| `list_api_versions` | `api:read` | List published versions for a service. |
| `get_latest_schema` | `api:read` | Get the latest raw schema for a service, optionally by branch. |
| `get_endpoint_detail` | `api:read` | Get one parsed endpoint from a published version. |
| `compare_api_versions` | `api:read` | Compare two published versions. |
| `get_change_summary` | `api:read` | Split diff items into `must_handle`, `breaking`, `optional`, and `non_breaking`. |
| `create_api_version_draft` | `api:draft` | Create a draft from OpenAPI content. |
| `update_api_version_draft` | `api:draft` | Update an editable draft. |
| `submit_api_version_draft` | `api:draft` | Submit a draft for review. |
| `get_api_version_draft` | `api:read` | Read one draft. |

## Route Reference

| Category | Methods | Path | Auth |
|---|---|---|---|
| Open | `GET` | `/api/v1/open/health` | Public |
| Open | `POST` | `/api/v1/open/auth/register` | Public |
| Open | `POST` | `/api/v1/open/auth/login` | Public |
| Open | `GET` | `/api/v1/open/docs/openapi.yaml` | Public |
| Open | `POST` | `/api/v1/open/mcp` | MCP token |
| Identity | `GET` | `/api/v1/private/identity/me` | JWT |
| System Users | `GET`, `POST` | `/api/v1/private/system/users` | JWT, SuperAdmin for mutation |
| System Users | `PATCH` | `/api/v1/private/system/users/{user_id}` | JWT, SuperAdmin |
| System Users | `GET` | `/api/v1/private/system/users/{user_id}/mcp-tokens` | JWT, SuperAdmin |
| System Users | `POST` | `/api/v1/private/system/users/{user_id}/mcp-tokens/{token_id}/revoke` | JWT, SuperAdmin |
| Teams | `GET`, `POST` | `/api/v1/private/teams` | JWT, SuperAdmin for create |
| Teams | `GET`, `PATCH` | `/api/v1/private/teams/{team_id}` | JWT, SuperAdmin for update |
| Teams | `POST` | `/api/v1/private/teams/{team_id}/archive` | JWT, SuperAdmin |
| Projects | `GET`, `POST` | `/api/v1/private/projects` | JWT, SuperAdmin for create |
| Projects | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}` | JWT, Admin for update |
| Projects | `POST` | `/api/v1/private/projects/{project_id}/archive` | JWT, Admin |
| Projects | `GET`, `POST` | `/api/v1/private/projects/{project_id}/members` | JWT, Admin for mutation |
| Projects | `DELETE` | `/api/v1/private/projects/{project_id}/members/{user_id}` | JWT, Admin |
| Projects | `PATCH` | `/api/v1/private/projects/{project_id}/members/{user_id}/role` | JWT, Admin |
| Services | `GET`, `POST` | `/api/v1/private/projects/{project_id}/services` | JWT, Admin for create |
| Services | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}/services/{service_id}` | JWT, Admin for update |
| Services | `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/archive` | JWT, Admin |
| Branches | `GET`, `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/branches` | JWT, Admin for create |
| Branches | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}/services/{service_id}/branches/{branch_id}` | JWT, Admin for update |
| Branches | `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/branches/{branch_id}/archive` | JWT, Admin |
| Contracts | `GET` | `/api/v1/private/projects/{project_id}/services/{service_id}/contracts` | JWT, Reader or higher |
| Contracts | `GET` | `/api/v1/private/projects/{project_id}/services/{service_id}/contracts/{version_id}` | JWT, Reader or higher |
| Contracts | `GET` | `/api/v1/private/projects/{project_id}/services/{service_id}/contracts/{version_id}/schemas/{schema_kind}` | JWT, Reader or higher |
| Drafts | `GET`, `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/contract-drafts` | JWT, Writer for create |
| Drafts | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}/services/{service_id}/contract-drafts/{draft_id}` | JWT, Writer for update |
| Drafts | `GET` | `/api/v1/private/projects/{project_id}/services/{service_id}/contract-drafts/{draft_id}/schemas/{schema_kind}` | JWT, Reader or higher |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/contract-drafts/{draft_id}/submit` | JWT, Writer or Admin |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/contract-drafts/{draft_id}/approve` | JWT, Admin |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/contract-drafts/{draft_id}/request-changes` | JWT, Admin |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/contract-drafts/{draft_id}/reject` | JWT, Admin |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/contract-drafts/promote` | JWT, Admin |
| Endpoints | `GET` | `/api/v1/private/projects/{project_id}/services/{service_id}/contracts/{version_id}/endpoints` | JWT, Reader or higher |
| Endpoints | `GET` | `/api/v1/private/projects/{project_id}/services/{service_id}/contracts/{version_id}/endpoints/{endpoint_id}` | JWT, Reader or higher |
| Diffs | `POST` | `/api/v1/private/projects/{project_id}/services/{service_id}/diffs` | JWT, Reader or higher |
| Diffs | `GET` | `/api/v1/private/projects/{project_id}/services/{service_id}/diffs/{diff_id}` | JWT, Reader or higher |
| Diffs | `GET` | `/api/v1/private/projects/{project_id}/services/{service_id}/diffs/{diff_id}/summary` | JWT, Reader or higher |
| MCP Tokens | `GET`, `POST` | `/api/v1/private/mcp-tokens` | JWT |
| MCP Tokens | `GET` | `/api/v1/private/mcp-tokens/{token_id}` | JWT, owner or SuperAdmin |
| MCP Tokens | `POST` | `/api/v1/private/mcp-tokens/{token_id}/revoke` | JWT, owner or SuperAdmin |
