package vdoc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDocumentActivityReadersSeeOnlyOrderedDocumentWorkflow(t *testing.T) {
	store, _, project, document, branch := newContractPipelineStore(t)
	version := publishContractDraft(t, store, "admin", project, document, branch, "1.0", testOpenAPI("activity"))
	at := time.Now().Add(time.Hour)
	for _, log := range []AuditLog{
		{ID: "same-a", ProjectID: project, ServiceID: document, Action: "contract_draft.update", ResourceID: version.DraftID, CreatedAt: at, Metadata: map[string]string{"branch_id": branch, "version_name": "next", "secret": "private-metadata"}, IPAddress: "203.0.113.7"},
		{ID: "same-b", ProjectID: project, ServiceID: document, Action: "contract_draft.submit", CreatedAt: at},
		{ID: "foreign", ProjectID: project, ServiceID: "other-document", Action: "contract_draft.submit", CreatedAt: at},
		{ID: "token-event", ProjectID: project, ServiceID: document, Action: "mcp.tool_call", CreatedAt: at},
	} {
		if err := store.RecordAudit(log); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.QueryDocumentActivity("reader", project, document, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != "same-b" || page.Items[1].ID != "same-a" || page.NextCursor == "" {
		t.Fatalf("page = %+v", page)
	}
	encoded, _ := json.Marshal(page)
	for _, value := range []string{"private-metadata", "203.0.113.7", "IPAddress", "Metadata"} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("activity leaked %q", value)
		}
	}
	seen := map[string]bool{"same-a": true, "same-b": true}
	cursor := page.NextCursor
	foundPublish := false
	for cursor != "" {
		page, err = store.QueryDocumentActivity("reader", project, document, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.ID] || item.ID == "foreign" || item.ID == "token-event" {
				t.Fatalf("invalid event %+v", item)
			}
			seen[item.ID] = true
			if item.Action == "document_version.publish" {
				foundPublish = true
			}
		}
		cursor = page.NextCursor
	}
	if !foundPublish {
		t.Fatal("missing publication event")
	}
	if _, err := store.QueryDocumentActivity("outsider", project, document, "", 50); err == nil {
		t.Fatal("outsider accessed activity")
	}
	if _, err := store.QueryDocumentActivity("reader", project, "other-document", "", 50); !Is(err, ErrNotFound) {
		t.Fatalf("wrong-document error = %v", err)
	}
	if _, err := store.QueryDocumentActivity("reader", project, document, "invalid-cursor", 50); !Is(err, ErrInvalidArgument) {
		t.Fatalf("cursor error = %v", err)
	}
}

func TestDraftHistoryUsesRealPublicationTimeAndValidatedSourceVersion(t *testing.T) {
	store, branches, project, document, branch := newContractPipelineStore(t)
	version := publishContractDraft(t, store, "admin", project, document, branch, "1.0", testOpenAPI("source"))
	input := DraftInput{BranchID: branches["test"].ID, VersionName: "next", SchemaContent: testOpenAPI("modified"), SourceVersionID: version.ID, SourceBranchID: branch}
	draft, err := store.CreateDraft("writer", project, document, input)
	if err != nil {
		t.Fatal(err)
	}
	if draft.SourceVersionID != version.ID || draft.SourceBranchID != branch || draft.PublishedAt != nil {
		t.Fatalf("source = %+v", draft)
	}
	history, _, err := store.QueryDrafts("reader", project, document, "", PageQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range history {
		if item.ID == version.DraftID {
			found = true
			if item.PublishedAt == nil || !item.PublishedAt.Equal(version.PublishedAt) {
				t.Fatalf("published at = %v, want %v", item.PublishedAt, version.PublishedAt)
			}
		}
		if item.ID == draft.ID && item.PublishedAt != nil {
			t.Fatal("unpublished draft has publication time")
		}
	}
	if !found {
		t.Fatal("published draft missing from history")
	}
	snapshot, _, err := store.ReadDraftContent("reader", project, document, version.DraftID, "raw")
	if err != nil || snapshot.PublishedAt == nil || !snapshot.PublishedAt.Equal(version.PublishedAt) {
		t.Fatalf("historical content = %+v %v", snapshot, err)
	}
	for _, invalid := range []DraftInput{
		{BranchID: branch, VersionName: "bad1", SchemaContent: testOpenAPI("x"), SourceVersionID: "foreign-version"},
		{BranchID: branch, VersionName: "bad2", SchemaContent: testOpenAPI("x"), SourceVersionID: version.ID, SourceBranchID: branches["test"].ID},
		{BranchID: branch, VersionName: "bad3", SchemaContent: testOpenAPI("x"), SourceBranchID: branch},
	} {
		if _, err := store.CreateDraft("writer", project, document, invalid); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
}
