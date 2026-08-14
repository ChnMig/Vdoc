package vdoc

import "testing"

func TestCompareVersionsProducesSemanticDiffItems(t *testing.T) {
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)
	from := publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.0.0", semanticDiffBaselineOpenAPI())
	to := publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.1.0", semanticDiffChangedOpenAPI())

	diff, err := store.CompareVersions("reader", projectID, serviceID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("CompareVersions() error = %v", err)
	}
	if diff.Summary.AddedEndpoints != 1 || diff.Summary.RemovedEndpoints != 1 || diff.Summary.ModifiedEndpoints != 1 {
		t.Fatalf("summary endpoint counts = %+v", diff.Summary)
	}

	assertDiffItem(t, diff, ChangeEndpointAdded, "endpoint", SeverityInfo, false, "Endpoint added")
	assertDiffItem(t, diff, ChangeEndpointRemoved, "endpoint", SeverityBreaking, true, "Endpoint removed")
	assertDiffItem(t, diff, ChangeParameterAdded, "parameters.query.limit", SeverityBreaking, true, "Parameter added")
	assertDiffItem(t, diff, ChangeParameterChanged, "parameters.query.filter", SeverityBreaking, true, "Parameter type changed")
	assertDiffItem(t, diff, ChangeParameterChanged, "parameters.query.trace", SeverityBreaking, true, "Parameter location changed")
	assertDiffItem(t, diff, ChangeParameterRemoved, "parameters.query.obsolete", SeverityWarning, false, "Parameter removed")
	assertDiffItem(t, diff, ChangeRequestBodyChanged, "requestBody.application/json.properties.sku", SeverityBreaking, true, "Request body field added")
	assertDiffItem(t, diff, ChangeResponseChanged, "responses.404", SeverityInfo, false, "Response status added")
	assertDiffItem(t, diff, ChangeResponseChanged, "responses.200.application/json.properties.name", SeverityBreaking, true, "Response field removed")
	assertDiffItem(t, diff, ChangeResponseChanged, "responses.200.application/json.properties.status", SeverityBreaking, true, "Response field type changed")
	assertDiffItem(t, diff, ChangeResponseChanged, "responses.200.application/json.properties.state", SeverityBreaking, true, "Enum value removed")
	assertDiffItem(t, diff, ChangeResponseChanged, "responses.200.application/json.properties.email", SeverityInfo, false, "Response field added")
	assertDiffItem(t, diff, ChangeSecurityChanged, "security", SeverityWarning, false, "Security requirements changed")
	assertDiffItem(t, diff, ChangeDeprecatedChanged, "deprecated", SeverityInfo, false, "Deprecated status changed")

	breaking := 0
	for index, item := range diff.Items {
		if item.SortOrder != index+1 {
			t.Fatalf("item %d sort_order = %d", index, item.SortOrder)
		}
		if item.IsBreaking != item.MustHandle {
			t.Fatalf("item %s must_handle = %v, want is_breaking %v", item.Location, item.MustHandle, item.IsBreaking)
		}
		if item.IsBreaking {
			breaking++
		}
	}
	if diff.Summary.BreakingChanges != breaking || breaking == 0 {
		t.Fatalf("breaking summary = %d, counted %d", diff.Summary.BreakingChanges, breaking)
	}
}

func TestCompareVersionsOptionalResponseFieldAdditionIsInfo(t *testing.T) {
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)
	from := publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.0.0", semanticDiffOptionalResponseOpenAPI(false))
	to := publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.1.0", semanticDiffOptionalResponseOpenAPI(true))

	diff, err := store.CompareVersions("reader", projectID, serviceID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("CompareVersions() error = %v", err)
	}
	if diff.Summary.BreakingChanges != 0 || diff.Summary.ModifiedEndpoints != 1 {
		t.Fatalf("summary = %+v, want one non-breaking modified endpoint", diff.Summary)
	}
	assertDiffItem(t, diff, ChangeResponseChanged, "responses.200.application/json.properties.email", SeverityInfo, false, "Response field added")
}

func TestCompareVersionsDetectsRootSchemaTypeChanges(t *testing.T) {
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)
	from := publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.0.0", semanticDiffRootTypeOpenAPI("object", "object"))
	to := publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.1.0", semanticDiffRootTypeOpenAPI("array", "array"))

	diff, err := store.CompareVersions("reader", projectID, serviceID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("CompareVersions() error = %v", err)
	}
	responseType := assertDiffItem(t, diff, ChangeResponseChanged, "responses.200.application/json.type", SeverityBreaking, true, "Response schema type changed")
	if responseType.OldValue != "object" || responseType.NewValue != "array" {
		t.Fatalf("response root type values = old %#v new %#v", responseType.OldValue, responseType.NewValue)
	}
	requestType := assertDiffItem(t, diff, ChangeRequestBodyChanged, "requestBody.application/json.type", SeverityBreaking, true, "Request body schema type changed")
	if requestType.OldValue != "object" || requestType.NewValue != "array" {
		t.Fatalf("request root type values = old %#v new %#v", requestType.OldValue, requestType.NewValue)
	}
	assertDiffItem(t, diff, ChangeResponseChanged, "responses.200.application/json.properties.name", SeverityBreaking, true, "Response field removed")
}

func TestSemanticDiffMarksPRDRequestAnd2xxRemovalRulesBreaking(t *testing.T) {
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)
	fromSchema := `{"openapi":"3.1.0","info":{"title":"Rules","version":"1"},"paths":{"/widgets":{"post":{"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"count":{"type":"string"}}}},"application/xml":{"schema":{"type":"object"}}}},"responses":{"200":{"description":"ok"},"404":{"description":"missing"}}}}}}`
	toSchema := `{"openapi":"3.1.0","info":{"title":"Rules","version":"2"},"paths":{"/widgets":{"post":{"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"count":{"type":"integer"}}}}}},"responses":{"500":{"description":"error"}}}}}}`
	from := publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.0.0", fromSchema)
	to := publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.1.0", toSchema)

	diff, err := store.CompareVersions("reader", projectID, serviceID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("CompareVersions() error = %v", err)
	}
	assertDiffItem(t, diff, ChangeRequestBodyChanged, "requestBody.application/json.properties.count", SeverityBreaking, true, "Request body field type changed")
	assertDiffItem(t, diff, ChangeRequestBodyChanged, "requestBody.application/xml", SeverityBreaking, true, "Request body media type removed")
	assertDiffItem(t, diff, ChangeResponseChanged, "responses.200", SeverityBreaking, true, "Response status removed")
	assertDiffItem(t, diff, ChangeResponseChanged, "responses.404", SeverityWarning, false, "Response status removed")
}

func assertDiffItem(t *testing.T, diff *Diff, change int, location string, severity int, breaking bool, message string) DiffItem {
	t.Helper()
	for _, item := range diff.Items {
		if item.ChangeType == change && item.Location == location && item.Message == message {
			if item.Severity != severity || item.IsBreaking != breaking || item.MustHandle != breaking {
				t.Fatalf("item %s/%s = severity %d breaking %v must_handle %v, want severity %d breaking %v", location, message, item.Severity, item.IsBreaking, item.MustHandle, severity, breaking)
			}
			if item.Method == "" || item.Path == "" {
				t.Fatalf("item %s/%s missing endpoint identity: %+v", location, message, item)
			}
			return item
		}
	}
	t.Fatalf("missing diff item change=%d location=%s message=%s in %+v", change, location, message, diff.Items)
	return DiffItem{}
}

func semanticDiffBaselineOpenAPI() string {
	return `{"openapi":"3.1.0","info":{"title":"Semantic API","version":"1.0.0"},"security":[{"api_key":[]}],"paths":{"/legacy":{"delete":{"operationId":"deleteLegacy","responses":{"204":{"description":"deleted"}}}},"/widgets":{"post":{"operationId":"upsertWidget","parameters":[{"name":"filter","in":"query","schema":{"type":"string"}},{"name":"trace","in":"header","schema":{"type":"string"}},{"name":"obsolete","in":"query","schema":{"type":"string"}}],"requestBody":{"required":true,"content":{"application/json":{"schema":{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","required":["id","name","status","state"],"properties":{"id":{"type":"string"},"name":{"type":"string"},"status":{"type":"string"},"state":{"type":"string","enum":["active","disabled"]}}}}}}}}}},"components":{"securitySchemes":{"api_key":{"type":"apiKey","in":"header","name":"X-API-Key"},"oauth":{"type":"http","scheme":"bearer"}}}}`
}

func semanticDiffChangedOpenAPI() string {
	return `{"openapi":"3.1.0","info":{"title":"Semantic API","version":"1.1.0"},"security":[{"oauth":[]}],"paths":{"/reports":{"get":{"operationId":"listReports","responses":{"200":{"description":"ok"}}}},"/widgets":{"post":{"operationId":"upsertWidget","deprecated":true,"parameters":[{"name":"filter","in":"query","schema":{"type":"integer"}},{"name":"trace","in":"query","schema":{"type":"string"}},{"name":"limit","in":"query","required":true,"schema":{"type":"integer"}}],"requestBody":{"required":true,"content":{"application/json":{"schema":{"type":"object","required":["name","sku"],"properties":{"name":{"type":"string"},"sku":{"type":"string"}}}}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","required":["id","status","state"],"properties":{"id":{"type":"string"},"status":{"type":"integer"},"state":{"type":"string","enum":["active"]},"email":{"type":"string"}}}}}},"404":{"description":"missing"}}}}},"components":{"securitySchemes":{"api_key":{"type":"apiKey","in":"header","name":"X-API-Key"},"oauth":{"type":"http","scheme":"bearer"}}}}`
}

func semanticDiffOptionalResponseOpenAPI(includeEmail bool) string {
	email := ""
	if includeEmail {
		email = `,"email":{"type":"string"}`
	}
	return `{"openapi":"3.1.0","info":{"title":"Optional API","version":"1.0.0"},"paths":{"/widgets":{"get":{"operationId":"getWidget","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","required":["id"],"properties":{"id":{"type":"string"}` + email + `}}}}}}}}}}`
}

func semanticDiffRootTypeOpenAPI(requestType, responseType string) string {
	requestSchema := `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`
	if requestType == "array" {
		requestSchema = `{"type":"array","items":{"type":"string"}}`
	}
	responseSchema := `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`
	if responseType == "array" {
		responseSchema = `{"type":"array","items":{"type":"string"}}`
	}
	return `{"openapi":"3.1.0","info":{"title":"Root Type API","version":"1.0.0"},"paths":{"/widgets":{"post":{"operationId":"upsertWidget","requestBody":{"required":true,"content":{"application/json":{"schema":` + requestSchema + `}}},"responses":{"200":{"description":"ok","content":{"application/json":{"schema":` + responseSchema + `}}}}}}}}`
}
