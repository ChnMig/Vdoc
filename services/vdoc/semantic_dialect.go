package vdoc

import (
	"maps"
	"strings"
)

func endpointForComparison(endpoint Endpoint) Endpoint {
	operation, _ := asMap(endpoint.NormalizedOperation)
	version, _ := operation["openapi"].(string)
	legacy30 := strings.HasPrefix(version, "3.0.")
	if !legacy30 && !strings.HasPrefix(version, "3.1.") {
		return endpoint
	}
	// 只改变比较视图，详情保留各方言的原始关键字和字面值。
	endpoint.Parameters, _ = schemaDialectView(endpoint.Parameters, refObject, legacy30)
	endpoint.RequestBody, _ = schemaDialectView(endpoint.RequestBody, refObject, legacy30)
	endpoint.Responses, _ = schemaDialectView(endpoint.Responses, refResponsesMap, legacy30)
	return endpoint
}

func schemaDialectView(value any, kind refValueKind, legacy30 bool) (any, bool) {
	if kind == refLiteral {
		return value, false
	}
	switch typed := value.(type) {
	case map[string]any:
		var out map[string]any
		for key, item := range typed {
			if kind == refSchema && !legacy30 && key == "nullable" {
				if out == nil {
					out = maps.Clone(typed)
				}
				delete(out, key)
				continue
			}
			if kind == refSchema && legacy30 {
				if key == "exclusiveMinimum" || key == "exclusiveMaximum" {
					if exclusive, ok := item.(bool); ok {
						bound := "minimum"
						if key == "exclusiveMaximum" {
							bound = "maximum"
						}
						if out == nil {
							out = maps.Clone(typed)
						}
						delete(out, key)
						if exclusive {
							if limit, exists := typed[bound]; exists {
								out[key] = limit
								delete(out, bound)
							}
						}
						continue
					}
				}
			}
			if next, changed := schemaDialectView(item, childRefKind(kind, key), legacy30); changed {
				if out == nil {
					out = maps.Clone(typed)
				}
				out[key] = next
			}
		}
		if out != nil {
			return out, true
		}
	case []any:
		var out []any
		if kind == refSchemaArray {
			kind = refSchema
		}
		for i, item := range typed {
			if next, changed := schemaDialectView(item, kind, legacy30); changed {
				if out == nil {
					out = append([]any(nil), typed...)
				}
				out[i] = next
			}
		}
		if out != nil {
			return out, true
		}
	}
	return value, false
}
