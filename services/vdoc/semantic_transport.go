package vdoc

import "strings"

// 地址、编码和响应头变化不能仅用请求/响应 Schema 推导兼容性。
func (b *semanticDiffBuilder) compareMediaEncoding(change int, endpoint Endpoint, location string, before, after any) {
	a, z := contentEntries(before), contentEntries(after)
	for _, media := range sortedStringKeys(z) {
		old, existed := a[media]
		if !existed {
			continue // 媒体整体的增删由调用方报告。
		}
		oldEntry, _ := asMap(old)
		newEntry, _ := asMap(z[media])
		oldEncoding, newEncoding := canonicalEncoding(oldEntry["encoding"]), canonicalEncoding(newEntry["encoding"])
		if !valuesEqual(oldEncoding, newEncoding) {
			b.manualContractChange(change, endpoint, location+"."+media+".encoding", "Media encoding changed; compatibility requires manual review", oldEncoding, newEncoding)
		}
	}
}

func contentEntries(value any) map[string]any {
	object, _ := asMap(value)
	content, _ := asMap(object["content"])
	return content
}

func responseHeaders(value any) map[string]any {
	object, _ := asMap(value)
	return canonicalHeaders(object["headers"])
}

func canonicalHeaders(value any) map[string]any {
	headers, _ := asMap(value)
	out := map[string]any{}
	for name, header := range headers {
		// Header 名称不区分大小写；OpenAPI 明确忽略此处的 Content-Type。
		if strings.EqualFold(name, "Content-Type") {
			continue
		}
		out[strings.ToLower(name)] = canonicalHeader(header)
	}
	return out
}

func canonicalHeader(value any) any {
	header, ok := asMap(value)
	if !ok {
		return normalizeValue(value)
	}
	out := map[string]any{}
	for key, item := range header {
		if key == "description" || key == "example" || key == "examples" || key == "deprecated" || strings.HasPrefix(key, "x-") {
			continue
		}
		switch key {
		case "schema":
			out[key] = canonicalSchemaConstraint(item)
		case "content":
			content, _ := asMap(item)
			mediaSchemas := map[string]any{}
			for media, entry := range content {
				object, _ := asMap(entry)
				mediaSchemas[media] = canonicalSchemaConstraint(unconstrainedSchema(object["schema"]))
			}
			out[key] = mediaSchemas
		default:
			out[key] = normalizeValue(item)
		}
	}
	return out
}

func canonicalEncoding(value any) map[string]any {
	entries, _ := asMap(value)
	out := map[string]any{}
	for name, item := range entries {
		entry, ok := asMap(item)
		if !ok {
			out[name] = normalizeValue(item)
			continue
		}
		fields := map[string]any{}
		for key, value := range entry {
			if strings.HasPrefix(key, "x-") {
				continue
			}
			if key == "headers" {
				fields[key] = canonicalHeaders(value)
			} else {
				fields[key] = normalizeValue(value)
			}
		}
		out[name] = fields
	}
	return out
}

func canonicalServers(value any) []any {
	servers, _ := value.([]any)
	if len(servers) == 0 {
		servers = []any{map[string]any{"url": "/"}}
	}
	out := make([]any, 0, len(servers))
	for _, item := range servers {
		server, _ := asMap(item)
		entry := map[string]any{"url": server["url"]}
		variables, _ := asMap(server["variables"])
		contracts := map[string]any{}
		for name, variable := range variables {
			object, _ := asMap(variable)
			contract := map[string]any{"default": object["default"]}
			if allowed, exists := object["enum"]; exists {
				contract["enum"] = canonicalSet(allowed, false)
			}
			contracts[name] = contract
		}
		entry["variables"] = contracts
		out = append(out, entry)
	}
	return out
}
