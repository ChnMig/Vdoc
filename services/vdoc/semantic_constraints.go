package vdoc

import (
	"encoding/json"
	"math/big"
	"sort"
	"strings"
)

// 明确比较接受范围；无法可靠证明兼容的关键字保留人工检查项。
func (b *semanticDiffBuilder) compareSchemaConstraints(change int, endpoint Endpoint, location string, oldSchema, newSchema any, response bool) {
	if oldSchema == nil || newSchema == nil {
		return
	}
	old, oldOK := asMap(oldSchema)
	next, nextOK := asMap(newSchema)
	if !oldOK || !nextOK {
		if !valuesEqual(oldSchema, newSchema) {
			b.manualSchemaChange(change, endpoint, location, oldSchema, newSchema)
		}
		return
	}
	b.compareNumericBounds(change, endpoint, location, old, next, response)
	for _, key := range []string{"minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties"} {
		a, aOK := old[key]
		z, zOK := next[key]
		if valuesEqual(a, z) {
			continue
		}
		narrower := zOK
		if aOK && zOK {
			an, anOK := constraintNumber(a)
			zn, znOK := constraintNumber(z)
			if !anOK || !znOK {
				b.manualSchemaChange(change, endpoint, location+"."+key, a, z)
				continue
			}
			comparison := an.Cmp(zn)
			if comparison == 0 {
				continue
			}
			narrower = comparison < 0
			if strings.Contains(strings.ToLower(key), "max") {
				narrower = !narrower
			}
		}
		breaking := narrower != response
		b.add(change, compatibilitySeverity(breaking), endpoint, location+"."+key, "Schema constraint changed", breaking, a, z)
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		a, z := schemaAlternatives(old[key]), schemaAlternatives(next[key])
		if valuesEqual(a, z) {
			continue
		}
		removed, added := setDifference(a, z), setDifference(z, a)
		// 替换分支无法仅按增删推断接受范围；oneOf 增删还要求分支类型互斥。
		if a == nil || z == nil || (len(removed) > 0 && len(added) > 0) || (key == "oneOf" && (!disjointTypes(old[key]) || !disjointTypes(next[key]))) {
			b.manualSchemaChange(change, endpoint, location+"."+key, old[key], next[key])
			continue
		}
		breaking := (!response && len(removed) > 0) || (response && len(added) > 0)
		b.add(change, compatibilitySeverity(breaking), endpoint, location+"."+key, "Schema alternatives changed", breaking, old[key], next[key])
	}
	for _, key := range []string{"properties"} {
		a, _ := asMap(old[key])
		z, _ := asMap(next[key])
		for _, name := range keys(z) {
			if previous, ok := a[name]; ok {
				b.compareSchemaConstraints(change, endpoint, location+"."+key+"."+schemaPropertyName(name), previous, z[name], response)
			}
		}
	}
	if old["items"] != nil || next["items"] != nil {
		b.compareSchemaConstraints(change, endpoint, location+".items", unconstrainedSchema(old["items"]), unconstrainedSchema(next["items"]), response)
	}
	// 未实现的关键字与组合约束不能被悄悄判成零差异。
	a, z := unsupportedSchemaKeywords(old), unsupportedSchemaKeywords(next)
	if !valuesEqual(a, z) {
		b.manualSchemaChange(change, endpoint, location, a, z)
	}
}

func (b *semanticDiffBuilder) manualSchemaChange(change int, endpoint Endpoint, location string, oldValue, newValue any) {
	b.manualContractChange(change, endpoint, location, "Schema compatibility requires manual review", oldValue, newValue)
}

func (b *semanticDiffBuilder) manualContractChange(change int, endpoint Endpoint, location, message string, oldValue, newValue any) {
	b.add(change, SeverityWarning, endpoint, location, message, false, oldValue, newValue)
	b.items[len(b.items)-1].MustHandle = true
}

func constraintNumber(value any) (*big.Rat, bool) {
	data, err := json.Marshal(normalizeValue(value))
	// 防止任意指数导致大整数分配；超出有界精确比较能力时转人工检查。
	if err != nil || len(data) > 256 {
		return nil, false
	}
	if i := strings.IndexAny(string(data), "eE"); i >= 0 {
		exponent, ok := new(big.Int).SetString(strings.TrimPrefix(string(data[i+1:]), "+"), 10)
		if !ok || !exponent.IsInt64() || exponent.Int64() < -1024 || exponent.Int64() > 1024 {
			return nil, false
		}
	}
	return new(big.Rat).SetString(string(data))
}

func unsupportedSchemaKeywords(schema map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range schema {
		if key == "allOf" {
			if value := allOfConstraintRemainder(value); value != nil {
				out[key] = value
			}
			continue
		}
		switch key {
		case "type", "nullable", "enum", "const", "properties", "required", "items", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties", "anyOf", "oneOf", "title", "description", "example", "examples", "$comment", "$id", "$schema", "default":
			continue
		}
		if strings.HasPrefix(key, "x-") {
			continue
		}
		out[key] = canonicalSchemaKeyword(key, value)
	}
	return out
}

// 只在 Schema 对象中忽略注释；字段名映射和 enum/const 字面值必须保留所有键。
func canonicalSchemaConstraint(value any) any {
	v, ok := asMap(value)
	if !ok {
		return normalizeValue(value)
	}
	out := map[string]any{}
	for key, item := range v {
		switch key {
		case "description", "title", "example", "examples", "default", "$comment":
			continue
		}
		if strings.HasPrefix(key, "x-") {
			continue
		}
		out[key] = canonicalSchemaKeyword(key, item)
	}
	canonicalNumericBounds(out)
	return out
}

func canonicalSchemaKeyword(key string, value any) any {
	switch key {
	case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
		if entries, ok := asMap(value); ok {
			out := map[string]any{}
			for name, schema := range entries {
				out[name] = canonicalSchemaConstraint(schema)
			}
			return out
		}
	case "anyOf", "oneOf", "allOf":
		return schemaAlternatives(value)
	case "required", "type", "enum":
		return canonicalSet(value, false)
	case "items", "prefixItems":
		if entries, ok := value.([]any); ok {
			out := make([]any, len(entries))
			for i, schema := range entries {
				out[i] = canonicalSchemaConstraint(schema)
			}
			return out
		}
		return canonicalSchemaConstraint(value)
	case "additionalProperties", "unevaluatedProperties", "additionalItems", "unevaluatedItems", "contains", "propertyNames", "not", "if", "then", "else":
		return canonicalSchemaConstraint(value)
	}
	// 未知关键字按字面值保留，不能假定内部 map 也是 Schema。
	return normalizeValue(value)
}

func schemaAlternatives(value any) []string { return canonicalSet(value, true) }

func canonicalSet(value any, schemas bool) []string {
	items, ok := value.([]any)
	if !ok {
		if value == nil {
			return nil
		}
		items = []any{value}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if schemas {
			item = canonicalSchemaConstraint(item)
		} else {
			item = normalizeValue(item)
		}
		data, _ := json.Marshal(item)
		out = append(out, string(data))
	}
	sort.Strings(out)
	return out
}

func setDifference(a, z []string) []string {
	other := map[string]bool{}
	for _, item := range z {
		other[item] = true
	}
	out := []string{}
	for _, item := range a {
		if !other[item] {
			out = append(out, item)
		}
	}
	return out
}

func disjointTypes(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	seen := map[string]bool{}
	for _, item := range items {
		types := schemaType(item)
		if types == "" || types == "never" {
			return false
		}
		for _, name := range strings.Split(types, "|") {
			if name == "integer" {
				name = "number"
			}
			if seen[name] {
				return false
			}
			seen[name] = true
		}
	}
	return true
}

func allOfConstraintRemainder(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			switch key {
			case "type", "nullable", "enum", "const", "required", "title", "description", "example", "examples", "default", "$comment":
				continue
			}
			if strings.HasPrefix(key, "x-") {
				continue
			}
			if key == "properties" {
				properties, _ := asMap(item)
				remaining := map[string]any{}
				for name, property := range properties {
					if rest := allOfConstraintRemainder(property); rest != nil {
						remaining[name] = rest
					}
				}
				if len(remaining) > 0 {
					out[key] = remaining
				}
			} else if key == "items" || key == "allOf" {
				if rest := allOfConstraintRemainder(item); rest != nil {
					out[key] = rest
				}
			} else {
				out[key] = canonicalSchemaKeyword(key, item)
			}
		}
		canonicalNumericBounds(out)
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := []any{}
		for _, item := range v {
			if rest := allOfConstraintRemainder(item); rest != nil {
				out = append(out, rest)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return canonicalSet(out, false)
	default:
		return value
	}
}
