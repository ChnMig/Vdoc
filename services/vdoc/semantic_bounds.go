package vdoc

import "math/big"

type numericBound struct {
	number    *big.Rat
	value     any
	exclusive bool
}

func (b *semanticDiffBuilder) compareNumericBounds(change int, endpoint Endpoint, location string, old, next map[string]any, response bool) {
	for _, keys := range [][2]string{{"minimum", "exclusiveMinimum"}, {"maximum", "exclusiveMaximum"}} {
		a, z := boundKeywords(old, keys), boundKeywords(next, keys)
		if valuesEqual(a, z) {
			continue
		}
		lower := keys[0] == "minimum"
		before, oldOK := effectiveNumericBound(a, keys, lower)
		after, newOK := effectiveNumericBound(z, keys, lower)
		if !oldOK || !newOK {
			b.manualSchemaChange(change, endpoint, location+"."+keys[0], a, z)
			continue
		}
		comparison := compareNumericBound(before, after, lower)
		if comparison == 0 {
			continue
		}
		// 正数表示接受范围收窄；响应的兼容方向与请求相反。
		breaking := (comparison > 0) != response
		b.add(change, compatibilitySeverity(breaking), endpoint, location+"."+keys[0], "Schema constraint changed", breaking, a, z)
	}
}

func boundKeywords(schema map[string]any, keys [2]string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if value, exists := schema[key]; exists {
			out[key] = value
		}
	}
	return out
}

func effectiveNumericBound(values map[string]any, keys [2]string, lower bool) (numericBound, bool) {
	var result numericBound
	for i, key := range keys {
		value, exists := values[key]
		if !exists {
			continue
		}
		number, ok := constraintNumber(value)
		if !ok {
			return numericBound{}, false
		}
		next := numericBound{number: number, value: value, exclusive: i == 1}
		if compareNumericBound(result, next, lower) > 0 {
			result = next
		}
	}
	return result, true
}

// 仅处理新建的 Schema 比较视图，保留有效边界的原始数值精度。
func canonicalNumericBounds(schema map[string]any) {
	for _, keys := range [][2]string{{"minimum", "exclusiveMinimum"}, {"maximum", "exclusiveMaximum"}} {
		bound, ok := effectiveNumericBound(schema, keys, keys[0] == "minimum")
		if !ok || bound.number == nil {
			continue
		}
		delete(schema, keys[0])
		delete(schema, keys[1])
		key := keys[0]
		if bound.exclusive {
			key = keys[1]
		}
		schema[key] = bound.value
	}
}

// 比较约束强度，而不是关键字是否出现，避免把等价边界和冗余条件当成变化。
func compareNumericBound(before, after numericBound, lower bool) int {
	if before.number == nil {
		if after.number == nil {
			return 0
		}
		return 1
	}
	if after.number == nil {
		return -1
	}
	comparison := after.number.Cmp(before.number)
	if !lower {
		comparison = -comparison
	}
	if comparison == 0 && before.exclusive != after.exclusive {
		if after.exclusive {
			return 1
		}
		return -1
	}
	return comparison
}
