# Endpoint Query Example

Use this workflow before generating endpoint integration code, client types, request snippets, or endpoint-specific tests. The IDs below are placeholders; resolve real IDs with Vdoc MCP and never paste MCP tokens, JWTs, Authorization headers, or copied secret values into examples.

## Optional Tool Discovery

```json
{
  "jsonrpc": "2.0",
  "id": "tools-list-example",
  "method": "tools/list"
}
```

## Query Endpoint Detail

```json
{
  "jsonrpc": "2.0",
  "id": "endpoint-detail-example",
  "method": "tools/call",
  "params": {
    "name": "get_endpoint_detail",
    "arguments": {
      "project_id": "proj_placeholder",
      "service_id": "svc_placeholder",
      "version_id": "ver_placeholder",
      "endpoint_id": "endpoint_placeholder"
    }
  }
}
```

Use only the returned method, path, operationId, parameters, request body, responses, security, servers, required fields, and enum values. If Vdoc MCP does not return a field, say it was not returned instead of guessing.
