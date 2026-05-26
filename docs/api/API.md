# Vdoc v0.1 API Guide

This guide is the human-readable companion to [openapi.yaml](openapi.yaml). The examples use raw `Authorization` header values. Do not prefix JWTs or MCP tokens with `Bearer`.

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
```

## Vdoc Response Envelope

REST handlers return HTTP 200 for both success and application errors. The semantic result is inside the JSON body.

| Field | Meaning |
|---|---|
| `code` | Semantic status code such as `200`, `400`, `401`, `403`, `404`, `409`, or `500`. |
| `status` | Semantic status text such as `OK`, `INVALID_ARGUMENT`, `UNAUTHENTICATED`, `PERMISSION_DENIED`, `NOT_FOUND`, or `INTERNAL`. |
| `detail` | Optional result body. |
| `total` | Optional list count on list endpoints. |

## Roles

Private project APIs use SuperAdmin plus project Reader, Writer, and Admin roles. Reader can query, Writer can upload draft and submit, and Admin can approve or reject.

Projects directly own typed Documents. `document_type` is `1` for OpenAPI and `2` for Markdown. `relative_path` is the document path identity stored by Vdoc; display names can change without creating a second persisted path/name identity.

## Route Categories

| Category | Purpose |
|---|---|
| Open | Health, register/login, OpenAPI YAML, and MCP JSON-RPC. |
| Identity | Current JWT user identity. |
| System Users | SuperAdmin user lifecycle and user MCP token oversight. |
| Teams | Team lifecycle. |
| Projects | Project lifecycle and membership. |
| Documents | Project document lifecycle. |
| Branches | Document branch lifecycle. |
| Drafts | Draft creation, update, submission, review, and promotion. |
| Versions | Published document versions and stored raw/normalized or stable content. |
| Endpoints | Parsed endpoint list and detail from published versions. |
| Diffs | Semantic version comparison and summaries. |
| MCP Tokens | User MCP token lifecycle. |

## v0.1 Workflows

The scriptable smoke path covers register/login, create project/document, upload draft, submit, approve, query endpoint, compare diff, create MCP token, MCP tools/list, and MCP tools/call.

### 1. Create Team, Project, And Document

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

DOCUMENT_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d '{"name":"petstore","document_type":1,"relative_path":"apis/petstore.yaml","description":"Docs sample document"}')
DOCUMENT_ID=$(printf '%s' "$DOCUMENT_RESPONSE" | jq -r '.detail.id')

BRANCH_ID=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/branches" \
  -H "Authorization: $JWT" | jq -r '.detail[] | select(.name=="dev") | .id')
```

Document creation creates `dev`, `test`, and protected `prod` branches. Admins can add `feature/*` branches when needed.

### 2. Upload Draft, Submit, And Approve

Create a draft by posting OpenAPI 3.0 or 3.1 content as `schema_content`. Markdown documents use the same private REST draft routes and may post Markdown text as `schema_content` or `content`; MCP Markdown draft tools use `markdown_content`. `content_kind` accepts `raw` or `normalized` for OpenAPI content and `raw` or `stable` for Markdown content.

```sh
DRAFT_ONE_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/drafts" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d "{\"branch_id\":\"$BRANCH_ID\",\"version_name\":\"1.0.0\",\"schema_content\":$SCHEMA_V1}")
DRAFT_ONE_ID=$(printf '%s' "$DRAFT_ONE_RESPONSE" | jq -r '.detail.id')

curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/drafts/$DRAFT_ONE_ID/content/raw" -H "Authorization: $JWT"
curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/drafts/$DRAFT_ONE_ID/content/normalized" -H "Authorization: $JWT"
curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/drafts/$DRAFT_ONE_ID/submit" -X POST -H "Authorization: $JWT"

VERSION_ONE_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/drafts/$DRAFT_ONE_ID/approve" \
  -X POST -H "Authorization: $JWT")
VERSION_ONE_ID=$(printf '%s' "$VERSION_ONE_RESPONSE" | jq -r '.detail.id')
```

Other review and promotion calls use:

```text
POST /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/request-changes
POST /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/reject
POST /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/promote
```

### 3. Query Versions, Endpoints, And Diffs

```sh
curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/versions/$VERSION_ONE_ID/content/raw" -H "Authorization: $JWT"
curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/versions/$VERSION_ONE_ID/content/normalized" -H "Authorization: $JWT"

ENDPOINTS_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/versions/$VERSION_ONE_ID/endpoints?path=/pets" -H "Authorization: $JWT")
ENDPOINT_ID=$(printf '%s' "$ENDPOINTS_RESPONSE" | jq -r '.detail[0].id')
curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/versions/$VERSION_ONE_ID/endpoints/$ENDPOINT_ID" -H "Authorization: $JWT"

DIFF_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/diffs" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d "{\"from_version_id\":\"$VERSION_ONE_ID\",\"to_version_id\":\"$VERSION_TWO_ID\"}")
DIFF_ID=$(printf '%s' "$DIFF_RESPONSE" | jq -r '.detail.id')
curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/diffs/$DIFF_ID/summary" -H "Authorization: $JWT"
```

### 4. MCP Tokens And Tools

```sh
MCP_TOKEN_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/mcp-tokens" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d '{"name":"docs-agent","scopes":[1,2]}')
MCP_TOKEN=$(printf '%s' "$MCP_TOKEN_RESPONSE" | jq -r '.detail.token')

curl -sS "$API_BASE/api/v1/open/mcp" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":"tools-list","method":"tools/list"}'

curl -sS "$API_BASE/api/v1/open/mcp" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $MCP_TOKEN" \
  -d "{\"jsonrpc\":\"2.0\",\"id\":\"endpoint-detail\",\"method\":\"tools/call\",\"params\":{\"name\":\"get_endpoint_detail\",\"arguments\":{\"project_id\":\"$PROJECT_ID\",\"document_id\":\"$DOCUMENT_ID\",\"version_id\":\"$VERSION_ONE_ID\",\"endpoint_id\":\"$ENDPOINT_ID\"}}}"
```

The token value in `.detail.token` is a one time copyable secret returned only by token creation. List, get, and revoke token responses are redacted.

Current MCP tools include `list_projects`, `list_documents`, `list_api_versions`, `get_latest_schema`, `get_endpoint_detail`, `compare_api_versions`, `get_change_summary`, `create_api_version_draft`, `update_api_version_draft`, `submit_api_version_draft`, `get_api_version_draft`, `get_latest_doc`, `compare_doc_versions`, `create_doc_draft`, `update_doc_draft`, `submit_doc_draft`, and `get_doc_draft`. Direct publish tools are intentionally not exposed in v0.1.

## Route Reference

| Category | Methods | Path | Auth |
|---|---|---|---|
| Open | `GET` | `/api/v1/open/health` | Public |
| Open | `POST` | `/api/v1/open/auth/register` | Public |
| Open | `POST` | `/api/v1/open/auth/login` | Public |
| Open | `GET` | `/api/v1/open/docs/openapi.yaml` | Public |
| Open | `POST` | `/api/v1/open/mcp` | MCP token |
| Identity | `GET` | `/api/v1/private/identity/me` | JWT |
| System Users | `GET`, `POST` | `/api/v1/private/system/users` | JWT |
| System Users | `PATCH` | `/api/v1/private/system/users/{user_id}` | JWT |
| System Users | `GET` | `/api/v1/private/system/users/{user_id}/mcp-tokens` | JWT |
| System Users | `POST` | `/api/v1/private/system/users/{user_id}/mcp-tokens/{token_id}/revoke` | JWT |
| Teams | `GET`, `POST` | `/api/v1/private/teams` | JWT |
| Teams | `GET`, `PATCH` | `/api/v1/private/teams/{team_id}` | JWT |
| Teams | `POST` | `/api/v1/private/teams/{team_id}/archive` | JWT |
| Projects | `GET`, `POST` | `/api/v1/private/projects` | JWT |
| Projects | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}` | JWT |
| Projects | `POST` | `/api/v1/private/projects/{project_id}/archive` | JWT |
| Projects | `GET`, `POST` | `/api/v1/private/projects/{project_id}/members` | JWT |
| Projects | `DELETE` | `/api/v1/private/projects/{project_id}/members/{user_id}` | JWT |
| Projects | `PATCH` | `/api/v1/private/projects/{project_id}/members/{user_id}/role` | JWT |
| Documents | `GET`, `POST` | `/api/v1/private/projects/{project_id}/documents` | JWT |
| Documents | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}/documents/{document_id}` | JWT |
| Documents | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/archive` | JWT |
| Branches | `GET`, `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/branches` | JWT |
| Branches | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}/documents/{document_id}/branches/{branch_id}` | JWT |
| Branches | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/branches/{branch_id}/archive` | JWT |
| Drafts | `GET`, `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts` | JWT |
| Drafts | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}` | JWT |
| Drafts | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/content/{content_kind}` | JWT |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/submit` | JWT |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/approve` | JWT |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/request-changes` | JWT |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/reject` | JWT |
| Drafts | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts/promote` | JWT |
| Versions | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/versions` | JWT |
| Versions | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/versions/{version_id}` | JWT |
| Versions | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/versions/{version_id}/content/{content_kind}` | JWT |
| Endpoints | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/versions/{version_id}/endpoints` | JWT |
| Endpoints | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/versions/{version_id}/endpoints/{endpoint_id}` | JWT |
| Diffs | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/diffs` | JWT |
| Diffs | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/diffs/{diff_id}` | JWT |
| Diffs | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/diffs/{diff_id}/summary` | JWT |
| MCP Tokens | `GET`, `POST` | `/api/v1/private/mcp-tokens` | JWT |
| MCP Tokens | `GET` | `/api/v1/private/mcp-tokens/{token_id}` | JWT |
| MCP Tokens | `POST` | `/api/v1/private/mcp-tokens/{token_id}/revoke` | JWT |
