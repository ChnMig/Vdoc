package vdoc

import (
	"context"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
	"vdoc/db/pgdb"
	domain "vdoc/domain/vdoc"
)

func TestDocumentActivityQueryIsBoundToDocumentAndAllowedActions(t *testing.T) {
	database := folderMappingDryRunDB(t)
	var sql string
	if err := database.Callback().Query().After("gorm:query").Register("test:activity_sql", func(tx *gorm.DB) { sql = tx.Statement.SQL.String() }); err != nil {
		t.Fatal(err)
	}
	_, err := NewRepository(database).ReadAudits(context.Background(), domain.AuditQuery{ProjectID: "project", DocumentID: "document", Actions: []string{"contract_draft.submit", "document_version.publish"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"project_id =", "document_id =", "action IN", "created_at DESC, id DESC", "LIMIT"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("missing %q in %s", fragment, sql)
		}
	}
	if strings.Contains(sql, "service_id") {
		t.Fatal("legacy database column used")
	}
}

func TestDraftHistoryProjectsRealPublicationWithoutInferringUpdatedAt(t *testing.T) {
	database := folderMappingDryRunDB(t)
	publishedAt := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	const published = "11111111111111111111111111111111"
	const unpublished = "22222222222222222222222222222222"
	var publicationSQL string
	if err := database.Callback().Query().After("gorm:query").Register("test:history_publication", func(tx *gorm.DB) {
		switch target := tx.Statement.Dest.(type) {
		case *int64:
			*target = 2
			tx.RowsAffected = 1
		case *[]DocumentDraft:
			*target = []DocumentDraft{{Base: pgdb.Base{ID: published, UpdatedAt: publishedAt.Add(time.Hour)}}, {Base: pgdb.Base{ID: unpublished}}}
		case *[]DocumentVersion:
			publicationSQL = tx.Statement.SQL.String()
			*target = []DocumentVersion{{SourceDraftID: published, PublishedAt: publishedAt}}
		}
	}); err != nil {
		t.Fatal(err)
	}
	drafts, total, err := NewRepository(database).ReadDraftPage(context.Background(), "document", "", domain.PageQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(drafts) != 2 || drafts[0].PublishedAt == nil || !drafts[0].PublishedAt.Equal(publishedAt) || drafts[1].PublishedAt != nil {
		t.Fatalf("history = %+v, total=%d", drafts, total)
	}
	for _, fragment := range []string{"source_draft_id", "document_id =", " IN "} {
		if !strings.Contains(publicationSQL, fragment) {
			t.Fatalf("missing %q in %s", fragment, publicationSQL)
		}
	}
}
