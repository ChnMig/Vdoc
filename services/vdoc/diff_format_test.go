package vdoc

import (
	"errors"
	"strings"
	"testing"
)

func TestDiffSummaryUsesTargetOpenAPIDialect(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		format   int
	}{
		{"3.0.3", "3.0.3", DocumentFormatOpenAPI30},
		{"3.1.0", "3.1.0", DocumentFormatOpenAPI31},
		{"3.0.3", "3.1.0", DocumentFormatOpenAPI31},
		{"3.1.0", "3.0.3", DocumentFormatOpenAPI30},
	} {
		t.Run(tc.from+"_to_"+tc.to, func(t *testing.T) {
			store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
			base := strings.Replace(semanticDiffBodySchema(`{"type":"string"}`, false), `"openapi":"3.1.0"`, `"openapi":"`+tc.from+`"`, 1)
			changed := strings.Replace(strings.Replace(base, `"type":"string"`, `"type":"integer"`, 1), `"openapi":"`+tc.from+`"`, `"openapi":"`+tc.to+`"`, 1)
			from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", base, "base")
			draft, err := store.CreateDocumentDraft("admin", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.1.0", SchemaContent: changed})
			if err != nil {
				t.Fatal(err)
			}
			if draft.SchemaFormat != tc.format || draft.DiffPreview == nil || draft.DiffPreview.Summary.DocumentFormat != tc.format {
				t.Fatalf("draft format=%d preview=%+v, want %d", draft.SchemaFormat, draft.DiffPreview, tc.format)
			}
			if _, err := store.SubmitDocumentDraft("admin", projectID, documentID, draft.ID); err != nil {
				t.Fatal(err)
			}
			published, err := store.ReviewDocumentDraft("admin", projectID, documentID, draft.ID, "approve", reviewInputForTest(t, store, "admin", projectID, documentID, draft.ID))
			if err != nil {
				t.Fatal(err)
			}
			to := published.(*ContractVersion)
			for _, pair := range [][2]*ContractVersion{{from, to}, {to, from}} {
				diff, err := store.CompareDocumentVersions("reader", projectID, documentID, pair[0].ID, pair[1].ID)
				if err != nil {
					t.Fatal(err)
				}
				if diff.Summary.DocumentFormat != pair[1].SchemaFormat || diff.Summary.BreakingChanges != 1 {
					t.Fatalf("diff summary=%+v, want target format=%d and one breaking change", diff.Summary, pair[1].SchemaFormat)
				}
			}
		})
	}
}

func TestUpdatingDraftChangesDiffFormatToNewDialect(t *testing.T) {
	store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
	base := semanticDiffBodySchema(`{"type":"string"}`, false)
	publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", base, "base")
	changed := strings.Replace(base, `"type":"string"`, `"type":"integer"`, 1)
	draft, err := store.CreateDocumentDraft("admin", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.1.0", SchemaContent: changed})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateDocumentDraft("admin", projectID, documentID, draft.ID, DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: strings.Replace(changed, `"3.1.0"`, `"3.0.3"`, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SchemaFormat != DocumentFormatOpenAPI30 || updated.DiffPreview.Summary.DocumentFormat != DocumentFormatOpenAPI30 {
		t.Fatalf("updated draft format=%d preview=%+v", updated.SchemaFormat, updated.DiffPreview)
	}
}

func TestCurrentParserDiffFormatCorrectionPersistsAndRollsBackOnFailure(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		name := "success"
		if failWrite {
			name = "write_failure"
		}
		t.Run(name, func(t *testing.T) {
			store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
			objects := newRecordingObjectStorage(nil)
			store.objects = objects
			base := semanticDiffBodySchema(`{"type":"string"}`, false)
			from := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.0.0", base, "base")
			to := publishOpenAPIDocumentDraft(t, store, "admin", projectID, documentID, branchID, "1.1.0", strings.Replace(base, `"type":"string"`, `"type":"integer"`, 1), "changed")
			diff := store.diffForVersionsLocked(documentID, from.ID, to.ID)
			original := cloneDiff(diff)
			diff.Summary.DocumentFormat = DocumentFormatOpenAPI30
			draft := store.drafts[to.DraftID]
			draft.DiffPreview.Summary.DocumentFormat = DocumentFormatOpenAPI30
			revision, timestamp := draft.Revision(), draft.UpdatedAt
			repo := &legacyFactsRepository{recordingRepository: newRecordingRepository(store.cloneStateLocked())}
			store.persistence = &postgresPersistence{repo: repo}
			if failWrite {
				repo.diffErr = errors.New("format correction write failed")
			}
			preview, err := store.Draft("reader", projectID, documentID, draft.ID)
			if err != nil {
				t.Fatal(err)
			}
			if preview.DiffPreview.Summary.DocumentFormat != DocumentFormatOpenAPI31 || preview.Revision() != revision || !preview.UpdatedAt.Equal(timestamp) {
				t.Fatalf("corrected preview changed user content or kept wrong format: %+v", preview)
			}
			beforeObjects := len(objects.objects)
			updated, err := store.Diff("reader", projectID, documentID, diff.ID)
			if failWrite {
				if !errors.Is(err, repo.diffErr) || repo.state.Diffs[diff.ID].Summary.DocumentFormat != DocumentFormatOpenAPI30 || store.diffs[diff.ID].Summary.DocumentFormat != DocumentFormatOpenAPI30 || len(objects.objects) != beforeObjects {
					t.Fatalf("failed format correction was not rolled back: err=%v objects=%d", err, len(objects.objects))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if updated.Summary.DocumentFormat != DocumentFormatOpenAPI31 || updated.Summary.ParserVersion != openAPIParserVersion || !valuesEqual(updated.Items, original.Items) || updated.ID != original.ID || !updated.CreatedAt.Equal(original.CreatedAt) || repo.diffWrites != 1 {
				t.Fatalf("incorrect format repair: diff=%+v writes=%d", updated, repo.diffWrites)
			}
			reloaded := NewStore()
			reloaded.persistence = &postgresPersistence{repo: repo}
			reloaded.objects = objects
			again, err := reloaded.Diff("reader", projectID, documentID, diff.ID)
			if err != nil || !valuesEqual(updated, again) || repo.diffWrites != 1 {
				t.Fatalf("correction did not persist or was repeated: diff=%+v err=%v writes=%d", again, err, repo.diffWrites)
			}
		})
	}
}
