package shared

import (
	"encoding/json"
	"strings"
	"testing"
	app "vdoc/appstore"
)

func TestExactContractJSONSurvivesBrowserDecoding(t *testing.T) {
	for _, raw := range []string{"9007199254740993", "0.123456789012345678901", "1e131071", "1e-16383"} {
		value := json.Number(raw)
		wire := mustMarshalDTO(t, DiffItem(app.DiffItem{OldValue: value, NewValue: raw}))
		var browser map[string]any
		if err := json.Unmarshal(wire, &browser); err != nil && raw != "1e131071" {
			t.Fatal(err)
		}
		// 独立读取新增字符串字段；超出 JS 范围的数字也必须原样保留。
		var dto struct {
			Old string `json:"old_value_json"`
			New string `json:"new_value_json"`
		}
		if err := json.Unmarshal(wire, &dto); err != nil {
			t.Fatal(err)
		}
		if dto.Old != raw || dto.New != `"`+raw+`"` {
			t.Fatalf("literal types/precision lost: %s", wire)
		}
		nested := map[string]any{"description": []any{value, raw}}
		endpoint := &app.Endpoint{Parameters: nested, RequestBody: nested, Responses: nested, Security: nested, Servers: nested, NormalizedOperation: nested, SchemaRefs: nested}
		var loaded struct {
			Preview map[string]string `json:"json_preview"`
		}
		if err := json.Unmarshal(mustMarshalDTO(t, Endpoint(endpoint)), &loaded); err != nil {
			t.Fatal(err)
		}
		if len(loaded.Preview) != 7 {
			t.Fatal("endpoint preview missing sections")
		}
		for key, text := range loaded.Preview {
			if !strings.Contains(text, raw) || !strings.Contains(text, `"`+raw+`"`) {
				t.Fatalf("%s lost precision: %s", key, text)
			}
		}
	}
}
