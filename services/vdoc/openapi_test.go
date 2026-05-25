package vdoc

import (
	"encoding/json"
	"testing"
)

func TestParseOpenAPIExtractsV01EndpointDetailsFromJSON(t *testing.T) {
	parsed, err := ParseOpenAPI(openAPIDetailFixtureJSON())
	if err != nil {
		t.Fatalf("ParseOpenAPI() error = %v", err)
	}
	if parsed.SchemaFormat != SchemaFormatOpenAPI31 {
		t.Fatalf("SchemaFormat = %d, want OpenAPI 3.1", parsed.SchemaFormat)
	}
	if len(parsed.Endpoints) != 1 {
		t.Fatalf("endpoints = %d, want 1", len(parsed.Endpoints))
	}
	endpoint := parsed.Endpoints[0]
	if endpoint.Method != "POST" || endpoint.Path != "/pets/{petId}" || endpoint.OperationID != "createPet" || endpoint.Summary != "Create pet" || !endpoint.Deprecated {
		t.Fatalf("endpoint summary fields = %+v", endpoint)
	}
	if got := endpoint.Tags; len(got) != 1 || got[0] != "pets" {
		t.Fatalf("tags = %#v, want [pets]", got)
	}

	parameters, ok := endpoint.Parameters.([]any)
	if !ok || len(parameters) != 2 {
		t.Fatalf("parameters = %#v, want two resolved parameters", endpoint.Parameters)
	}
	pathParam := parameters[0].(map[string]any)
	queryParam := parameters[1].(map[string]any)
	if pathParam["name"] != "petId" || pathParam["in"] != "path" || pathParam["required"] != true {
		t.Fatalf("path parameter = %#v", pathParam)
	}
	querySchema := queryParam["schema"].(map[string]any)
	if enum := querySchema["enum"].([]any); len(enum) != 2 || enum[0] != "small" || enum[1] != "large" {
		t.Fatalf("query enum = %#v", querySchema["enum"])
	}

	requestBody := endpoint.RequestBody.(map[string]any)
	jsonContent := requestBody["content"].(map[string]any)["application/json"].(map[string]any)
	bodySchema := jsonContent["schema"].(map[string]any)
	if required := bodySchema["required"].([]any); len(required) != 2 || required[0] != "name" || required[1] != "kind" {
		t.Fatalf("request required = %#v", bodySchema["required"])
	}
	properties := bodySchema["properties"].(map[string]any)
	kind := properties["kind"].(map[string]any)
	if enum := kind["enum"].([]any); len(enum) != 2 || enum[0] != "cat" || enum[1] != "dog" {
		t.Fatalf("schema enum = %#v", kind["enum"])
	}

	responses := endpoint.Responses.(map[string]any)
	created := responses["201"].(map[string]any)
	responseSchema := created["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if responseSchema["type"] != "object" {
		t.Fatalf("response schema = %#v", responseSchema)
	}
	if security := endpoint.Security.([]any); len(security) != 1 {
		t.Fatalf("security = %#v, want one requirement", endpoint.Security)
	}
	if servers := endpoint.Servers.([]any); len(servers) != 1 || servers[0].(map[string]any)["url"] != "https://api.example.com/v1" {
		t.Fatalf("servers = %#v", endpoint.Servers)
	}
	if refs := endpoint.SchemaRefs.([]any); len(refs) != 5 {
		t.Fatalf("schema refs = %#v, want five refs", endpoint.SchemaRefs)
	}
	if endpoint.NormalizedOperation == nil {
		t.Fatal("NormalizedOperation is nil")
	}
}

func TestParseOpenAPIYAMLMatchesJSONNormalization(t *testing.T) {
	jsonParsed, err := ParseOpenAPI(openAPIDetailFixtureJSON())
	if err != nil {
		t.Fatalf("ParseOpenAPI(json) error = %v", err)
	}
	yamlParsed, err := ParseOpenAPI(openAPIDetailFixtureYAML())
	if err != nil {
		t.Fatalf("ParseOpenAPI(yaml) error = %v", err)
	}
	if yamlParsed.SchemaFormat != SchemaFormatOpenAPI31 {
		t.Fatalf("yaml SchemaFormat = %d, want OpenAPI 3.1", yamlParsed.SchemaFormat)
	}
	if jsonParsed.Normalized != yamlParsed.Normalized {
		t.Fatalf("normalized JSON/YAML differ:\njson=%s\nyaml=%s", jsonParsed.Normalized, yamlParsed.Normalized)
	}
	if jsonParsed.Endpoints[0].Hash != yamlParsed.Endpoints[0].Hash {
		t.Fatalf("endpoint hash differs: json=%s yaml=%s", jsonParsed.Endpoints[0].Hash, yamlParsed.Endpoints[0].Hash)
	}
}

func TestParseOpenAPIDoesNotFabricateAbsentDetailFields(t *testing.T) {
	parsed, err := ParseOpenAPI(`{"openapi":"3.0.3","info":{"title":"Test","version":"1"},"paths":{"/ping":{"get":{"operationId":"ping","responses":{"204":{"description":"empty"}}}}}}`)
	if err != nil {
		t.Fatalf("ParseOpenAPI() error = %v", err)
	}
	endpoint := parsed.Endpoints[0]
	if endpoint.Parameters != nil || endpoint.RequestBody != nil || endpoint.Security != nil || endpoint.Servers != nil || endpoint.SchemaRefs != nil {
		t.Fatalf("fabricated optional details: %+v", endpoint)
	}
	responses := endpoint.Responses.(map[string]any)
	if responses["204"].(map[string]any)["schema"] != nil {
		t.Fatalf("fabricated response schema: %#v", responses["204"])
	}
}

func openAPIDetailFixtureJSON() string {
	fixture := map[string]any{
		"openapi":  "3.1.0",
		"info":     map[string]any{"title": "Pet API", "version": "1.0.0"},
		"servers":  []any{map[string]any{"url": "https://api.example.com"}},
		"security": []any{map[string]any{"api_key": []any{}}},
		"paths": map[string]any{
			"/pets/{petId}": map[string]any{
				"servers":    []any{map[string]any{"url": "https://api.example.com/v1"}},
				"parameters": []any{map[string]any{"$ref": "#/components/parameters/PetID"}},
				"post": map[string]any{
					"tags":        []any{"pets"},
					"operationId": "createPet",
					"summary":     "Create pet",
					"deprecated":  true,
					"parameters":  []any{map[string]any{"$ref": "#/components/parameters/Size"}},
					"requestBody": map[string]any{"$ref": "#/components/requestBodies/PetBody"},
					"responses": map[string]any{
						"201": map[string]any{"$ref": "#/components/responses/PetCreated"},
					},
				},
			},
		},
		"components": map[string]any{
			"parameters": map[string]any{
				"PetID": map[string]any{"name": "petId", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
				"Size":  map[string]any{"name": "size", "in": "query", "schema": map[string]any{"type": "string", "enum": []any{"small", "large"}}},
			},
			"requestBodies": map[string]any{
				"PetBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Pet"}}}},
			},
			"responses": map[string]any{
				"PetCreated": map[string]any{"description": "created", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Pet"}}}},
			},
			"schemas": map[string]any{
				"Pet": map[string]any{"type": "object", "required": []any{"name", "kind"}, "properties": map[string]any{"name": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string", "enum": []any{"cat", "dog"}}}},
			},
		},
	}
	raw, _ := json.Marshal(fixture)
	return string(raw)
}

func openAPIDetailFixtureYAML() string {
	return `openapi: 3.1.0
info:
  title: Pet API
  version: 1.0.0
servers:
  - url: https://api.example.com
security:
  - api_key: []
paths:
  /pets/{petId}:
    servers:
      - url: https://api.example.com/v1
    parameters:
      - $ref: '#/components/parameters/PetID'
    post:
      tags: [pets]
      operationId: createPet
      summary: Create pet
      deprecated: true
      parameters:
        - $ref: '#/components/parameters/Size'
      requestBody:
        $ref: '#/components/requestBodies/PetBody'
      responses:
        '201':
          $ref: '#/components/responses/PetCreated'
components:
  parameters:
    PetID:
      name: petId
      in: path
      required: true
      schema:
        type: string
    Size:
      name: size
      in: query
      schema:
        type: string
        enum: [small, large]
  requestBodies:
    PetBody:
      required: true
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Pet'
  responses:
    PetCreated:
      description: created
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Pet'
  schemas:
    Pet:
      type: object
      required: [name, kind]
      properties:
        name:
          type: string
        kind:
          type: string
          enum: [cat, dog]
`
}
