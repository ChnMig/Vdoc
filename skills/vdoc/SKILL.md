---
name: vdoc
description: Use for Vdoc API contract lookup, Markdown document lookup, endpoint integration, frontend change summaries, migration impact analysis, and draft submission through Vdoc MCP.
---

# Vdoc Skill

Use this skill when a user needs Vdoc project document facts, API contract facts, Markdown document content, endpoint integration code or client types, frontend change summaries, migration impact analysis, or draft submission that should not rely on guessed details.

## Contract Source Of Truth

- Vdoc MCP is the source of truth for API contract facts and Markdown document content.
- Do not infer or hallucinate endpoint fields, parameters, response properties, enum values, auth schemes, servers, breaking-change claims, or Markdown text that were not returned by Vdoc MCP.
- Treat user-provided source code, stale docs, screenshots, and memory as consumer context only. Use Vdoc MCP before stating contract facts.
- v0.1 does not support automatic frontend repository modification. Produce analysis, instructions, and code snippets only when requested, based on MCP results.

## Trigger Use Cases

- Endpoint integration: generate request code, TypeScript types, tests, or usage notes for a specific endpoint.
- Frontend change summary: explain what frontend code must change between API versions.
- Migration impact: analyze breaking changes and optional changes before an upgrade.
- OpenAPI draft submission: create, update, and submit a schema draft for human review.
- Markdown document lookup: fetch latest Markdown content or compare Markdown document versions.
- Markdown draft submission: create, update, inspect, and submit a Markdown draft for human review.
- Contract lookup: answer questions about projects, documents, versions, schemas, endpoints, diffs, and draft status.

## Valid v0.1 MCP Tools

- Discovery tools: `list_projects`, `list_documents`.
- API read tools: `list_api_versions`, `get_latest_schema`, `get_endpoint_detail`, `compare_api_versions`, `get_change_summary`, `get_api_version_draft`.
- API draft tools: `create_api_version_draft`, `update_api_version_draft`, `submit_api_version_draft`.
- Markdown read tools: `get_latest_doc`, `compare_doc_versions`.
- Markdown draft tools: `create_doc_draft`, `update_doc_draft`, `submit_doc_draft`, `get_doc_draft`.
- Direct publish tools are unavailable in v0.1. Do not call direct publish operations or present them as available MCP capabilities.

## Mandatory MCP Workflow

- Always call JSON-RPC `tools/list` if unsure which Vdoc MCP tools are available.
- Resolve IDs with `list_projects`, `list_documents`, and `list_api_versions` when the user gives names instead of IDs.
- Use `document_id` for API and Markdown document tools.
- You must call `get_endpoint_detail` before generating endpoint integration code or client types.
- You must call `compare_api_versions` before migration advice or frontend impact analysis.
- After `compare_api_versions`, optionally call `get_change_summary` when the user asks for a concise frontend summary or when the raw diff needs grouping into `must_handle`, `breaking`, `optional`, and `non_breaking` buckets.
- For OpenAPI submission, use draft tools only: `create_api_version_draft`, `update_api_version_draft`, then `submit_api_version_draft`. Human Admin/SuperAdmin review publishes versions; v0.1 MCP has no direct publish tool.
- For Markdown documents, use `get_latest_doc` for current stable Markdown content, `compare_doc_versions` for plain line-level diffs, and draft tools only for proposed edits.

## Endpoint Integration Workflow

1. Resolve `project_id`, `document_id`, `version_id`, and `endpoint_id`.
2. Call `get_endpoint_detail` with `project_id`, `document_id`, `version_id`, and `endpoint_id`.
3. Generate integration output only from returned method, path, operationId, parameters, request body, responses, security, servers, required fields, and enum values.
4. If required contract data is missing from the MCP response, say it is not available in Vdoc instead of inventing it.
5. Use `templates/endpoint-integration.md` for the final structure when the user asks for code or client types.

## Version Change Workflow

1. Resolve `project_id`, `document_id`, `from_version_id`, and `to_version_id`.
2. Call `compare_api_versions` with those IDs.
3. Optionally call `get_change_summary` with the returned `diff_id`.
4. Use diff item fields such as `location`, `message`, `old_value`, `new_value`, `frontend_impact`, `is_breaking`, and `must_handle`.
5. Output must distinguish `must_handle` / breaking changes from optional/non-breaking changes.
6. Use `templates/frontend-change-summary.md` for frontend-facing reports.

## Markdown Document Workflow

1. Resolve `project_id` and `document_id` with `list_projects` and `list_documents`.
2. Call `get_latest_doc` before quoting or editing Markdown document content.
3. Call `compare_doc_versions` before describing Markdown changes between two published versions.
4. For Markdown draft submission, use `create_doc_draft`, `update_doc_draft`, optional `get_doc_draft`, then `submit_doc_draft`.
5. Use `markdown_content` for Markdown draft content and keep it tied to the Vdoc-returned document.

## OpenAPI Draft Submission Workflow

Use JSON-RPC `tools/call` requests with placeholder IDs and redacted schema content. Never include tokens or Authorization headers in examples or final output.

Create a draft:

```json
{
  "jsonrpc": "2.0",
  "id": "create-api-draft-example",
  "method": "tools/call",
  "params": {
    "name": "create_api_version_draft",
    "arguments": {
      "project_id": "proj_placeholder",
      "document_id": "doc_placeholder",
      "branch_id": "branch_placeholder",
      "version_name": "1.2.0",
      "changelog": "Describe the API contract changes for human review.",
      "source_git_commit_id": "commit_placeholder",
      "schema_content": "openapi: 3.1.0\ninfo:\n  title: Example API\n  version: 1.2.0\npaths: {}\n"
    }
  }
}
```

Update a draft:

```json
{
  "jsonrpc": "2.0",
  "id": "update-api-draft-example",
  "method": "tools/call",
  "params": {
    "name": "update_api_version_draft",
    "arguments": {
      "project_id": "proj_placeholder",
      "document_id": "doc_placeholder",
      "draft_id": "draft_placeholder",
      "branch_id": "branch_placeholder",
      "version_name": "1.2.0",
      "changelog": "Update the draft after local schema correction.",
      "source_git_commit_id": "commit_placeholder",
      "schema_content": "openapi: 3.1.0\ninfo:\n  title: Example API\n  version: 1.2.0\npaths: {}\n"
    }
  }
}
```

Submit a draft for review:

```json
{
  "jsonrpc": "2.0",
  "id": "submit-api-draft-example",
  "method": "tools/call",
  "params": {
    "name": "submit_api_version_draft",
    "arguments": {
      "project_id": "proj_placeholder",
      "document_id": "doc_placeholder",
      "draft_id": "draft_placeholder"
    }
  }
}
```

## Markdown Examples

Get latest Markdown content:

```json
{
  "jsonrpc": "2.0",
  "id": "latest-doc-example",
  "method": "tools/call",
  "params": {
    "name": "get_latest_doc",
    "arguments": {
      "project_id": "proj_placeholder",
      "document_id": "doc_placeholder"
    }
  }
}
```

Compare two Markdown versions:

```json
{
  "jsonrpc": "2.0",
  "id": "compare-doc-example",
  "method": "tools/call",
  "params": {
    "name": "compare_doc_versions",
    "arguments": {
      "project_id": "proj_placeholder",
      "document_id": "doc_placeholder",
      "from_version_id": "ver_placeholder_from",
      "to_version_id": "ver_placeholder_to"
    }
  }
}
```

Create, inspect, update, and submit a Markdown draft:

```json
{
  "jsonrpc": "2.0",
  "id": "create-doc-draft-example",
  "method": "tools/call",
  "params": {
    "name": "create_doc_draft",
    "arguments": {
      "project_id": "proj_placeholder",
      "document_id": "doc_placeholder",
      "branch_id": "branch_placeholder",
      "version_name": "1.2.0",
      "changelog": "Describe the Markdown update for human review.",
      "source_git_commit_id": "commit_placeholder",
      "markdown_content": "# Example\n\nUpdated Markdown content.\n"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": "get-doc-draft-example",
  "method": "tools/call",
  "params": {
    "name": "get_doc_draft",
    "arguments": {
      "project_id": "proj_placeholder",
      "document_id": "doc_placeholder",
      "draft_id": "draft_placeholder"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": "update-doc-draft-example",
  "method": "tools/call",
  "params": {
    "name": "update_doc_draft",
    "arguments": {
      "project_id": "proj_placeholder",
      "document_id": "doc_placeholder",
      "draft_id": "draft_placeholder",
      "branch_id": "branch_placeholder",
      "version_name": "1.2.0",
      "changelog": "Refine the Markdown draft after review.",
      "source_git_commit_id": "commit_placeholder",
      "markdown_content": "# Example\n\nRefined Markdown content.\n"
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": "submit-doc-draft-example",
  "method": "tools/call",
  "params": {
    "name": "submit_doc_draft",
    "arguments": {
      "project_id": "proj_placeholder",
      "document_id": "doc_placeholder",
      "draft_id": "draft_placeholder"
    }
  }
}
```

After submission, state that the draft is waiting for human Admin/SuperAdmin review and that Vdoc creates a published immutable version only after that review path succeeds.

## Security Rules

- Never print, copy, or log MCP tokens or JWTs.
- Never include Authorization headers in final output.
- Never include copied secret values, passwords, tokens, or credentials in examples.
- Redact accidental secrets as `<redacted>` and summarize what was redacted.
- Do not ask the user to paste an MCP token or JWT into chat when an existing MCP connection can perform the call.

## Output Rules

- Say which Vdoc MCP facts were used, but do not expose request credentials.
- Keep contract facts tied to MCP-returned IDs and fields.
- For endpoint code, include a short note if any expected field was absent from `get_endpoint_detail`.
- For Markdown answers, quote only content returned by `get_latest_doc`, `get_doc_draft`, or a Vdoc diff response.
