package vdoc

import (
	"testing"
	"time"
)

func TestCloneStateLockedDoesNotAliasNestedMutableFields(t *testing.T) {
	store := NewStore()
	submittedAt := time.Unix(100, 0).UTC()
	expiresAt := time.Unix(200, 0).UTC()
	wantSubmittedAt := submittedAt
	wantExpiresAt := expiresAt
	oldValue := map[string]any{
		"nested": []any{map[string]any{"value": "before"}},
	}
	diff := &Diff{ID: "diff", Items: []DiffItem{{ID: "item", OldValue: oldValue}}}
	store.drafts["draft"] = &ContractDraft{ID: "draft", SubmittedAt: &submittedAt, DiffPreview: diff}
	store.diffs[diff.ID] = diff
	store.endpoints["endpoint"] = &Endpoint{
		ID:         "endpoint",
		Tags:       []string{"before"},
		Parameters: map[string]any{"items": []any{map[string]any{"name": "before"}}},
	}
	store.tokens["token"] = &MCPToken{ID: "token", Scopes: []int{ScopeAPIRead}, TokenCiphertext: []byte{1}, ExpiresAt: &expiresAt}

	snapshot := store.cloneStateLocked()

	*store.drafts["draft"].SubmittedAt = submittedAt.Add(time.Hour)
	store.drafts["draft"].DiffPreview.Items[0].OldValue.(map[string]any)["nested"].([]any)[0].(map[string]any)["value"] = "after"
	store.endpoints["endpoint"].Tags[0] = "after"
	store.endpoints["endpoint"].Parameters.(map[string]any)["items"].([]any)[0].(map[string]any)["name"] = "after"
	store.tokens["token"].Scopes[0] = ScopeAPIDraft
	store.tokens["token"].TokenCiphertext[0] = 2
	*store.tokens["token"].ExpiresAt = expiresAt.Add(time.Hour)

	if !snapshot.Drafts["draft"].SubmittedAt.Equal(wantSubmittedAt) {
		t.Fatalf("draft submitted_at aliased live state: %v", snapshot.Drafts["draft"].SubmittedAt)
	}
	if got := snapshot.Drafts["draft"].DiffPreview.Items[0].OldValue.(map[string]any)["nested"].([]any)[0].(map[string]any)["value"]; got != "before" {
		t.Fatalf("draft diff preview aliased live state: %v", got)
	}
	if got := snapshot.Diffs["diff"].Items[0].OldValue.(map[string]any)["nested"].([]any)[0].(map[string]any)["value"]; got != "before" {
		t.Fatalf("diff item aliased live state: %v", got)
	}
	if got := snapshot.Endpoints["endpoint"].Tags[0]; got != "before" {
		t.Fatalf("endpoint tags aliased live state: %q", got)
	}
	if got := snapshot.Endpoints["endpoint"].Parameters.(map[string]any)["items"].([]any)[0].(map[string]any)["name"]; got != "before" {
		t.Fatalf("endpoint parameters aliased live state: %v", got)
	}
	if snapshot.Tokens["token"].Scopes[0] != ScopeAPIRead || snapshot.Tokens["token"].TokenCiphertext[0] != 1 || !snapshot.Tokens["token"].ExpiresAt.Equal(wantExpiresAt) {
		t.Fatalf("token mutable fields aliased live state: %+v", snapshot.Tokens["token"])
	}
}
