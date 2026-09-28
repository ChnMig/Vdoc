package shared

import (
	"encoding/json"
	app "vdoc/appstore"
)

// 展示字段由服务端直接序列化，避免浏览器 JSON 解码把契约数字舍入。
func exactJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

func endpointJSONPreview(v *app.Endpoint) map[string]string {
	out := map[string]string{}
	for key, value := range map[string]any{"parameters": v.Parameters, "request_body": v.RequestBody, "responses": v.Responses, "security": v.Security, "servers": v.Servers, "normalized_operation": v.NormalizedOperation, "schema_refs": v.SchemaRefs} {
		if text := exactJSON(value); text != "" {
			out[key] = text
		}
	}
	return out
}
