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

Public auth normally starts with login. Anonymous registration is disabled by default and must be explicitly enabled with `VDOC_AUTH_ALLOW_REGISTRATION=true` only for a trusted disposable or pilot environment. Private REST calls use the JWT returned in the Vdoc envelope. MCP calls use an MCP token created through the private REST API.

```sh
API_BASE="${API_BASE:-http://127.0.0.1:8080}"
PASSWORD="sample-password-change-me"

# This request requires VDOC_AUTH_ALLOW_REGISTRATION=true on the backend.
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

List routes accept the PRD filters used by the workbench: document lists accept `?document_type=1|2`, while draft and version lists accept `?branch_id={branch_id}`. Draft detail/list responses include `review_comment` after request-changes, reject, or approve review actions.

## Route Categories

| Category | Purpose |
|---|---|
| Open | Health, login, opt-in registration, OpenAPI YAML, MCP JSON-RPC, and capability-authenticated public document shares. |
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
| AI | Built-in Admin AI provider, prompt, AI summary, and page chat APIs. |
| Document Shares | Admin-managed public capability links and anonymous published-content access. |

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

Document lifecycle state is not patchable. `PATCH /api/v1/private/projects/{project_id}/documents/{document_id}` updates document metadata only; omit `status` (or send the unchanged active value for legacy clients). Archive a document only through `POST /api/v1/private/projects/{project_id}/documents/{document_id}/archive`. A rejected status change does not partially update the other fields.

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

Review actions accept an optional JSON `comment` (or legacy `reason` alias), trim it, persist it on the draft, return it as `review_comment`, and include it in review audit metadata without placing it in the published version.

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

OpenAPI summaries use `added_endpoints`, `removed_endpoints`, `modified_endpoints`, and `breaking_changes`. Markdown summaries set `document_format: 3` and use `added_lines`, `removed_lines`, `modified_lines`, and `modified_blocks`; endpoint counts remain zero for Markdown.

Creating a comparison requires an active Project and Document, and `from_version_id` must differ from `to_version_id`. Previously stored Diff records and summaries remain readable after their Project, Document, or target Branch is archived; only creation of a new comparison is blocked.

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

The token value in `.detail.token` is returned on creation and may be revealed again by the token owner with `GET /api/v1/private/mcp-tokens/{token_id}` while the token remains active. List, revoked, and expired token responses stay redacted. Storage uses `token_hash` for authentication and encrypted `token_ciphertext` for owner reveal; neither storage field is exposed by the API.

Current MCP tools include `list_projects`, `list_documents`, `list_api_versions`, `list_doc_versions`, `get_latest_schema`, `get_endpoint_detail`, `compare_api_versions`, `get_change_summary`, `create_api_version_draft`, `update_api_version_draft`, `submit_api_version_draft`, `get_api_version_draft`, `get_latest_doc`, `compare_doc_versions`, `create_doc_draft`, `update_doc_draft`, `submit_doc_draft`, and `get_doc_draft`. Direct publish tools are intentionally not exposed in v0.1. `list_api_versions` is retained for API-document clients; Markdown agents should use the discoverable `list_doc_versions` tool with a `doc:read` scope.

### 5. Built-In Admin AI

SuperAdmins alone can read, update, and test the system OpenAI-compatible provider and system prompts at `/api/v1/private/ai/*`. Project Admins and SuperAdmins can read, update, and test project provider and prompt configuration under `/api/v1/private/projects/{project_id}/ai/*`; Reader and Writer roles are denied those configuration APIs even though they may use page summaries and Chat where document permissions allow. Provider responses expose `api_key_set` and `api_key_last4` only. AI summary and chat outputs are AI-generated helper text and cannot approve, request changes, reject, publish, or modify drafts or versions.

Provider payloads accept tuning fields alongside `name`, `base_url`, `model`, `api_mode`, `api_key`, and `enabled`. `temperature` defaults to `0.2` and accepts `0` through `2`. `timeout_ms` defaults to `30000` and accepts `1000` through `120000`. `max_output_tokens` defaults to `1000` and accepts `1` through `32000`. Project provider endpoints use the same request and response shape as the system provider.

Provider tests accept an optional request body. Omitting the body tests the saved effective configuration; for a project without an enabled override, this deliberately tests the enabled system-provider fallback. Prompt update bodies contain `system_prompt`, `user_prompt_template`, and `enabled`; the path is the single source of truth for `prompt_key`. Both prompt strings must be non-blank, every `user_prompt_template` must contain the literal `{{context}}`, and `page_chat` must additionally contain `{{message}}`.

```sh
curl -sS "$API_BASE/api/v1/private/ai/provider" \
  -X PUT \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d '{"name":"docs-ai","base_url":"https://api.openai.example","model":"gpt-4.1-mini","api_mode":"chat_completions","api_key":"sk-change-me","enabled":true,"temperature":0.2,"timeout_ms":30000,"max_output_tokens":1000}'
```

Submitting a draft automatically attempts a draft AI summary after the draft is saved as submitted. Approving a draft automatically attempts a version AI summary after the version is published. OpenAPI and Markdown draft submit and approve paths both follow this rule. The summary attempt is helper work, not part of the approval decision.

The latest in-flight generation is visible as `pending`. Skipped and failed automatic summaries are saved as non-blocking `skipped` or `failed` records. Missing providers and disabled prompts are stored as `skipped`; provider call errors are stored as `failed`. A completion is rejected if its target, permissions, provider, or prompt changed while the provider call was running. These records are visible through the same `ai-summary` read endpoints and do not roll back submit or publish.

AI chat sends a bounded window of the current session history. Each session uses a persisted generation token so an older provider response cannot overwrite a newer request, including when requests are handled by different Vdoc instances. The chat-session collection supports `GET` with required `document_id`, `context_type`, and `context_id` query parameters so clients can recover all sessions for the current page context, newest-updated first.

Archiving is a read-only boundary for AI history. Project provider and prompt configuration remains readable to Project Admins and SuperAdmins after Project archive, while provider update/test and prompt update are blocked. Reader and Writer roles still cannot read that configuration. Existing Summary records, Chat sessions, and Chat messages remain readable after Project, Document, or target Branch archive. Manual regeneration, new Chat session creation, and new Chat messages require an active Project, Document, and target Branch. Manual Summary regeneration additionally requires Project Admin or SuperAdmin permission; readable project members may use Chat while the context is active. A provider completion that returns after the context or relevant configuration became stale is discarded.

This produces the following archive boundary across the private APIs:

| Resource after parent archive | Read/list | New work | Remaining lifecycle action |
|---|---|---|---|
| Draft, Version, stored Diff | Allowed | Draft mutation and Compare blocked | None |
| AI provider/prompts | Project Admin/SuperAdmin only | Update and provider test blocked | None |
| AI Summary/Chat | Allowed | Regenerate, create session, and send blocked | None |
| Document share | List allowed | Create and reveal blocked | Revoke allowed |

AI audit metadata includes token usage fields when the provider returns them: `prompt_tokens`, `completion_tokens`, and `total_tokens`. API keys, JWTs, MCP tokens, and Authorization headers are not stored in AI audit metadata. Provider test calls, manual summary regeneration, automatic summaries, and chat calls are audited with status and non-secret context.

```text
GET  /api/v1/private/ai/provider
PUT  /api/v1/private/ai/provider
POST /api/v1/private/ai/provider/test
GET  /api/v1/private/projects/{project_id}/ai/provider
PUT  /api/v1/private/projects/{project_id}/ai/provider
POST /api/v1/private/projects/{project_id}/ai/provider/test
GET  /api/v1/private/ai/prompts
PUT  /api/v1/private/ai/prompts/{prompt_key}
GET  /api/v1/private/projects/{project_id}/ai/prompts
PUT  /api/v1/private/projects/{project_id}/ai/prompts/{prompt_key}
GET  /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/ai-summary
POST /api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/ai-summary/regenerate
GET  /api/v1/private/projects/{project_id}/documents/{document_id}/versions/{version_id}/ai-summary
POST /api/v1/private/projects/{project_id}/documents/{document_id}/versions/{version_id}/ai-summary/regenerate
GET  /api/v1/private/projects/{project_id}/documents/{document_id}/diffs/{diff_id}/ai-summary
POST /api/v1/private/projects/{project_id}/documents/{document_id}/diffs/{diff_id}/ai-summary/regenerate
GET  /api/v1/private/projects/{project_id}/ai/chat-sessions?document_id={document_id}&context_type={draft|version|diff}&context_id={context_id}
POST /api/v1/private/projects/{project_id}/ai/chat-sessions
GET  /api/v1/private/projects/{project_id}/ai/chat-sessions/{session_id}
POST /api/v1/private/projects/{project_id}/ai/chat-sessions/{session_id}/messages
```

### 6. Public Document Shares

Only Project Admins and SuperAdmins can manage public links. A branch must be active and already contain at least one published version before a link can be created. `version_scope` is `1` for the moving latest published version and `2` for all published versions on the branch. `expiry_preset` accepts `1_month`, `3_months`, `6_months`, `1_year`, or `permanent`; Admin selects `3_months` by default. `password` is optional and, when present, must be 12–72 UTF-8 bytes with no leading or trailing Unicode whitespace.

After the parent Project, Document, or Branch is archived, Admins can still list retained share history and revoke an active link. Creating a link and revealing its capability are blocked. Anonymous public access remains unavailable whenever a parent is inactive and deliberately does not reveal which lifecycle check failed.

```sh
SHARE_RESPONSE=$(curl -sS "$API_BASE/api/v1/private/projects/$PROJECT_ID/documents/$DOCUMENT_ID/shares" \
  -H 'Content-Type: application/json' \
  -H "Authorization: $JWT" \
  -d "{\"branch_id\":\"$BRANCH_ID\",\"version_scope\":2,\"expiry_preset\":\"1_month\",\"password\":\"sample share password\"}")
SHARE_ID=$(printf '%s' "$SHARE_RESPONSE" | jq -r '.detail.share.id')
SHARE_SECRET=$(printf '%s' "$SHARE_RESPONSE" | jq -r '.detail.secret')
```

The complete browser URL is built as `/share/{share_id}#{secret}`. The fragment must be removed from the current history entry before any network request. Anonymous API calls send `Authorization: VdocShare {secret}`, omit account cookies/JWT, and receive `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, and `X-Robots-Tag: noindex` protections.

Password-protected shares exchange the password for a 15-minute, share-bound proof:

```text
POST /api/v1/open/document-shares/{share_id}/unlock
GET  /api/v1/open/document-shares/{share_id}
GET  /api/v1/open/document-shares/{share_id}/versions
GET  /api/v1/open/document-shares/{share_id}/versions/{version_id}/content
GET  /api/v1/open/document-shares/{share_id}/versions/{version_id}/download
```

Send the proof as `X-Vdoc-Share-Unlock`. Invalid capabilities, passwords, proofs, revoked/expired links, and inactive parent resources all return the same public unavailable response. Downloads always pass through Vdoc authorization; object storage remains private. Markdown viewers must disable raw HTML, remote images, and unsafe links, while OpenAPI content is rendered only as escaped read-only text.

## Route Reference

| Category | Methods | Path | Auth |
|---|---|---|---|
| Open | `GET` | `/api/v1/open/health` | Public |
| Open | `GET` | `/api/v1/open/auth/config` | Public |
| Open | `POST` | `/api/v1/open/auth/register` | Public |
| Open | `POST` | `/api/v1/open/auth/login` | Public |
| Open | `GET` | `/api/v1/open/docs/openapi.yaml` | Public |
| Open | `POST` | `/api/v1/open/mcp` | MCP token |
| Document Shares | `GET` | `/api/v1/open/document-shares/{share_id}` | Share capability |
| Document Shares | `POST` | `/api/v1/open/document-shares/{share_id}/unlock` | Share capability |
| Document Shares | `GET` | `/api/v1/open/document-shares/{share_id}/versions` | Share capability/proof |
| Document Shares | `GET` | `/api/v1/open/document-shares/{share_id}/versions/{version_id}/content` | Share capability/proof |
| Document Shares | `GET` | `/api/v1/open/document-shares/{share_id}/versions/{version_id}/download` | Share capability/proof |
| Identity | `GET` | `/api/v1/private/identity/me` | JWT |
| System Users | `GET`, `POST` | `/api/v1/private/system/users` | JWT |
| System Users | `PATCH` | `/api/v1/private/system/users/{user_id}` | JWT |
| System Users | `GET` | `/api/v1/private/system/users/{user_id}/mcp-tokens` | JWT |
| System Users | `POST` | `/api/v1/private/system/users/{user_id}/mcp-tokens/{token_id}/revoke` | JWT |
| Teams | `GET`, `POST` | `/api/v1/private/teams` | JWT; SuperAdmin |
| Teams | `GET`, `PATCH` | `/api/v1/private/teams/{team_id}` | JWT; SuperAdmin |
| Teams | `POST` | `/api/v1/private/teams/{team_id}/archive` | JWT; SuperAdmin |
| Projects | `GET`, `POST` | `/api/v1/private/projects` | JWT |
| Projects | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}` | JWT |
| Projects | `POST` | `/api/v1/private/projects/{project_id}/archive` | JWT |
| Projects | `GET`, `POST` | `/api/v1/private/projects/{project_id}/members` | JWT |
| Projects | `GET` | `/api/v1/private/projects/{project_id}/member-candidates` | JWT; Project Admin or SuperAdmin |
| Projects | `DELETE` | `/api/v1/private/projects/{project_id}/members/{user_id}` | JWT |
| Projects | `PATCH` | `/api/v1/private/projects/{project_id}/members/{user_id}/role` | JWT |
| Documents | `GET`, `POST` | `/api/v1/private/projects/{project_id}/documents` | JWT |
| Documents | `GET`, `PATCH` | `/api/v1/private/projects/{project_id}/documents/{document_id}` | JWT |
| Documents | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/archive` | JWT |
| Document Shares | `GET`, `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/shares` | JWT (Project Admin/SuperAdmin) |
| Document Shares | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/shares/{share_id}/reveal` | JWT (Project Admin/SuperAdmin) |
| Document Shares | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/shares/{share_id}/revoke` | JWT (Project Admin/SuperAdmin) |
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
| Diffs | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/diffs` | JWT |
| Diffs | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/diffs/{diff_id}` | JWT |
| Diffs | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/diffs/{diff_id}/summary` | JWT |
| Audit Logs | `GET` | `/api/v1/private/audit-logs` | JWT; super admin or scoped project admin |
| AI | `GET`, `PUT` | `/api/v1/private/ai/provider` | JWT; SuperAdmin |
| AI | `POST` | `/api/v1/private/ai/provider/test` | JWT; SuperAdmin |
| AI | `GET`, `PUT` | `/api/v1/private/projects/{project_id}/ai/provider` | JWT; Project Admin or SuperAdmin |
| AI | `POST` | `/api/v1/private/projects/{project_id}/ai/provider/test` | JWT; Project Admin or SuperAdmin |
| AI | `GET` | `/api/v1/private/ai/prompts` | JWT; SuperAdmin |
| AI | `PUT` | `/api/v1/private/ai/prompts/{prompt_key}` | JWT; SuperAdmin |
| AI | `GET` | `/api/v1/private/projects/{project_id}/ai/prompts` | JWT; Project Admin or SuperAdmin |
| AI | `PUT` | `/api/v1/private/projects/{project_id}/ai/prompts/{prompt_key}` | JWT; Project Admin or SuperAdmin |
| AI | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/ai-summary` | JWT |
| AI | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/drafts/{draft_id}/ai-summary/regenerate` | JWT |
| AI | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/versions/{version_id}/ai-summary` | JWT |
| AI | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/versions/{version_id}/ai-summary/regenerate` | JWT |
| AI | `GET` | `/api/v1/private/projects/{project_id}/documents/{document_id}/diffs/{diff_id}/ai-summary` | JWT |
| AI | `POST` | `/api/v1/private/projects/{project_id}/documents/{document_id}/diffs/{diff_id}/ai-summary/regenerate` | JWT |
| AI | `GET`, `POST` | `/api/v1/private/projects/{project_id}/ai/chat-sessions` | JWT |
| AI | `GET` | `/api/v1/private/projects/{project_id}/ai/chat-sessions/{session_id}` | JWT |
| AI | `POST` | `/api/v1/private/projects/{project_id}/ai/chat-sessions/{session_id}/messages` | JWT |
| MCP Tokens | `GET`, `POST` | `/api/v1/private/mcp-tokens` | JWT |
| MCP Tokens | `GET` | `/api/v1/private/mcp-tokens/{token_id}` | JWT |
| MCP Tokens | `POST` | `/api/v1/private/mcp-tokens/{token_id}/revoke` | JWT |
