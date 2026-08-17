package vdoc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vdoc/utils/id"
)

type semanticDiffBuilder struct {
	items []DiffItem
}

func (b *semanticDiffBuilder) compareEndpoint(from, to Endpoint) {
	if !valuesEqual(endpointMetadata(from), endpointMetadata(to)) {
		b.add(ChangeEndpointModified, SeverityWarning, to, "endpoint", "Endpoint metadata changed", false, endpointMetadata(from), endpointMetadata(to))
	}
	b.compareParameters(from, to)
	b.compareRequestBody(from, to)
	b.compareResponses(from, to)
	if !valuesEqual(from.Security, to.Security) {
		b.add(ChangeSecurityChanged, SeverityWarning, to, "security", "Security requirements changed", false, from.Security, to.Security)
	}
	if from.Deprecated != to.Deprecated {
		b.add(ChangeDeprecatedChanged, SeverityInfo, to, "deprecated", "Deprecated status changed", false, from.Deprecated, to.Deprecated)
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
	oldType, newType := schemaType(oldParam["schema"]), schemaType(newParam["schema"])
	if oldType != newType {
		b.add(ChangeParameterChanged, SeverityBreaking, endpoint, parameterPath(location, name), "Parameter type changed", true, oldType, newType)
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
	b.compareEnumValues(ChangeParameterChanged, endpoint, parameterPath(location, name), oldParam["schema"], newParam["schema"], "Parameter enum value removed")
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
			b.compareSchemaFields(ChangeRequestBodyChanged, to, "requestBody."+media, nil, newSchema, false)
			continue
		}
		b.compareSchemaFields(ChangeRequestBodyChanged, to, "requestBody."+media, oldSchema, newSchema, false)
	}
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
	oldFields := schemaFields(oldSchema)
	newFields := schemaFields(newSchema)
	for _, path := range sortedStringKeys(newFields) {
		newField := newFields[path]
		oldField, ok := oldFields[path]
		location := prefix + "." + path
		if !ok {
			breaking := !response && newField.Required
			severity := SeverityInfo
			message := "Response field added"
			if !response {
				message = "Request body field added"
				if breaking {
					severity = SeverityBreaking
				}
			}
			b.add(change, severity, endpoint, location, message, breaking, nil, newField.diffValue())
			continue
		}
		if oldField.Type != newField.Type {
			b.add(change, SeverityBreaking, endpoint, location, fieldTypeChangeMessage(response), true, oldField.Type, newField.Type)
		}
		if oldField.Required != newField.Required {
			breaking := !response && newField.Required
			severity := SeverityWarning
			if breaking {
				severity = SeverityBreaking
			}
			b.add(change, severity, endpoint, location, fieldRequiredChangeMessage(response), breaking, oldField.Required, newField.Required)
		}
		b.compareEnumValueLists(change, endpoint, location, oldField.Enum, newField.Enum, "Enum value removed")
	}
	for _, path := range sortedStringKeys(oldFields) {
		if _, ok := newFields[path]; !ok {
			breaking := response
			severity := SeverityWarning
			message := "Request body field removed"
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
	if oldType == "" || newType == "" || oldType == newType {
		return
	}
	b.add(change, SeverityBreaking, endpoint, prefix+".type", schemaTypeChangeMessage(response), true, oldType, newType)
}

func (b *semanticDiffBuilder) compareEnumValues(change int, endpoint Endpoint, location string, oldSchema, newSchema any, message string) {
	b.compareEnumValueLists(change, endpoint, location, enumValues(oldSchema), enumValues(newSchema), message)
}

func (b *semanticDiffBuilder) compareEnumValueLists(change int, endpoint Endpoint, location string, oldValues, newValues []string, message string) {
	newSet := map[string]bool{}
	for _, value := range newValues {
		newSet[value] = true
	}
	for _, value := range oldValues {
		if !newSet[value] {
			b.add(change, SeverityBreaking, endpoint, location, message, true, value, nil)
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
	return map[string]any{"operation_id": endpoint.OperationID, "summary": endpoint.Summary, "tags": endpoint.Tags}
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
		if schema := entry["schema"]; schema != nil {
			out[media] = schema
		}
	}
	return out
}

func requestBodyRequired(requestBody any) bool {
	body, ok := requestBody.(map[string]any)
	return ok && boolValue(body["required"])
}

func responseStatuses(responses any) map[string]any {
	responseMap, ok := responses.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return responseMap
}

func responseSchemas(responses any) map[string]any {
	out := map[string]any{}
	responseMap, ok := responses.(map[string]any)
	if !ok {
		return out
	}
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
			if schema := entry["schema"]; schema != nil {
				out[status+"."+media] = schema
			}
		}
	}
	return out
}

type schemaField struct {
	Type     string   `json:"type,omitempty"`
	Required bool     `json:"required"`
	Enum     []string `json:"enum,omitempty"`
}

func (f schemaField) diffValue() map[string]any {
	return map[string]any{"type": f.Type, "required": f.Required, "enum": f.Enum}
}

func schemaFields(schema any) map[string]schemaField {
	out := map[string]schemaField{}
	collectSchemaFields(out, "", schema)
	return out
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
	for _, fragment := range fragments {
		properties, _ := fragment["properties"].(map[string]any)
		for _, name := range sortedStringKeys(properties) {
			property := properties[name]
			path := schemaPath(prefix, "properties."+name)
			mergeSchemaField(out, path, schemaField{Type: schemaType(property), Required: required[name], Enum: enumValues(property)})
			collectSchemaFields(out, path, property)
		}
		if items := fragment["items"]; items != nil {
			collectSchemaFields(out, schemaPath(prefix, "items"), items)
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
	if current.Type == "" {
		current.Type = next.Type
	}
	current.Required = current.Required || next.Required
	if len(current.Enum) == 0 && len(next.Enum) > 0 {
		current.Enum = next.Enum
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
	m, ok := schema.(map[string]any)
	if !ok {
		return ""
	}
	if value, ok := m["type"].(string); ok {
		return value
	}
	return ""
}

func enumValues(schema any) []string {
	m, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	items, ok := m["enum"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprint(item))
	}
	sort.Strings(out)
	return out
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func valuesEqual(left, right any) bool {
	leftBytes, _ := json.Marshal(canonicalDiffValue(left))
	rightBytes, _ := json.Marshal(canonicalDiffValue(right))
	return string(leftBytes) == string(rightBytes)
}

func canonicalDiffValue(value any) any {
	switch typed := value.(type) {
	case []string:
		if len(typed) == 0 {
			return nil
		}
		return typed
	case []any:
		if len(typed) == 0 {
			return nil
		}
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = canonicalDiffValue(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = canonicalDiffValue(item)
		}
		return out
	default:
		return typed
	}
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
