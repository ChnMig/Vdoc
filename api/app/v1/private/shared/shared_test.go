package shared

import (
	"bytes"
	"encoding/json"
	"testing"

	app "vdoc/appstore"
)

func TestDocumentDTOsKeepContentBehindExplicitContentEndpoint(t *testing.T) {
	draft := &app.ContractDraft{
		ID:                   "draft-1",
		DocumentID:           "document-1",
		SchemaFormat:         app.DocumentFormatMarkdown,
		RawSchema:            "# raw",
		NormalizedSchema:     "# stable",
		RawSchemaObjectKey:   "draft/raw",
		NormalizedObjectKey:  "draft/stable",
		RawSchemaHash:        "raw-hash",
		NormalizedSchemaHash: "stable-hash",
	}
	version := &app.ContractVersion{
		ID:                   "version-1",
		DocumentID:           "document-1",
		SchemaFormat:         app.DocumentFormatOpenAPI31,
		RawSchema:            `{"openapi":"3.1.0"}`,
		NormalizedSchema:     `{"openapi":"3.1.0"}`,
		RawSchemaObjectKey:   "version/raw",
		NormalizedObjectKey:  "version/normalized",
		RawSchemaHash:        "raw-hash",
		NormalizedSchemaHash: "normalized-hash",
	}

	assertSummaryDTO(t, "draft", Draft(draft), []string{`"raw_content_hash":"raw-hash"`, `"stable_content_hash":"stable-hash"`})
	assertSummaryDTO(t, "version", Version(version), []string{`"raw_content_hash":"raw-hash"`, `"normalized_content_hash":"normalized-hash"`})

	contentBody := mustMarshalDTO(t, Content(&app.SchemaDocument{OwnerType: "version", OwnerID: "version-1", Kind: "raw", Content: version.RawSchema, ObjectKey: "version/raw", Hash: "raw-hash"}))
	if !bytes.Contains(contentBody, []byte(`"content":"{\"openapi\":\"3.1.0\"}"`)) {
		t.Fatalf("content DTO missing content: %s", contentBody)
	}
	if bytes.Contains(contentBody, []byte("object_key")) {
		t.Fatalf("content DTO exposed storage key: %s", contentBody)
	}
}

func assertSummaryDTO(t *testing.T, label string, value any, required []string) {
	t.Helper()
	body := mustMarshalDTO(t, value)
	for _, forbidden := range []string{`"raw_content"`, `"normalized_content"`, `"stable_content"`, `"raw_schema"`, `"normalized_schema"`, "object_key"} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatalf("%s DTO exposed %s: %s", label, forbidden, body)
		}
	}
	for _, field := range required {
		if !bytes.Contains(body, []byte(field)) {
			t.Fatalf("%s DTO missing %s: %s", label, field, body)
		}
	}
}

func mustMarshalDTO(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return body
}
