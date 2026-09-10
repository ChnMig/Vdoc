package vdoc

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

var openAPIMethods = []string{"get", "post", "put", "patch", "delete", "options", "head", "trace"}

type ParsedOpenAPI struct {
	SchemaFormat int
	Normalized   string
	Endpoints    []Endpoint
}

func ParseOpenAPI(content string) (ParsedOpenAPI, error) {
	raw, err := decodeOpenAPI(content)
	if err != nil {
		return ParsedOpenAPI{}, err
	}
	openapi, _ := raw["openapi"].(string)
	format := 0
	switch {
	case strings.HasPrefix(openapi, "3.0"):
		format = SchemaFormatOpenAPI30
	case strings.HasPrefix(openapi, "3.1"):
		format = SchemaFormatOpenAPI31
	default:
		return ParsedOpenAPI{}, fmt.Errorf("%w: openapi must be 3.0.x or 3.1.x", ErrInvalidArgument)
	}
	paths, ok := asMap(raw["paths"])
	if !ok || len(paths) == 0 {
		return ParsedOpenAPI{}, fmt.Errorf("%w: paths is required", ErrInvalidArgument)
	}
	if err := validateOpenAPIInfo(raw); err != nil {
		return ParsedOpenAPI{}, err
	}
	normalizedBytes, err := json.Marshal(normalizeValue(raw))
	if err != nil {
		return ParsedOpenAPI{}, err
	}
	endpoints := []Endpoint{}
	for _, pathName := range keys(paths) {
		if !strings.HasPrefix(pathName, "/") {
			return ParsedOpenAPI{}, fmt.Errorf("%w: path must start with /", ErrInvalidArgument)
		}
		pathItem, ok := asMap(paths[pathName])
		if !ok {
			return ParsedOpenAPI{}, fmt.Errorf("%w: path item must be an object", ErrInvalidArgument)
		}
		for _, method := range openAPIMethods {
			op, ok := asMap(pathItem[method])
			if !ok {
				continue
			}
			endpoint, err := extractEndpoint(raw, pathName, method, pathItem, op)
			if err != nil {
				return ParsedOpenAPI{}, err
			}
			endpoints = append(endpoints, endpoint)
		}
	}
	if len(endpoints) == 0 {
		return ParsedOpenAPI{}, fmt.Errorf("%w: no endpoint operation found", ErrInvalidArgument)
	}
	return ParsedOpenAPI{SchemaFormat: format, Normalized: string(normalizedBytes), Endpoints: endpoints}, nil
}

func validateOpenAPIInfo(root map[string]any) error {
	info, ok := asMap(root["info"])
	if !ok {
		return fmt.Errorf("%w: info is required", ErrInvalidArgument)
	}
	title, _ := info["title"].(string)
	version, _ := info["version"].(string)
	if strings.TrimSpace(title) == "" || strings.TrimSpace(version) == "" {
		return fmt.Errorf("%w: info.title and info.version are required", ErrInvalidArgument)
	}
	return nil
}

func decodeOpenAPI(content string) (map[string]any, error) {
	var raw any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
			return nil, fmt.Errorf("%w: invalid OpenAPI JSON/YAML", ErrInvalidArgument)
		}
	}
	converted := normalizeValue(raw)
	root, ok := converted.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: OpenAPI document must be an object", ErrInvalidArgument)
	}
	return root, nil
}

func extractEndpoint(root map[string]any, pathName, method string, pathItem, op map[string]any) (Endpoint, error) {
	refs := map[string]bool{}
	endpoint := Endpoint{Method: strings.ToUpper(method), Path: pathName}
	endpoint.OperationID, _ = op["operationId"].(string)
	endpoint.Summary, _ = op["summary"].(string)
	endpoint.Deprecated, _ = op["deprecated"].(bool)
	endpoint.Tags = stringSlice(op["tags"])

	parameters, err := resolveParameters(root, pathItem["parameters"], op["parameters"], refs)
	if err != nil {
		return Endpoint{}, err
	}
	if len(parameters) > 0 {
		endpoint.Parameters = parameters
	}
	if requestBody, ok, err := resolveOptional(root, op["requestBody"], refs); err != nil {
		return Endpoint{}, err
	} else if ok {
		endpoint.RequestBody = requestBody
	}
	if responses, ok, err := resolveOptional(root, op["responses"], refs); err != nil {
		return Endpoint{}, err
	} else if ok {
		responsesMap, ok := asMap(responses)
		if !ok || len(responsesMap) == 0 {
			return Endpoint{}, fmt.Errorf("%w: operation responses must be a non-empty object", ErrInvalidArgument)
		}
		endpoint.Responses = responses
	} else {
		return Endpoint{}, fmt.Errorf("%w: operation responses are required", ErrInvalidArgument)
	}
	if security, ok := effectiveValue(root, pathItem, op, "security"); ok {
		endpoint.Security = normalizeValue(security)
	}
	securitySchemes, err := resolveSecuritySchemes(root, endpoint.Security)
	if err != nil {
		return Endpoint{}, err
	}
	if servers, ok := effectiveValue(root, pathItem, op, "servers"); ok {
		endpoint.Servers = normalizeValue(servers)
	}
	if len(refs) > 0 {
		endpoint.SchemaRefs = refsList(refs)
	}
	normalizedOperation := map[string]any{
		"method":          endpoint.Method,
		"path":            endpoint.Path,
		"operationId":     endpoint.OperationID,
		"summary":         endpoint.Summary,
		"tags":            endpoint.Tags,
		"deprecated":      endpoint.Deprecated,
		"parameters":      endpoint.Parameters,
		"requestBody":     endpoint.RequestBody,
		"responses":       endpoint.Responses,
		"security":        endpoint.Security,
		"securitySchemes": securitySchemes,
		"servers":         endpoint.Servers,
		"schemaRefs":      endpoint.SchemaRefs,
	}
	endpoint.NormalizedOperation = dropNil(normalizedOperation)
	hashBytes, _ := json.Marshal(endpoint.NormalizedOperation)
	endpoint.Hash = sha(string(hashBytes))
	return endpoint, nil
}

func resolveParameters(root map[string]any, pathParameters, operationParameters any, refs map[string]bool) ([]any, error) {
	merged := []any{}
	positions := map[string]int{}
	for _, source := range []any{pathParameters, operationParameters} {
		items, ok := source.([]any)
		if !ok {
			continue
		}
		identities := map[string]bool{}
		for _, item := range items {
			resolved, err := resolveRefs(root, item, refs, map[string]bool{})
			if err != nil {
				return nil, err
			}
			parameter, ok := asMap(resolved)
			if !ok {
				return nil, fmt.Errorf("%w: parameter must be an object", ErrInvalidArgument)
			}
			name, _ := parameter["name"].(string)
			location, _ := parameter["in"].(string)
			if name == "" || location == "" {
				return nil, fmt.Errorf("%w: parameter name and in are required", ErrInvalidArgument)
			}
			identity := parameterIdentity(location, name)
			if identities[identity] {
				return nil, fmt.Errorf("%w: duplicate parameter %s in %s", ErrInvalidArgument, name, location)
			}
			identities[identity] = true
			if position, exists := positions[identity]; exists {
				merged[position] = resolved
			} else {
				positions[identity] = len(merged)
				merged = append(merged, resolved)
			}
		}
	}
	return merged, nil
}

func resolveOptional(root map[string]any, value any, refs map[string]bool) (any, bool, error) {
	if value == nil {
		return nil, false, nil
	}
	resolved, err := resolveRefs(root, value, refs, map[string]bool{})
	if err != nil {
		return nil, false, err
	}
	return resolved, true, nil
}

func resolveRefs(root map[string]any, value any, refs, seen map[string]bool) (any, error) {
	return resolveOpenAPIValue(root, value, refs, seen, refObject)
}

type refValueKind int

const (
	refObject refValueKind = iota
	refSchema
	refSchemaMap
	refSchemaArray
	refObjectMap
	refLiteral
)

func resolveOpenAPIValue(root map[string]any, value any, refs, seen map[string]bool, kind refValueKind) (any, error) {
	if kind == refLiteral {
		return normalizeValue(value), nil
	}
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok && kind != refSchemaMap && kind != refObjectMap {
			if !strings.HasPrefix(ref, "#/") {
				return nil, fmt.Errorf("%w: only local OpenAPI $ref values are supported", ErrInvalidArgument)
			}
			if seen[ref] {
				return nil, fmt.Errorf("%w: circular OpenAPI $ref %s", ErrInvalidArgument, ref)
			}
			resolved, ok := lookupJSONPointer(root, ref)
			if !ok {
				return nil, fmt.Errorf("%w: unresolved OpenAPI $ref %s", ErrInvalidArgument, ref)
			}
			refs[ref] = true
			nextSeen := mapsClone(seen)
			nextSeen[ref] = true
			target, err := resolveOpenAPIValue(root, resolved, refs, nextSeen, kind)
			if err != nil {
				return nil, err
			}
			version, _ := root["openapi"].(string)
			if !strings.HasPrefix(version, "3.1.") {
				return target, nil
			}
			siblings := maps.Clone(typed)
			delete(siblings, "$ref")
			if kind == refSchema && len(siblings) > 0 {
				// 3.1 Schema 的同级关键字与引用共同生效，不能用覆盖合并削弱约束。
				additional, err := resolveOpenAPIValue(root, siblings, refs, seen, refSchema)
				if err != nil {
					return nil, err
				}
				// 同级关键字保持原作用域，让 unevaluatedProperties 等仍能读取引用产生的注解。
				object := additional.(map[string]any)
				allOf, _ := object["allOf"].([]any)
				object["allOf"] = append([]any{target}, allOf...)
				return object, nil
			}
			if kind == refObject {
				if object, ok := asMap(target); ok {
					for _, key := range []string{"summary", "description"} {
						if value, exists := siblings[key]; exists {
							object[key] = normalizeValue(value)
						}
					}
				}
			}
			return target, nil
		}
		out := make(map[string]any, len(typed))
		for _, key := range keys(typed) {
			resolved, err := resolveOpenAPIValue(root, typed[key], refs, seen, childRefKind(kind, key))
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			childKind := kind
			if kind == refSchemaArray {
				childKind = refSchema
			}
			resolved, err := resolveOpenAPIValue(root, item, refs, seen, childKind)
			if err != nil {
				return nil, err
			}
			out = append(out, resolved)
		}
		return out, nil
	default:
		return typed, nil
	}
}

func childRefKind(parent refValueKind, key string) refValueKind {
	if parent == refSchemaMap {
		return refSchema
	}
	if parent == refObjectMap {
		return refObject
	}
	if parent == refSchema {
		switch key {
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
			return refSchemaMap
		case "allOf", "anyOf", "oneOf", "prefixItems":
			return refSchemaArray
		case "items", "additionalItems", "additionalProperties", "unevaluatedProperties", "unevaluatedItems", "propertyNames", "contains", "not", "if", "then", "else":
			return refSchema
		default:
			return refLiteral
		}
	}
	switch key {
	case "schema":
		return refSchema
	case "content", "headers", "examples", "responses":
		return refObjectMap
	case "example", "default", "enum", "const", "value":
		return refLiteral
	default:
		return refObject
	}
}

func resolveSecuritySchemes(root map[string]any, security any) (map[string]any, error) {
	out := map[string]any{}
	components, _ := asMap(root["components"])
	schemes, _ := asMap(components["securitySchemes"])
	requirements, _ := security.([]any)
	for _, requirement := range requirements {
		entry, _ := asMap(requirement)
		for _, name := range keys(entry) {
			definition, exists := schemes[name]
			if !exists {
				continue
			}
			resolved, err := resolveRefs(root, definition, map[string]bool{}, map[string]bool{})
			if err != nil {
				return nil, err
			}
			out[name] = resolved
		}
	}
	return out, nil
}

func endpointSecuritySchemes(endpoint Endpoint) any {
	operation, _ := asMap(endpoint.NormalizedOperation)
	definitions, _ := asMap(operation["securitySchemes"])
	if len(definitions) == 0 {
		return nil
	}
	return definitions
}

func lookupJSONPointer(root map[string]any, ref string) (any, bool) {
	current := any(root)
	for part := range strings.SplitSeq(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = currentMap[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func effectiveValue(root, pathItem, op map[string]any, key string) (any, bool) {
	if value, ok := op[key]; ok {
		return value, true
	}
	if value, ok := pathItem[key]; ok {
		return value, true
	}
	value, ok := root[key]
	return value, ok
}

func dropNil(in map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range keys(in) {
		value := in[key]
		if value == nil {
			continue
		}
		if stringsValue, ok := value.([]string); ok && len(stringsValue) == 0 {
			continue
		}
		out[key] = value
	}
	return out
}

func refsList(refs map[string]bool) []any {
	keys := make([]string, 0, len(refs))
	for ref := range refs {
		keys = append(keys, ref)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, ref := range keys {
		out = append(out, ref)
	}
	return out
}

func asMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	return nil, false
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = normalizeValue(value)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[fmt.Sprint(key)] = normalizeValue(value)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeValue(item))
		}
		return out
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	case uint:
		return float64(typed)
	case uint64:
		return float64(typed)
	case uint32:
		return float64(typed)
	default:
		return typed
	}
}

func mapsClone(in map[string]bool) map[string]bool {
	return maps.Clone(in)
}
