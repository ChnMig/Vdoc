package vdoc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"vdoc/utils/jsonvalue"

	yaml "go.yaml.in/yaml/v3"
)

var openAPIMethods = []string{"get", "post", "put", "patch", "delete", "options", "head", "trace"}

type ParsedOpenAPI struct {
	SchemaFormat int
	Normalized   string
	Endpoints    []Endpoint
}

func ParseOpenAPI(content string) (ParsedOpenAPI, error) {
	return ParseOpenAPIContext(context.Background(), content)
}

func ParseOpenAPIContext(ctx context.Context, content string) (ParsedOpenAPI, error) {
	r := &openAPIResolver{ctx: ctx}
	if len(content) > maxOpenAPIBytes {
		return ParsedOpenAPI{}, fmt.Errorf("%w: OpenAPI source exceeds size limit", ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return ParsedOpenAPI{}, err
	}
	raw, err := r.decode(content)
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
		if strings.HasPrefix(pathName, "x-") {
			continue
		}
		if !strings.HasPrefix(pathName, "/") {
			return ParsedOpenAPI{}, fmt.Errorf("%w: path must start with /", ErrInvalidArgument)
		}
		pathItem, ok := asMap(paths[pathName])
		if !ok {
			return ParsedOpenAPI{}, fmt.Errorf("%w: path item must be an object", ErrInvalidArgument)
		}
		pathItem, err = r.pathItem(raw, pathItem, map[string]bool{}, 0)
		if err != nil {
			return ParsedOpenAPI{}, err
		}
		for _, method := range openAPIMethods {
			op, ok := asMap(pathItem[method])
			if !ok {
				continue
			}
			endpoint, err := r.extractEndpoint(raw, pathName, method, pathItem, op)
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
	return (&openAPIResolver{ctx: context.Background()}).decode(content)
}

// 输入树和展开树共用累计预算，重复引用与 YAML alias 也会逐次计费。
const (
	maxOpenAPIBytes = 16 << 20
	maxOpenAPINodes = 200000
	maxOpenAPIDepth = 128
)

type openAPIResolver struct {
	ctx          context.Context
	nodes, bytes int
	outputBytes  int
}

func (r *openAPIResolver) visit(value any, depth int) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	r.nodes++
	r.bytes += 8
	switch v := value.(type) {
	case string:
		if strings.ContainsRune(v, 0) {
			return fmt.Errorf("%w: OpenAPI strings cannot contain NUL (unsupported by JSONB)", ErrInvalidArgument)
		}
		r.bytes += len(v) * 6 // JSON 转义的最坏字节数
	case json.Number:
		r.bytes += len(v)
	case map[string]any:
		for key := range v {
			if strings.ContainsRune(key, 0) {
				return fmt.Errorf("%w: OpenAPI object keys cannot contain NUL (unsupported by JSONB)", ErrInvalidArgument)
			}
			r.bytes += len(key)*6 + 4
		}
	}
	if r.nodes > maxOpenAPINodes || r.bytes > maxOpenAPIBytes || depth > maxOpenAPIDepth {
		return fmt.Errorf("%w: OpenAPI expansion exceeds node, byte or depth limit", ErrInvalidArgument)
	}
	return nil
}

func (r *openAPIResolver) decode(content string) (map[string]any, error) {
	var raw any
	if err := jsonvalue.Decode([]byte(content), &raw); err != nil {
		var node yaml.Node
		decoder := yaml.NewDecoder(strings.NewReader(content))
		if err := decoder.Decode(&node); err != nil {
			return nil, fmt.Errorf("%w: invalid OpenAPI JSON/YAML", ErrInvalidArgument)
		}
		var extra yaml.Node
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("%w: expected one OpenAPI document", ErrInvalidArgument)
		}
		var err error
		raw, err = r.yamlValue(&node, map[*yaml.Node]bool{}, 0)
		if err != nil {
			return nil, err
		}
	}
	converted, err := r.value(nil, raw, nil, nil, refLiteral, 0)
	if err != nil {
		return nil, err
	}
	root, ok := converted.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: OpenAPI document must be an object", ErrInvalidArgument)
	}
	return root, nil
}

func (r *openAPIResolver) yamlValue(node *yaml.Node, seen map[*yaml.Node]bool, depth int) (any, error) {
	if err := r.visit(node.Value, depth); err != nil {
		return nil, err
	}
	if seen[node] {
		return nil, fmt.Errorf("%w: circular YAML alias", ErrInvalidArgument)
	}
	seen[node] = true
	defer delete(seen, node)
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return nil, fmt.Errorf("%w: invalid YAML document", ErrInvalidArgument)
		}
		return r.yamlValue(node.Content[0], seen, depth+1)
	case yaml.AliasNode:
		return r.yamlValue(node.Alias, seen, depth+1)
	case yaml.MappingNode:
		out := map[string]any{}
		explicit := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if node.Content[i].Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("%w: YAML key must be scalar", ErrInvalidArgument)
			}
			merge := node.Content[i].Tag == "!!merge"
			if !merge && explicit[key] {
				return nil, fmt.Errorf("%w: duplicate YAML key %s", ErrInvalidArgument, key)
			}
			value, err := r.yamlValue(node.Content[i+1], seen, depth+1)
			if err != nil {
				return nil, err
			}
			if !merge {
				out[key] = value
				explicit[key] = true
				continue
			}
			sources, ok := value.([]any)
			if !ok {
				sources = []any{value}
			}
			for _, source := range sources {
				mapping, ok := asMap(source)
				if !ok {
					return nil, fmt.Errorf("%w: YAML merge must reference a mapping", ErrInvalidArgument)
				}
				for name, item := range mapping {
					if _, exists := out[name]; !exists {
						out[name] = item
					}
				}
			}
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := r.yamlValue(child, seen, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!int":
			if len(node.Value) > 4096 {
				return nil, fmt.Errorf("%w: number exceeds digit limit", ErrInvalidArgument)
			}
			value := strings.ReplaceAll(node.Value, "_", "")
			integer, ok := new(big.Int).SetString(value, 0)
			if !ok {
				integer, ok = new(big.Int).SetString(value, 10)
			}
			if !ok {
				return nil, fmt.Errorf("%w: invalid YAML integer", ErrInvalidArgument)
			}
			number, err := jsonvalue.Number(integer.String())
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
			}
			return number, nil
		case "!!float":
			value := strings.TrimPrefix(strings.ReplaceAll(node.Value, "_", ""), "+")
			exponent := ""
			if index := strings.IndexAny(value, "eE"); index >= 0 {
				value, exponent = value[:index], value[index:]
			}
			sign := ""
			if strings.HasPrefix(value, "-") {
				sign, value = "-", value[1:]
			}
			// YAML 允许前导零、缺失整数部分和小数点后直接接指数。
			value = strings.TrimLeft(value, "0")
			if value == "" {
				value = "0"
			}
			if strings.HasPrefix(value, ".") {
				value = "0" + value
			}
			if strings.HasSuffix(value, ".") {
				value += "0"
			}
			number, err := jsonvalue.Number(sign + value + exponent)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid YAML number", ErrInvalidArgument)
			}
			return number, nil
		case "!!null":
			return nil, nil
		case "!!bool":
			return strings.EqualFold(node.Value, "true"), nil
		default:
			return node.Value, nil
		}
	}
	return nil, fmt.Errorf("%w: invalid YAML node", ErrInvalidArgument)
}

// Path Item 的引用只解析路径级对象，schema 留给对应 operation 的有界解析。
func (r *openAPIResolver) pathItem(root, item map[string]any, seen map[string]bool, depth int) (map[string]any, error) {
	if err := r.visit(item, depth); err != nil {
		return nil, err
	}
	ref, ok := item["$ref"].(string)
	if !ok {
		return item, nil
	}
	if !strings.HasPrefix(ref, "#") || seen[ref] {
		return nil, fmt.Errorf("%w: invalid or circular Path Item reference %s", ErrInvalidArgument, ref)
	}
	target, ok := lookupJSONPointer(root, ref)
	object, objectOK := asMap(target)
	if !ok || !objectOK {
		return nil, fmt.Errorf("%w: unresolved Path Item reference %s", ErrInvalidArgument, ref)
	}
	seen[ref] = true
	resolved, err := r.pathItem(root, object, seen, depth+1)
	if err != nil {
		return nil, err
	}
	out := maps.Clone(resolved)
	for key, value := range item {
		if key == "$ref" {
			continue
		}
		if old, exists := out[key]; exists && !valuesEqual(old, value) {
			return nil, fmt.Errorf("%w: ambiguous Path Item reference sibling %s", ErrInvalidArgument, key)
		}
		out[key] = value
	}
	return out, nil
}

func (r *openAPIResolver) extractEndpoint(root map[string]any, pathName, method string, pathItem, op map[string]any) (Endpoint, error) {
	refs := map[string]bool{}
	endpoint := Endpoint{Method: strings.ToUpper(method), Path: pathName}
	endpoint.OperationID, _ = op["operationId"].(string)
	endpoint.Summary, _ = op["summary"].(string)
	endpoint.Deprecated, _ = op["deprecated"].(bool)
	endpoint.Tags = stringSlice(op["tags"])

	parameters, err := r.resolveParameters(root, pathItem["parameters"], op["parameters"], refs)
	if err != nil {
		return Endpoint{}, err
	}
	if len(parameters) > 0 {
		endpoint.Parameters = parameters
	}
	if requestBody, ok, err := r.resolveOptional(root, op["requestBody"], refs, refObject); err != nil {
		return Endpoint{}, err
	} else if ok {
		endpoint.RequestBody = requestBody
	}
	if responses, ok, err := r.resolveOptional(root, op["responses"], refs, refResponsesMap); err != nil {
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
	securitySchemes, err := r.resolveSecuritySchemes(root, endpoint.Security)
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
		"openapi":         root["openapi"],
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
	hashBytes, err := json.Marshal(endpoint.NormalizedOperation)
	if err != nil {
		return Endpoint{}, err
	}
	r.outputBytes += len(hashBytes)
	if r.outputBytes > maxOpenAPIBytes {
		return Endpoint{}, fmt.Errorf("%w: expanded operations exceed size limit", ErrInvalidArgument)
	}
	endpoint.Hash = sha(string(hashBytes))
	return endpoint, nil
}

func (r *openAPIResolver) resolveParameters(root map[string]any, pathParameters, operationParameters any, refs map[string]bool) ([]any, error) {
	merged := []any{}
	positions := map[string]int{}
	for _, source := range []any{pathParameters, operationParameters} {
		items, ok := source.([]any)
		if !ok {
			continue
		}
		identities := map[string]bool{}
		for _, item := range items {
			resolved, err := r.value(root, item, refs, map[string]bool{}, refObject, 0)
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

func (r *openAPIResolver) resolveOptional(root map[string]any, value any, refs map[string]bool, kind refValueKind) (any, bool, error) {
	if value == nil {
		return nil, false, nil
	}
	resolved, err := r.value(root, value, refs, map[string]bool{}, kind, 0)
	if err != nil {
		return nil, false, err
	}
	return resolved, true, nil
}

func resolveRefs(root map[string]any, value any, refs, seen map[string]bool) (any, error) {
	return (&openAPIResolver{ctx: context.Background()}).value(root, value, refs, seen, refObject, 0)
}

type refValueKind int

const (
	refObject refValueKind = iota
	refSchema
	refSchemaMap
	refSchemaArray
	refObjectMap
	refResponsesMap
	refLiteral
)

func (r *openAPIResolver) value(root map[string]any, value any, refs, seen map[string]bool, kind refValueKind, depth int) (any, error) {
	if err := r.visit(value, depth); err != nil {
		return nil, err
	}
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok && kind != refLiteral && kind != refSchemaMap && kind != refObjectMap && kind != refResponsesMap {
			if !strings.HasPrefix(ref, "#") {
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
			target, err := r.value(root, resolved, refs, nextSeen, kind, depth+1)
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
				additional, err := r.value(root, siblings, refs, seen, refSchema, depth+1)
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
			resolved, err := r.value(root, typed[key], refs, seen, childRefKind(kind, key), depth+1)
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
			resolved, err := r.value(root, item, refs, seen, childKind, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, resolved)
		}
		return out, nil
	case json.Number:
		value, err := jsonvalue.Number(string(typed))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
		}
		return value, nil
	default:
		return typed, nil
	}
}

func childRefKind(parent refValueKind, key string) refValueKind {
	if parent == refLiteral {
		return refLiteral
	}
	if parent == refSchemaMap {
		return refSchema
	}
	if parent == refObjectMap {
		return refObject
	}
	if parent == refResponsesMap {
		if strings.HasPrefix(key, "x-") {
			return refLiteral
		}
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
	if strings.HasPrefix(key, "x-") {
		return refLiteral
	}
	switch key {
	case "schema":
		return refSchema
	case "content", "headers", "examples", "encoding":
		return refObjectMap
	case "responses":
		return refResponsesMap
	case "example", "default", "enum", "const", "value":
		return refLiteral
	default:
		return refObject
	}
}

func (r *openAPIResolver) resolveSecuritySchemes(root map[string]any, security any) (map[string]any, error) {
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
			resolved, err := r.value(root, definition, map[string]bool{}, map[string]bool{}, refObject, 0)
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
	if !strings.HasPrefix(ref, "#") {
		return nil, false
	}
	pointer, err := url.PathUnescape(strings.TrimPrefix(ref, "#"))
	if err != nil {
		return nil, false
	}
	current := any(root)
	if pointer == "" {
		return current, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	for part := range strings.SplitSeq(pointer[1:], "/") {
		for i := 0; i < len(part); i++ {
			if part[i] == '~' {
				if i+1 >= len(part) || (part[i+1] != '0' && part[i+1] != '1') {
					return nil, false
				}
				i++
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[part]
			if !ok {
				return nil, false
			}
		case []any:
			if part == "" || (len(part) > 1 && part[0] == '0') {
				return nil, false
			}
			for _, digit := range part {
				if digit < '0' || digit > '9' {
					return nil, false
				}
			}
			index, err := strconv.Atoi(part)
			if err != nil || index >= len(value) {
				return nil, false
			}
			current = value[index]
		default:
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
	case json.Number:
		return jsonvalue.Normalize(typed)
	case float64:
		return jsonvalue.Normalize(json.Number(strconv.FormatFloat(typed, 'g', -1, 64)))
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
		return json.Number(fmt.Sprint(typed))
	case int64:
		return json.Number(fmt.Sprint(typed))
	case int32:
		return json.Number(fmt.Sprint(typed))
	case uint:
		return json.Number(fmt.Sprint(typed))
	case uint64:
		return json.Number(fmt.Sprint(typed))
	case uint32:
		return json.Number(fmt.Sprint(typed))
	default:
		return typed
	}
}

func mapsClone(in map[string]bool) map[string]bool {
	return maps.Clone(in)
}

func resolveParameters(root map[string]any, pathParameters, operationParameters any, refs map[string]bool) ([]any, error) {
	return (&openAPIResolver{ctx: context.Background()}).resolveParameters(root, pathParameters, operationParameters, refs)
}
func resolveSecuritySchemes(root map[string]any, security any) (map[string]any, error) {
	return (&openAPIResolver{ctx: context.Background()}).resolveSecuritySchemes(root, security)
}
