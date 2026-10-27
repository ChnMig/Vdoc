package vdoc

import (
	"strings"
	"testing"
	"time"
)

func TestAuditActorSearchAndNamesPreserveProjectPermissionsAndPagination(t *testing.T) {
	store := newProjectRoleAuthorizationStore()
	store.users["writer"].Email = "writer+test@example.test"
	store.users["super"].Email = "super@example.test"
	store.projects["project-a"].Name = "测试项目"
	now := time.Now()
	for _, entry := range []struct{ id, actor, project string }{{"a", "writer", "project-a"}, {"b", "super", "project-a"}, {"c", "writer", "project-b"}} {
		store.audits[entry.id] = &AuditLog{ID: entry.id, ActorUserID: entry.actor, ActorType: AuditActorUser, ProjectID: entry.project, Action: "document.create", ResourceType: "document", CreatedAt: now, Metadata: map[string]string{"result": "success"}}
	}
	for _, filter := range []string{"writer", " WRITER+TEST@EXAMPLE.TEST "} {
		page, err := store.QueryAuditLogPage("admin", AuditLogQuery{ProjectID: "project-a", Actor: filter, Limit: 1})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != "a" || page.NextCursor != "" {
			t.Fatalf("scoped actor search: %v %v", page, err)
		}
		if page.Items[0].ActorEmail != "writer+test@example.test" || page.Items[0].ProjectName != "测试项目" {
			t.Fatal("missing human-readable identity")
		}
	}
	if _, err := store.QueryAuditLogPage("reader", AuditLogQuery{ProjectID: "project-a", Actor: "writer"}); !Is(err, ErrPermissionDenied) {
		t.Fatalf("reader access: %v", err)
	}
	if _, err := store.QueryAuditLogPage("admin", AuditLogQuery{Actor: "writer"}); !Is(err, ErrInvalidArgument) {
		t.Fatalf("global admin access: %v", err)
	}
	if _, err := store.QueryAuditLogPage("super", AuditLogQuery{Actor: strings.Repeat("a", 257)}); !Is(err, ErrInvalidArgument) {
		t.Fatalf("oversized filter: %v", err)
	}
	page, err := store.QueryAuditLogPage("super", AuditLogQuery{Actor: "writer", Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("first page: %v %v", page, err)
	}
	next, err := store.QueryAuditLogPage("super", AuditLogQuery{Actor: "writer", Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.NextCursor != "" {
		t.Fatalf("second page: %v %v", next, err)
	}
	if store.audits["a"].ActorEmail != "" || store.audits["a"].ProjectName != "" {
		t.Fatal("display enrichment mutated stored audit")
	}
}
