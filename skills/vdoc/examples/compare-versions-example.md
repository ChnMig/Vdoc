# Compare Versions Example

Use this workflow before migration advice or frontend impact analysis. The IDs below are placeholders; resolve real IDs with Vdoc MCP and never paste MCP tokens, JWTs, Authorization headers, or copied secret values into examples.

## Compare Two Published Versions

```json
{
  "jsonrpc": "2.0",
  "id": "compare-versions-example",
  "method": "tools/call",
  "params": {
    "name": "compare_api_versions",
    "arguments": {
      "project_id": "proj_placeholder",
      "service_id": "svc_placeholder",
      "from_version_id": "ver_placeholder_from",
      "to_version_id": "ver_placeholder_to"
    }
  }
}
```

Use the returned diff items as the source of truth. For each item, preserve fields such as `location`, `message`, `old_value`, `new_value`, `frontend_impact`, `is_breaking`, and `must_handle`.

## Get A Must-handle And Optional Summary

```json
{
  "jsonrpc": "2.0",
  "id": "change-summary-example",
  "method": "tools/call",
  "params": {
    "name": "get_change_summary",
    "arguments": {
      "project_id": "proj_placeholder",
      "service_id": "svc_placeholder",
      "diff_id": "diff_placeholder"
    }
  }
}
```

Report `must_handle` / breaking items first, then optional/non-breaking items. Do not claim a breaking change unless Vdoc MCP returned `is_breaking` or `must_handle` for that item.
