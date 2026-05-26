# Endpoint Integration Template

Use this template only after calling `get_endpoint_detail`. Do not generate endpoint integration code, request functions, or client types until Vdoc MCP returns the endpoint detail.

## MCP Requirement

- Required call: `get_endpoint_detail`
- Required arguments: `project_id`, `document_id`, `version_id`, `endpoint_id`
- Source of truth: the returned endpoint method, path, operationId, parameters, request body, responses, security, servers, required fields, and enum values

## Contract Inputs From `get_endpoint_detail`

- Method: `{{method}}`
- Path: `{{path}}`
- operationId: `{{operationId}}`
- Parameters: `{{parameters}}`
- Request body: `{{request_body}}`
- Responses: `{{responses}}`
- Security: `{{security}}`
- Servers: `{{servers}}`
- Required fields: `{{required_fields}}`
- Enum values: `{{enum_values}}`

## Integration Output

### Types

- Request parameter types come only from returned parameters.
- Request body types come only from the returned request body schema and required fields.
- Response types come only from returned responses, status codes, required fields, and enum values.

### Request Function

- Use the returned method and path exactly.
- Include only returned path, query, header, cookie, and body inputs.
- Apply only returned security requirements. Do not invent an auth scheme.
- If servers are returned, document the selected server or base URL assumption.

### Validation Notes

- Mark every required field as required in generated types.
- Preserve enum values exactly as returned by Vdoc MCP.
- If a field, parameter, response, security scheme, or server is missing from the MCP result, write `not returned by Vdoc MCP` instead of guessing.

## Final Response Shape

- Contract summary: method, path, operationId, security, and servers.
- Generated types or request snippet, if requested.
- Assumptions limited to values explicitly absent from Vdoc MCP and clearly labeled as not returned.
