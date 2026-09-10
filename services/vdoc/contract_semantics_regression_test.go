package vdoc

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestRegressionRefSiblingRequiredMustReachDetailAndDiff(t *testing.T) {
	base := `{"openapi":"3.1.0","info":{"title":"Sibling constraints","version":"1"},"paths":{"/widgets":{"post":{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Widget"}}}},"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{"Widget":{"type":"object","properties":{"id":{"type":"string"}}}}}}`
	changed := strings.Replace(base, `"$ref":"#/components/schemas/Widget"`, `"$ref":"#/components/schemas/Widget","required":["id"]`, 1)
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", base, "review-base")
	draft, err := store.CreateDocumentDraft("admin", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "preview", SchemaContent: changed})
	if err != nil || draft.DiffPreview == nil || draft.DiffPreview.Summary.BreakingChanges != 1 {
		t.Fatalf("required sibling missing from draft preview: draft=%+v err=%v", draft, err)
	}
	to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", changed, "review-change")
	parsed, err := ParseOpenAPI(changed)
	if err != nil {
		t.Fatal(err)
	}
	body := parsed.Endpoints[0].RequestBody.(map[string]any)
	schema := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if !schemaFields(schema)["properties.id"].Required {
		t.Fatal("required sibling is missing from the parsed endpoint")
	}
	endpoints, err := store.ListEndpoints("reader", projectID, documentID, to.ID, "")
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints=%v err=%v", endpoints, err)
	}
	detail, err := store.Endpoint("reader", projectID, documentID, to.ID, endpoints[0].ID)
	if err != nil || !valuesEqual(detail.RequestBody, body) {
		t.Fatalf("published detail lost schema facts: detail=%+v err=%v", detail, err)
	}
	diff, err := store.CompareDocumentVersions("reader", projectID, documentID, from.ID, to.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("resolved schema=%v; diff items=%d; breaking changes=%d", schema, len(diff.Items), diff.Summary.BreakingChanges)
	if len(diff.Items) == 0 || diff.Summary.BreakingChanges == 0 {
		t.Error("required request field added beside an OpenAPI 3.1 $ref was omitted from the semantic diff")
	}
}

func TestRegressionSecuritySchemeDefinitionChangeMustReachDiff(t *testing.T) {
	base := `{"openapi":"3.1.0","info":{"title":"Security definition","version":"1"},"security":[{"api_key":[]}],"paths":{"/widgets":{"get":{"responses":{"200":{"description":"ok"}}}}},"components":{"securitySchemes":{"api_key":{"type":"apiKey","in":"header","name":"X-API-Key"}}}}`
	changed := strings.Replace(base, `"X-API-Key"`, `"X-New-API-Key"`, 1)
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", base, "review-base")
	to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", changed, "review-change")
	diff, err := store.CompareDocumentVersions("reader", projectID, documentID, from.ID, to.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("header X-API-Key -> X-New-API-Key; diff items=%d; modified endpoints=%d", len(diff.Items), diff.Summary.ModifiedEndpoints)
	if len(diff.Items) != 1 || diff.Items[0].ChangeType != ChangeSecurityChanged || diff.Items[0].Location != "securitySchemes" || diff.Summary.ModifiedEndpoints != 1 {
		t.Fatalf("changing an active authentication header produced incorrect changes: %+v", diff)
	}
	if diff.Items[0].OldValue.(map[string]any)["api_key"].(map[string]any)["name"] != "X-API-Key" || diff.Items[0].NewValue.(map[string]any)["api_key"].(map[string]any)["name"] != "X-New-API-Key" {
		t.Fatal("security diff omitted the old/new header definitions")
	}
}

func TestRegressionOperationParameterOverridesPathParameter(t *testing.T) {
	schema := `{"openapi":"3.1.0","info":{"title":"Parameter override","version":"1"},"paths":{"/widgets":{"parameters":[{"name":"limit","in":"query","required":true,"schema":{"type":"integer"}}],"get":{"parameters":[{"name":"limit","in":"query","required":false,"schema":{"type":"integer"}}],"responses":{"200":{"description":"ok"}}}}}}`
	parsed, err := ParseOpenAPI(schema)
	if err != nil {
		t.Fatal(err)
	}
	parameters := parsed.Endpoints[0].Parameters.([]any)
	t.Logf("resolved parameters=%v", parameters)
	if len(parameters) != 1 {
		t.Errorf("got %d parameters for one name+in identity; want only the operation override", len(parameters))
	}
}

func TestRegressionChatKeepsLiteralTemplateMarkersInDocument(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	content := "# Template documentation\n\nThe placeholders are {{message}} and {{history}}.\n"
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", content, "review-fixture")
	var sentUserContent string
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		var body struct {
			Messages []aiMessagePayload `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		sentUserContent = body.Messages[len(body.Messages)-1].Content
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"Fixture answer"}}]}`))}, nil
	})})
	_, err := store.UpsertSystemAIProvider("super", AIProviderInput{Name: "review fixture", BaseURL: testAIProviderBaseURL, Model: "fixture", APIMode: domainai.ProviderModeChatCompletions, APIKey: "sk-test-review-only", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: domainai.SummaryOwnerVersion, ContextID: version.ID})
	if err != nil {
		t.Fatal(err)
	}
	question := "Explain these placeholders: {{context}}, {{message}}, and {{history}}"
	_, err = store.SendAIChatMessage("reader", projectID, session.ID, question)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(sentUserContent, "\n") {
		if strings.Contains(line, "placeholders") {
			t.Logf("sent prompt line: %s", line)
		}
	}
	if !strings.Contains(sentUserContent, "The placeholders are {{message}} and {{history}}.") {
		t.Error("document text was substituted as if it were the prompt template")
	}
	if !strings.Contains(sentUserContent, question) {
		t.Error("question text was substituted as if it were the prompt template")
	}
}
