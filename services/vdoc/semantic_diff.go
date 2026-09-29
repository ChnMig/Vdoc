package vdoc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vdoc/utils/id"
	"vdoc/utils/jsonvalue"
)

type semanticDiffBuilder struct {
	items []DiffItem
}

func (b *semanticDiffBuilder) compareEndpoint(from, to Endpoint) {
	from, to = endpointForComparison(from), endpointForComparison(to)
	if !valuesEqual(endpointMetadata(from), endpointMetadata(to)) {
		b.add(ChangeEndpointModified, SeverityWarning, to, "endpoint", "Endpoint metadata changed", false, endpointMetadata(from), endpointMetadata(to))
	}
	b.compareParameters(from, to)
	b.compareRequestBody(from, to)
	b.compareResponses(from, to)
	if !valuesEqual(optionalCollection(from.Security), optionalCollection(to.Security)) {
		b.add(ChangeSecurityChanged, SeverityWarning, to, "security", "Security requirements changed", false, from.Security, to.Security)
	}
	if !valuesEqual(endpointSecuritySchemes(from), endpointSecuritySchemes(to)) {
		b.add(ChangeSecurityChanged, SeverityWarning, to, "securitySchemes", "Security scheme definitions changed", false, endpointSecuritySchemes(from), endpointSecuritySchemes(to))
	}
	if from.Deprecated != to.Deprecated {
		b.add(ChangeDeprecatedChanged, SeverityInfo, to, "deprecated", "Deprecated status changed", false, from.Deprecated, to.Deprecated)
	}
	if !valuesEqual(canonicalServers(from.Servers), canonicalServers(to.Servers)) {
		b.manualContractChange(ChangeEndpointModified, to, "servers", "Server contract changed; compatibility requires manual review", from.Servers, to.Servers)
	}
}

func (b *semanticDiffBuilder) compareParameters(from, to Endpoint) {
	fp := parametersByIdentity(from.Parameters)
	tp := parametersByIdentity(to.Parameters)
	matchedOld := map[string]bool{}
	matchedNew := map[string]bool{}

	// OpenAPI identifies a parameter by both name and location. Match that
	// identity first so query/header parameters with the same name cannot hide
	// each other's changes.
	for _, key := range sortedStringKeys(tp) {
		oldParam, ok := fp[key]
		if !ok {
			continue
		}
		b.compareParameterPair(to, oldParam, tp[key])
		matchedOld[key] = true
		matchedNew[key] = true
	}

	oldByName := unmatchedParametersByName(fp, matchedOld)
	newByName := unmatchedParametersByName(tp, matchedNew)
	for _, name := range sortedStringUnionKeys(oldByName, newByName) {
		oldKeys := oldByName[name]
		newKeys := newByName[name]
		// A single unmatched parameter on each side is an unambiguous location
		// migration. Preserve the dedicated breaking-change explanation.
		if len(oldKeys) == 1 && len(newKeys) == 1 {
			b.compareParameterPair(to, fp[oldKeys[0]], tp[newKeys[0]])
			continue
		}
		for _, key := range newKeys {
			newParam := tp[key]
			breaking := boolValue(newParam["required"])
			severity := SeverityInfo
			if breaking {
				severity = SeverityBreaking
			}
			b.add(ChangeParameterAdded, severity, to, parameterPath(parameterLocation(newParam), name), "Parameter added", breaking, nil, compactParameterValue(newParam))
		}
		for _, key := range oldKeys {
			oldParam := fp[key]
			b.add(ChangeParameterRemoved, SeverityWarning, to, parameterPath(parameterLocation(oldParam), name), "Parameter removed", false, compactParameterValue(oldParam), nil)
		}
	}
}

func (b *semanticDiffBuilder) compareParameterPair(endpoint Endpoint, oldParam, newParam map[string]any) {
	name, _ := newParam["name"].(string)
	location := parameterLocation(newParam)
	oldLocation := parameterLocation(oldParam)
	if oldLocation != location {
		b.add(ChangeParameterChanged, SeverityBreaking, endpoint, parameterPath(location, name), "Parameter location changed", true, oldLocation, location)
	}
	oldRequired, newRequired := boolValue(oldParam["required"]), boolValue(newParam["required"])
	if oldRequired != newRequired {
		breaking := newRequired
		severity := SeverityWarning
		if breaking {
			severity = SeverityBreaking
		}
		b.add(ChangeParameterChanged, severity, endpoint, parameterPath(location, name), "Parameter required flag changed", breaking, oldRequired, newRequired)
	}
	prefix := parameterPath(location, name)
	oldContent, newContent := mediaSchemas(oldParam), mediaSchemas(newParam)
	if len(oldContent) > 0 || len(newContent) > 0 {
		if len(oldContent) == 0 || len(newContent) == 0 {
			b.manualSchemaChange(ChangeParameterChanged, endpoint, prefix, oldParam, newParam)
		} else {
			for _, media := range sortedStringUnionKeys(oldContent, newContent) {
				oldSchema, oldOK := oldContent[media]
				newSchema, newOK := newContent[media]
				if !oldOK || !newOK {
					b.manualSchemaChange(ChangeParameterChanged, endpoint, prefix+".content."+media, oldSchema, newSchema)
				} else {
					b.compareSchemaFields(ChangeParameterChanged, endpoint, prefix+".content."+media, oldSchema, newSchema, false)
				}
			}
		}
	} else {
		oldSchema, newSchema := unconstrainedSchema(oldParam["schema"]), unconstrainedSchema(newParam["schema"])
		oldType, newType := schemaType(oldSchema), schemaType(newSchema)
		if oldType != newType {
			breaking := typeChangeBreaking(oldType, newType, false)
			b.add(ChangeParameterChanged, compatibilitySeverity(breaking), endpoint, parameterPath(location, name), "Parameter type changed", breaking, oldType, newType)
		}
		b.compareEnumValues(ChangeParameterChanged, endpoint, prefix, oldSchema, newSchema, "Parameter enum value removed")
		b.compareSchemaChildren(ChangeParameterChanged, endpoint, prefix, oldSchema, newSchema, false)
		b.compareSchemaConstraints(ChangeParameterChanged, endpoint, prefix, oldSchema, newSchema, false)
	}
	// 序列化选项会改变线上的参数表示，无法只靠 schema 判断兼容性。
	for _, key := range []string{"style", "explode", "allowReserved", "allowEmptyValue"} {
		if !valuesEqual(oldParam[key], newParam[key]) {
			b.manualSchemaChange(ChangeParameterChanged, endpoint, prefix+"."+key, oldParam[key], newParam[key])
		}
	}
}

func (b *semanticDiffBuilder) compareRequestBody(from, to Endpoint) {
	oldRequired := requestBodyRequired(from.RequestBody)
	newRequired := requestBodyRequired(to.RequestBody)
	if oldRequired != newRequired {
		breaking := newRequired
		severity := SeverityWarning
		if breaking {
			severity = SeverityBreaking
		}
		b.add(ChangeRequestBodyChanged, severity, to, "requestBody.required", "Request body required flag changed", breaking, oldRequired, newRequired)
	}
	fs := mediaSchemas(from.RequestBody)
	ts := mediaSchemas(to.RequestBody)
	for _, media := range sortedStringKeys(ts) {
		newSchema := ts[media]
		oldSchema, ok := fs[media]
		if !ok {
			b.add(ChangeRequestBodyChanged, SeverityWarning, to, "requestBody."+media, "Request body media type added", false, nil, compactSchemaValue(newSchema))
			continue
		}
		b.compareSchemaFields(ChangeRequestBodyChanged, to, "requestBody."+media, oldSchema, newSchema, false)
	}
	b.compareMediaEncoding(ChangeRequestBodyChanged, to, "requestBody", from.RequestBody, to.RequestBody)
	for _, media := range sortedStringKeys(fs) {
		if _, ok := ts[media]; !ok {
			b.add(ChangeRequestBodyChanged, SeverityBreaking, to, "requestBody."+media, "Request body media type removed", true, compactSchemaValue(fs[media]), nil)
		}
	}
}

func (b *semanticDiffBuilder) compareResponses(from, to Endpoint) {
	fs := responseStatuses(from.Responses)
	ts := responseStatuses(to.Responses)
	for _, status := range sortedStringKeys(ts) {
		if _, ok := fs[status]; !ok {
			b.add(ChangeResponseChanged, SeverityInfo, to, "responses."+status, "Response status added", false, nil, ts[status])
		} else {
			oldHeaders, newHeaders := responseHeaders(fs[status]), responseHeaders(ts[status])
			if !valuesEqual(oldHeaders, newHeaders) {
				b.manualContractChange(ChangeResponseChanged, to, "responses."+status+".headers", "Response header contract changed; compatibility requires manual review", oldHeaders, newHeaders)
			}
		}
	}
	for _, status := range sortedStringKeys(fs) {
		if _, ok := ts[status]; !ok {
			breaking := strings.HasPrefix(status, "2")
			severity := SeverityWarning
			if breaking {
				severity = SeverityBreaking
			}
			b.add(ChangeResponseChanged, severity, to, "responses."+status, "Response status removed", breaking, fs[status], nil)
		}
	}
	oldSchemas := responseSchemas(from.Responses)
	newSchemas := responseSchemas(to.Responses)
	for _, key := range sortedStringKeys(newSchemas) {
		newSchema := newSchemas[key]
		oldSchema, ok := oldSchemas[key]
		if !ok {
			b.add(ChangeResponseChanged, SeverityInfo, to, "responses."+key, "Response body added", false, nil, compactSchemaValue(newSchema))
			b.compareSchemaFields(ChangeResponseChanged, to, "responses."+key, nil, newSchema, true)
			continue
		}
		b.compareSchemaFields(ChangeResponseChanged, to, "responses."+key, oldSchema, newSchema, true)
	}
	for _, key := range sortedStringKeys(oldSchemas) {
		if _, ok := newSchemas[key]; !ok {
			b.add(ChangeResponseChanged, SeverityBreaking, to, "responses."+key, "Response body removed", true, compactSchemaValue(oldSchemas[key]), nil)
		}
	}
}

func (b *semanticDiffBuilder) compareSchemaFields(change int, endpoint Endpoint, prefix string, oldSchema, newSchema any, response bool) {
	b.compareSchemaRootType(change, endpoint, prefix, oldSchema, newSchema, response)
	b.compareEnumValues(change, endpoint, prefix, oldSchema, newSchema, "Enum value removed")
	b.compareSchemaChildren(change, endpoint, prefix, oldSchema, newSchema, response)
	b.compareSchemaConstraints(change, endpoint, prefix, oldSchema, newSchema, response)
}

func (b *semanticDiffBuilder) compareSchemaChildren(change int, endpoint Endpoint, prefix string, oldSchema, newSchema any, response bool) {
	oldFields := schemaFieldsForDirection(oldSchema, response)
	newFields := schemaFieldsForDirection(newSchema, response)
	for _, path := range sortedStringKeys(newFields) {
		newField := newFields[path]
		oldField, ok := oldFields[path]
		location := prefix + "." + path
		if !ok {
			itemConstrained := newField.arrayItem && (newField.Type != "" || newField.Enum != nil)
			breaking := !response && (newField.Required || itemConstrained) && newFieldAffectsExistingInput(path, oldFields, newFields)
			severity := SeverityInfo
			message := "Response field added"
			if !response {
				message = "Request body field added"
				if change == ChangeParameterChanged {
					message = "Parameter field added"
				}
				if breaking {
					severity = SeverityBreaking
				}
			}
			b.add(change, severity, endpoint, location, message, breaking, nil, newField.diffValue())
			continue
		}
		if oldField.Type != newField.Type {
			message := fieldTypeChangeMessage(response)
			if change == ChangeParameterChanged {
				message = "Parameter field type changed"
			}
			breaking := typeChangeBreaking(oldField.Type, newField.Type, response)
			b.add(change, compatibilitySeverity(breaking), endpoint, location, message, breaking, oldField.Type, newField.Type)
		}
		if oldField.Required != newField.Required {
			breaking := (!response && newField.Required) || (response && oldField.Required)
			severity := SeverityWarning
			if breaking {
				severity = SeverityBreaking
			}
			message := fieldRequiredChangeMessage(response)
			if change == ChangeParameterChanged {
				message = "Parameter field required flag changed"
			}
			b.add(change, severity, endpoint, location, message, breaking, oldField.Required, newField.Required)
		}
		b.compareEnumValueLists(change, endpoint, location, oldField.Enum, newField.Enum, "Enum value removed")
	}
	for _, path := range sortedStringKeys(oldFields) {
		if _, ok := newFields[path]; !ok {
			breaking := response
			severity := SeverityWarning
			message := "Request body field removed"
			if change == ChangeParameterChanged {
				message = "Parameter field removed"
			}
			if response {
				severity = SeverityBreaking
				message = "Response field removed"
			}
			b.add(change, severity, endpoint, prefix+"."+path, message, breaking, oldFields[path].diffValue(), nil)
		}
	}
}

func (b *semanticDiffBuilder) compareSchemaRootType(change int, endpoint Endpoint, prefix string, oldSchema, newSchema any, response bool) {
	oldType, newType := schemaType(oldSchema), schemaType(newSchema)
	if oldSchema == nil || newSchema == nil || oldType == newType {
		return
	}
	breaking := typeChangeBreaking(oldType, newType, response)
	b.add(change, compatibilitySeverity(breaking), endpoint, prefix+".type", schemaTypeChangeMessage(response), breaking, oldType, newType)
}

func (b *semanticDiffBuilder) compareEnumValues(change int, endpoint Endpoint, location string, oldSchema, newSchema any, message string) {
	if oldSchema == nil || newSchema == nil {
		return
	}
	b.compareEnumValueLists(change, endpoint, location, enumIdentities(oldSchema), enumIdentities(newSchema), message)
}

func (b *semanticDiffBuilder) compareEnumValueLists(change int, endpoint Endpoint, location string, oldValues, newValues []string, message string) {
	// nil 表示没有枚举限制；空但非 nil 的集合表示不允许任何值。
	if oldValues == nil && newValues == nil {
		return
	}
	if oldValues == nil || newValues == nil {
		breaking := newValues != nil
		message := "Enum constraint added"
		if newValues == nil {
			message = "Enum constraint removed"
		}
		if change == ChangeResponseChanged {
			breaking = !breaking
		}
		severity := SeverityInfo
		if breaking {
			severity = SeverityBreaking
		}
		b.add(change, severity, endpoint, location, message, breaking, enumDisplay(oldValues), enumDisplay(newValues))
		return
	}
	newSet := map[string]bool{}
	for _, value := range newValues {
		newSet[value] = true
	}
	for _, value := range oldValues {
		if !newSet[value] {
			b.add(change, SeverityBreaking, endpoint, location, message, true, enumDisplayValue(value), nil)
		}
	}
	oldSet := map[string]bool{}
	for _, value := range oldValues {
		oldSet[value] = true
	}
	for _, value := range newValues {
		if !oldSet[value] {
			breaking := change == ChangeResponseChanged
			b.add(change, compatibilitySeverity(breaking), endpoint, location, "Enum value added", breaking, nil, enumDisplayValue(value))
		}
	}
}

func (b *semanticDiffBuilder) add(change, severity int, endpoint Endpoint, location, message string, breaking bool, oldValue, newValue any) {
	b.items = append(b.items, DiffItem{ID: id.GenerateID(), ChangeType: change, Severity: severity, Method: endpoint.Method, Path: endpoint.Path, OperationID: endpoint.OperationID, Location: location, OldValue: oldValue, NewValue: newValue, Message: message, FrontendImpact: message, IsBreaking: breaking, MustHandle: breaking})
}

func (b *semanticDiffBuilder) sortedItems() []DiffItem {
	items := append([]DiffItem(nil), b.items...)
	sort.SliceStable(items, func(i, j int) bool { return diffItemSortKey(items[i]) < diffItemSortKey(items[j]) })
	for i := range items {
		items[i].SortOrder = i + 1
	}
	return items
}

func diffItemSortKey(item DiffItem) string {
	return item.Path + "\x00" + item.Method + "\x00" + item.Location + "\x00" + fmt.Sprintf("%03d", item.ChangeType) + "\x00" + item.Message
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func endpointIdentity(endpoint Endpoint) map[string]any {
	return map[string]any{"method": endpoint.Method, "path": endpoint.Path, "operation_id": endpoint.OperationID}
}

func endpointMetadata(endpoint Endpoint) map[string]any {
	return map[string]any{"operation_id": endpoint.OperationID, "summary": endpoint.Summary, "tags": optionalCollection(endpoint.Tags)}
}

func parametersByIdentity(value any) map[string]map[string]any {
	out := map[string]map[string]any{}
	items, ok := value.([]any)
	if !ok {
		return out
	}
	for _, item := range items {
		param, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := param["name"].(string)
		if name == "" {
			continue
		}
		out[parameterIdentity(parameterLocation(param), name)] = param
	}
	return out
}

func parameterIdentity(location, name string) string { return location + "\x00" + name }

func unmatchedParametersByName(parameters map[string]map[string]any, matched map[string]bool) map[string][]string {
	out := map[string][]string{}
	for key, parameter := range parameters {
		if matched[key] {
			continue
		}
		name, _ := parameter["name"].(string)
		out[name] = append(out[name], key)
	}
	for name := range out {
		sort.Strings(out[name])
	}
	return out
}

func sortedStringUnionKeys[V any, W any](left map[string]V, right map[string]W) []string {
	keys := make(map[string]bool, len(left)+len(right))
	for key := range left {
		keys[key] = true
	}
	for key := range right {
		keys[key] = true
	}
	return sortedStringKeys(keys)
}

func parameterLocation(param map[string]any) string {
	location, _ := param["in"].(string)
	if location == "" {
		return "unknown"
	}
	return location
}

func parameterPath(location, name string) string { return "parameters." + location + "." + name }

func compactParameterValue(param map[string]any) map[string]any {
	return map[string]any{"name": param["name"], "in": param["in"], "required": boolValue(param["required"]), "type": schemaType(param["schema"]), "enum": enumValues(param["schema"])}
}

func compactSchemaValue(schema any) map[string]any {
	return map[string]any{"type": schemaType(schema), "fields": schemaFields(schema)}
}

func unconstrainedSchema(schema any) any {
	if schema == nil {
		return map[string]any{}
	}
	return schema
}

func mediaSchemas(requestBody any) map[string]any {
	out := map[string]any{}
	body, ok := requestBody.(map[string]any)
	if !ok {
		return out
	}
	content, ok := body["content"].(map[string]any)
	if !ok {
		return out
	}
	for media, value := range content {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out[media] = unconstrainedSchema(entry["schema"])
	}
	return out
}

func requestBodyRequired(requestBody any) bool {
	body, ok := requestBody.(map[string]any)
	return ok && boolValue(body["required"])
}

func responseStatuses(responses any) map[string]any {
	responseMap, _ := responses.(map[string]any)
	out := map[string]any{}
	for key, value := range responseMap {
		if !strings.HasPrefix(key, "x-") {
			out[key] = value
		}
	}
	return out
}

func responseSchemas(responses any) map[string]any {
	out := map[string]any{}
	responseMap := responseStatuses(responses)
	for status, value := range responseMap {
		response, ok := value.(map[string]any)
		if !ok {
			continue
		}
		content, ok := response["content"].(map[string]any)
		if !ok {
			if schema := response["schema"]; schema != nil {
				out[status] = schema
			}
			continue
		}
		for media, contentValue := range content {
			entry, ok := contentValue.(map[string]any)
			if !ok {
				continue
			}
			out[status+"."+media] = unconstrainedSchema(entry["schema"])
		}
	}
	return out
}

type schemaField struct {
	arrayItem bool
	defined   bool
	parent    string
	readOnly  bool
	writeOnly bool
	Type      string   `json:"type,omitempty"`
	Required  bool     `json:"required"`
	Enum      []string `json:"enum,omitempty"`
}

func (f schemaField) diffValue() map[string]any {
	return map[string]any{"type": f.Type, "required": f.Required, "enum": enumDisplay(f.Enum)}
}

func schemaFields(schema any) map[string]schemaField {
	out := map[string]schemaField{}
	collectSchemaFields(out, "", schema)
	return out
}

func schemaFieldsForDirection(schema any, response bool) map[string]schemaField {
	fields := schemaFields(schema)
	// 先合并全部 allOf 字段，再按父路径优先的顺序传播访问注解；注解与定义
	// 可能分处不同分支。仅调整派生的 required，不改动存储的原始 schema。
	for _, path := range sortedStringKeys(fields) {
		field := fields[path]
		parent := fields[field.parent]
		field.readOnly = field.readOnly || parent.readOnly
		field.writeOnly = field.writeOnly || parent.writeOnly
		if (!response && field.readOnly) || (response && field.writeOnly) {
			// required 可以单独提及未定义的属性；方向不适用时不应凭空
			// 生成字段，否则移除 required 会被误判为响应字段删除。
			if !field.defined && !field.arrayItem {
				delete(fields, path)
				continue
			}
			field.Required = false
		}
		fields[path] = field
	}
	return fields
}

func schemaAccess(schema any) (readOnly, writeOnly bool) {
	if schemaMap, ok := schema.(map[string]any); ok {
		for _, fragment := range schemaFragments(schemaMap) {
			readOnly = readOnly || boolValue(fragment["readOnly"])
			writeOnly = writeOnly || boolValue(fragment["writeOnly"])
		}
	}
	return
}

func collectSchemaFields(out map[string]schemaField, prefix string, schema any) {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return
	}
	fragments := schemaFragments(schemaMap)
	required := map[string]bool{}
	for _, fragment := range fragments {
		for name := range stringSet(fragment["required"]) {
			required[name] = true
		}
	}
	for name := range required {
		mergeSchemaField(out, schemaPath(prefix, "properties."+schemaPropertyName(name)), schemaField{parent: prefix, Required: true})
	}
	for _, fragment := range fragments {
		properties, _ := fragment["properties"].(map[string]any)
		for _, name := range sortedStringKeys(properties) {
			property := properties[name]
			path := schemaPath(prefix, "properties."+schemaPropertyName(name))
			readOnly, writeOnly := schemaAccess(property)
			mergeSchemaField(out, path, schemaField{parent: prefix, defined: true, readOnly: readOnly, writeOnly: writeOnly, Type: schemaType(property), Required: required[name], Enum: enumIdentities(property)})
			collectSchemaFields(out, path, property)
		}
		if items := fragment["items"]; items != nil {
			path := schemaPath(prefix, "items")
			// items 本身也有类型和枚举，不能只收集其下的对象属性。
			mergeSchemaField(out, path, schemaField{parent: prefix, Type: schemaType(items), Enum: enumIdentities(items), arrayItem: true})
			collectSchemaFields(out, path, items)
		}
	}
}

func schemaFragments(schema map[string]any) []map[string]any {
	out := []map[string]any{schema}
	allOf, _ := schema["allOf"].([]any)
	for _, value := range allOf {
		fragment, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, schemaFragments(fragment)...)
	}
	return out
}

// 转义字段名里的路径分隔符，避免字面属性 a.properties.b 与嵌套 a/b 合并。
func schemaPropertyName(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), `.`, `\.`)
}

func schemaPath(prefix, suffix string) string {
	if prefix == "" {
		return suffix
	}
	return prefix + "." + suffix
}

func mergeSchemaField(out map[string]schemaField, path string, next schemaField) {
	current, ok := out[path]
	if !ok {
		out[path] = next
		return
	}
	current.Type = intersectSchemaTypes(current.Type, next.Type)
	current.Required = current.Required || next.Required
	current.arrayItem = current.arrayItem || next.arrayItem
	current.defined = current.defined || next.defined
	current.readOnly = current.readOnly || next.readOnly
	current.writeOnly = current.writeOnly || next.writeOnly
	if current.Enum == nil {
		current.Enum = next.Enum
	} else if next.Enum != nil {
		allowed := map[string]bool{}
		for _, value := range next.Enum {
			allowed[value] = true
		}
		intersection := make([]string, 0)
		for _, value := range current.Enum {
			if allowed[value] {
				intersection = append(intersection, value)
			}
		}
		current.Enum = intersection
	}
	out[path] = current
}

func stringSet(value any) map[string]bool {
	out := map[string]bool{}
	items, ok := value.([]any)
	if !ok {
		return out
	}
	for _, item := range items {
		if s, ok := item.(string); ok {
			out[s] = true
		}
	}
	return out
}

func schemaType(schema any) string {
	m, ok := asMap(schema)
	if !ok {
		return ""
	}
	var allowed map[string]bool
	for _, fragment := range schemaFragments(m) {
		var types []string
		switch value := fragment["type"].(type) {
		case string:
			types = []string{value}
		case []any:
			types = stringSlice(value)
		}
		if len(types) == 0 {
			continue
		}
		next := map[string]bool{}
		for _, value := range types {
			next[value] = true
		}
		if boolValue(fragment["nullable"]) {
			next["null"] = true
		}
		if allowed == nil {
			allowed = next
			continue
		}
		intersection := intersectSchemaTypes(strings.Join(sortedStringKeys(allowed), "|"), strings.Join(sortedStringKeys(next), "|"))
		allowed = map[string]bool{}
		for _, value := range strings.Split(intersection, "|") {
			allowed[value] = true
		}
	}
	if allowed == nil {
		return ""
	}
	if len(allowed) == 0 {
		return "never"
	}
	return strings.Join(sortedStringKeys(allowed), "|")
}

func typeChangeBreaking(oldType, newType string, response bool) bool {
	if response {
		oldType, newType = newType, oldType
	}
	if newType == "" || oldType == "never" {
		return false
	}
	if oldType == "" {
		return true
	}
	allowed := map[string]bool{}
	for _, value := range strings.Split(newType, "|") {
		allowed[value] = true
	}
	for _, value := range strings.Split(oldType, "|") {
		if !allowed[value] && !(value == "integer" && allowed["number"]) {
			return true
		}
	}
	return false
}

func compatibilitySeverity(breaking bool) int {
	if breaking {
		return SeverityBreaking
	}
	return SeverityInfo
}

func enumDisplayValue(value string) any {
	var decoded any
	if jsonvalue.Decode([]byte(value), &decoded) == nil {
		return decoded
	}
	return value
}
func enumDisplay(values []string) []any {
	if values == nil {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, enumDisplayValue(value))
	}
	return out
}
func enumValues(schema any) []string {
	identities := enumIdentities(schema)
	if identities == nil {
		return nil
	}
	out := make([]string, 0, len(identities))
	for _, value := range identities {
		out = append(out, fmt.Sprint(enumDisplayValue(value)))
	}
	return out
}

func enumIdentities(schema any) []string {
	m, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	var allowed map[string]bool
	for _, fragment := range schemaFragments(m) {
		items, constrained := fragment["enum"].([]any)
		if constant, exists := fragment["const"]; exists {
			if !constrained {
				items, constrained = []any{constant}, true
			} else {
				intersection := []any{}
				for _, item := range items {
					if valuesEqual(item, constant) {
						intersection = append(intersection, item)
					}
				}
				items = intersection
			}
		}
		if !constrained {
			continue
		}
		next := map[string]bool{}
		for _, item := range items {
			encoded, _ := json.Marshal(normalizeValue(item))
			next[string(encoded)] = true
		}
		if allowed == nil {
			allowed = next
			continue
		}
		for value := range allowed {
			if !next[value] {
				delete(allowed, value)
			}
		}
	}
	if allowed == nil {
		return nil
	}
	out := make([]string, 0, len(allowed))
	for value := range allowed {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func valuesEqual(left, right any) bool {
	leftBytes, leftErr := json.Marshal(normalizeValue(left))
	rightBytes, rightErr := json.Marshal(normalizeValue(right))
	return leftErr == nil && rightErr == nil && string(leftBytes) == string(rightBytes)
}

// 仅端点级可选集合允许空/缺省等价，Schema 内的 [] 与 null 必须保持区别。
func optionalCollection(value any) any {
	switch typed := value.(type) {
	case []string:
		if len(typed) == 0 {
			return nil
		}
	case []any:
		if len(typed) == 0 {
			return nil
		}
	}
	return value
}

func fieldTypeChangeMessage(response bool) string {
	if response {
		return "Response field type changed"
	}
	return "Request body field type changed"
}

func schemaTypeChangeMessage(response bool) string {
	if response {
		return "Response schema type changed"
	}
	return "Request body schema type changed"
}

func fieldRequiredChangeMessage(response bool) string {
	if response {
		return "Response field required flag changed"
	}
	return "Request body field required flag changed"
}

func newFieldAffectsExistingInput(path string, oldFields, newFields map[string]schemaField) bool {
	// 只检查祖先路径，避免大量新增字段时逐个扫描整张字段表。
	for index := strings.LastIndexByte(path, '.'); index >= 0; index = strings.LastIndexByte(path[:index], '.') {
		parent := path[:index]
		field, exists := newFields[parent]
		if !exists {
			continue
		}
		if _, existed := oldFields[parent]; !existed && !field.Required && !field.arrayItem {
			return false
		}
	}
	return true
}

func intersectSchemaTypes(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	result := map[string]bool{}
	for _, x := range strings.Split(a, "|") {
		for _, y := range strings.Split(b, "|") {
			if x == y {
				result[x] = true
			} else if (x == "integer" && y == "number") || (x == "number" && y == "integer") {
				result["integer"] = true
			}
		}
	}
	if len(result) == 0 {
		return "never"
	}
	return strings.Join(sortedStringKeys(result), "|")
}
