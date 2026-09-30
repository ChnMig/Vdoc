package vdoc

import "testing"

func TestVersionCompareAllowsArchivedTargetBranchWhileDocumentActive(t *testing.T) {
	for _, format := range []string{"openapi", "markdown"} {
		t.Run(format, func(t *testing.T) {
			var store *Store
			var projectID, documentID, sourceBranchID string
			if format == "openapi" {
				store, projectID, documentID, sourceBranchID = newOpenAPIDocumentFlowStore(t)
			} else {
				store, projectID, documentID, sourceBranchID = newMarkdownDocumentFlowStore(t)
			}
			target, err := store.CreateBranch("admin", projectID, documentID, "feature/archival-review", "")
			if err != nil {
				t.Fatalf("CreateBranch() error = %v", err)
			}
			var source, destination *ContractVersion
			if format == "openapi" {
				source = publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, sourceBranchID, "1.0.0-dev", semanticDiffBaselineOpenAPI(), "dev")
				destination = publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, target.ID, "1.1.0-feature", semanticDiffChangedOpenAPI(), "feature")
			} else {
				source = publishMarkdownDocumentDraft(t, store, projectID, documentID, sourceBranchID, "1.0.0-dev", markdownV1(), "dev")
				destination = publishMarkdownDocumentDraft(t, store, projectID, documentID, target.ID, "1.1.0-feature", markdownV2(), "feature")
			}
			compare := func(fromID, toID string) (*Diff, error) {
				if format == "markdown" {
					return store.CompareMarkdownVersions("reader", projectID, documentID, fromID, toID)
				}
				return store.CompareDocumentVersions("reader", projectID, documentID, fromID, toID)
			}
			historical, err := compare(destination.ID, source.ID)
			if err != nil || historical == nil {
				t.Fatalf("active branch comparison = (%+v, %v)", historical, err)
			}
			archived, err := store.ArchiveBranch("admin", projectID, documentID, target.ID)
			if err != nil || archived == nil || archived.Status != BranchStatusArchived {
				t.Fatalf("ArchiveBranch() = (%+v, %v), want archived target", archived, err)
			}

			// 跨分支正向比较此前不存在，归档分支不限制已发布快照之间的新比较。
			before, err := store.ListDocumentDiffs("reader", projectID, documentID, source.ID, destination.ID)
			if err != nil || len(before) != 0 {
				t.Fatalf("forward comparisons before creation = (%+v, %v), want empty", before, err)
			}
			created, err := compare(source.ID, destination.ID)
			if err != nil || created == nil || created.FromVersionID != source.ID || created.ToVersionID != destination.ID {
				t.Fatalf("archived target comparison = (%+v, %v), want source to destination", created, err)
			}
			repeated, err := compare(source.ID, destination.ID)
			if err != nil || repeated == nil || repeated.ID != created.ID || !repeated.CreatedAt.Equal(created.CreatedAt) {
				t.Fatalf("repeated comparison = (%+v, %v), want stored Diff %s", repeated, err, created.ID)
			}
			for _, diff := range []*Diff{historical, created} {
				stored, err := store.DocumentDiff("reader", projectID, documentID, diff.ID)
				if err != nil || stored == nil || stored.ID != diff.ID || stored.FromVersionID != diff.FromVersionID || stored.ToVersionID != diff.ToVersionID {
					t.Fatalf("DocumentDiff() after branch archive = (%+v, %v), want stored Diff %s", stored, err, diff.ID)
				}
				listed, err := store.ListDocumentDiffs("reader", projectID, documentID, diff.FromVersionID, diff.ToVersionID)
				if err != nil || len(listed) != 1 || listed[0].ID != diff.ID {
					t.Fatalf("ListDocumentDiffs() after branch archive = (%+v, %v), want only Diff %s", listed, err, diff.ID)
				}
			}
		})
	}
}
