package vdoc

import (
	"encoding/json"
	"strings"
)

// 保留各对象的原始分支作用域；allOf 中的 additionalProperties 只认识
// 同一分支声明的 properties，不能用合并后的字段表决定额外属性规则。
func schemaObjectNodes(schema any) map[string][]any {
	out := map[string][]any{}
	var collect func(string, any)
	collect = func(path string, value any) {
		out[path] = append(out[path], value)
		object, ok := asMap(value)
		if !ok {
			return
		}
		for _, fragment := range schemaFragments(object) {
			properties, _ := asMap(fragment["properties"])
			for name, property := range properties {
				collect(schemaPath(path, "properties."+schemaPropertyName(name)), property)
			}
			if items, exists := fragment["items"]; exists {
				collect(schemaPath(path, "items"), items)
			}
		}
	}
	if schema != nil {
		collect("", schema)
	}
	return out
}

func changedPropertyCompatibility(field schemaField, oldNodes, newNodes map[string][]any, response bool) (breaking, manual bool) {
	if !field.defined || field.arrayItem {
		return false, false
	}
	oldParent, oldExists := oldNodes[field.parent]
	newParent, newExists := newNodes[field.parent]
	// 新增/删除父字段已有自己的差异；不凭空把其后代当作已有对象中的字段。
	if !oldExists || !newExists {
		return false, false
	}
	oldRule, oldUnknown := objectPropertyRule(oldParent, field.name)
	newRule, newUnknown := objectPropertyRule(newParent, field.name)
	if valuesEqual(canonicalSchemaConstraint(oldRule), canonicalSchemaConstraint(newRule)) && !oldUnknown && !newUnknown {
		return false, false
	}
	if response {
		oldRule, newRule = newRule, oldRule
	}
	if oldUnknown || newUnknown {
		return false, true
	}
	oldSimple, newSimple := simplePropertyRule(oldRule), simplePropertyRule(newRule)
	if oldSimple.empty() || newSimple.universal() {
		return false, false
	}
	if oldSimple.unknown || newSimple.unknown {
		return false, true
	}
	if oldSimple.enum != nil {
		for _, value := range oldSimple.enum {
			if !newSimple.accepts(value) {
				return true, false
			}
		}
		return false, false
	}
	return newSimple.enum != nil || typeChangeBreaking(oldSimple.types, newSimple.types, false), false
}

func propertyDeclarationScopesChanged(field schemaField, oldNodes, newNodes map[string][]any) bool {
	var shape func(any) any
	shape = func(value any) any {
		object, ok := asMap(value)
		if !ok {
			return false
		}
		properties, _ := asMap(object["properties"])
		_, declared := properties[field.name]
		fragments, _ := object["allOf"].([]any)
		children := make([]any, 0, len(fragments))
		for _, fragment := range fragments {
			children = append(children, shape(fragment))
		}
		// 声明可能在 AP/type 相同、pattern/条件不同的分支之间迁移；
		// 保留非声明约束作为作用域锚点；已独立比较的整体上下界不
		// 影响属性归属，避免普通兼容放宽额外生成必须人工检查的项。
		scope := make(map[string]any, len(object))
		for key, value := range object {
			switch key {
			case "properties", "allOf", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties":
				continue
			}
			scope[key] = value
		}
		return map[string]any{"declared": declared, "scope": canonicalSchemaConstraint(scope), "allOf": canonicalSet(children, false)}
	}
	shapes := func(nodes []any) []string {
		values := make([]any, 0, len(nodes))
		for _, node := range nodes {
			values = append(values, shape(node))
		}
		return canonicalSet(values, false)
	}
	return !valuesEqual(shapes(oldNodes[field.parent]), shapes(newNodes[field.parent]))
}

func objectPropertyRule(nodes []any, name string) (any, bool) {
	rules := []any{}
	unknown := false
	var collect func(any)
	collect = func(value any) {
		if allowed, ok := value.(bool); ok {
			if !allowed {
				rules = append(rules, false)
			}
			return
		}
		object, ok := asMap(value)
		if !ok {
			unknown = true
			return
		}
		if types := schemaType(object); types != "" && !strings.Contains("|"+types+"|", "|object|") {
			rules = append(rules, false)
		}
		for key := range object {
			switch key {
			case "type", "nullable", "properties", "required", "additionalProperties", "allOf", "title", "description", "example", "examples", "default", "$comment", "$id", "$schema", "readOnly", "writeOnly":
				continue
			}
			if !strings.HasPrefix(key, "x-") {
				unknown = true
			}
		}
		properties, _ := asMap(object["properties"])
		if property, exists := properties[name]; exists {
			rules = append(rules, property)
		} else if additional, exists := object["additionalProperties"]; exists {
			rules = append(rules, additional)
		}
		if fragments, ok := object["allOf"].([]any); ok {
			for _, fragment := range fragments {
				collect(fragment)
			}
		}
	}
	for _, node := range nodes {
		collect(node)
	}
	return map[string]any{"allOf": rules}, unknown
}

type propertyValueRule struct {
	types   string
	enum    []string
	unknown bool
}

func simplePropertyRule(schema any) propertyValueRule {
	rule := propertyValueRule{}
	var collect func(any)
	collect = func(value any) {
		if allowed, ok := value.(bool); ok {
			if !allowed {
				rule.types = "never"
			}
			return
		}
		object, ok := asMap(value)
		if !ok {
			rule.unknown = true
			return
		}
		rule.types = intersectSchemaTypes(rule.types, schemaType(object))
		if values := enumIdentities(object); values != nil {
			if rule.enum == nil {
				rule.enum = values
			} else {
				rule.enum = setIntersection(rule.enum, values)
			}
		}
		for key := range object {
			switch key {
			case "type", "nullable", "enum", "const", "allOf", "title", "description", "example", "examples", "default", "$comment", "$id", "$schema", "readOnly", "writeOnly":
				continue
			}
			if !strings.HasPrefix(key, "x-") {
				rule.unknown = true
			}
		}
		if fragments, ok := object["allOf"].([]any); ok {
			for _, fragment := range fragments {
				collect(fragment)
			}
		}
	}
	collect(schema)
	if rule.enum != nil {
		filtered := []string{}
		for _, value := range rule.enum {
			if _, known := propertyLiteralType(value); !known {
				rule.unknown = true
				filtered = append(filtered, value)
				continue
			}
			if rule.acceptsType(value) {
				filtered = append(filtered, value)
			}
		}
		rule.enum = filtered
	}
	return rule
}

func (r propertyValueRule) empty() bool {
	return r.types == "never" || (r.enum != nil && len(r.enum) == 0)
}
func (r propertyValueRule) universal() bool { return r.types == "" && r.enum == nil && !r.unknown }

func (r propertyValueRule) accepts(value string) bool {
	if !r.acceptsType(value) {
		return false
	}
	if r.enum == nil {
		return true
	}
	for _, allowed := range r.enum {
		if value == allowed {
			return true
		}
	}
	return false
}

func (r propertyValueRule) acceptsType(value string) bool {
	if r.types == "" {
		return true
	}
	typeName, known := propertyLiteralType(value)
	return known && !typeChangeBreaking(typeName, r.types, false)
}

func propertyLiteralType(value string) (string, bool) {
	typeName := "null"
	switch literal := enumDisplayValue(value).(type) {
	case string:
		typeName = "string"
	case bool:
		typeName = "boolean"
	case map[string]any:
		typeName = "object"
	case []any:
		typeName = "array"
	case json.Number:
		typeName = "number"
		number, ok := constraintNumber(literal)
		if !ok {
			return "", false
		}
		if number.IsInt() {
			typeName = "integer"
		}
	}
	return typeName, true
}

func setIntersection(left, right []string) []string {
	allowed := map[string]bool{}
	for _, item := range right {
		allowed[item] = true
	}
	out := []string{}
	for _, item := range left {
		if allowed[item] {
			out = append(out, item)
		}
	}
	return out
}
