package vdoc

import "testing"

// 故障注入用例只捕获已加载的快照，避免在目标操作前触发对象存储故障。
func reviewInputForTest(t *testing.T, store *Store, actorID, projectID, documentID, draftID string) DraftReviewInput {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	return DraftReviewInput{ExpectedReviewRevision: store.drafts[draftID].ReviewRevision()}
}

func TestReviewRejectsChangedAndResubmittedDrafts(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		name := "openapi"
		if markdown {
			name = "markdown"
		}
		for _, action := range []string{"approve", "request-changes", "reject"} {
			t.Run(name+"/"+action, func(t *testing.T) {
				store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
				original, changed := testOpenAPI("reviewed"), testOpenAPI("unreviewed")
				if markdown {
					store, projectID, documentID, branchID = newMarkdownDocumentFlowStore(t)
					original, changed = "# Reviewed\n", "# Unreviewed\n"
				}
				create, update, submit, review := store.CreateDraft, store.UpdateDraft, store.SubmitDraft, store.ReviewDraft
				if markdown {
					create, update, submit, review = store.CreateMarkdownDraft, store.UpdateMarkdownDraft, store.SubmitMarkdownDraft, store.ReviewMarkdownDraft
				}
				objects := newRecordingObjectStorage(nil)
				store.objects = objects
				d, err := create("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: original})
				if err != nil {
					t.Fatal(err)
				}
				first, err := submit("writer", projectID, documentID, d.ID)
				if err != nil {
					t.Fatal(err)
				}
				old := DraftReviewInput{ExpectedReviewRevision: first.ReviewRevision()}
				returned, err := review("admin", projectID, documentID, d.ID, "request-changes", old)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := update("writer", projectID, documentID, d.ID, DraftPatchInput{ExpectedRevision: returned.(*ContractDraft).Revision(), SchemaContent: changed}); err != nil {
					t.Fatal(err)
				}
				current, err := submit("writer", projectID, documentID, d.ID)
				if err != nil {
					t.Fatal(err)
				}
				writes, audits := len(objects.writes), len(store.audits)
				if _, err := review("admin", projectID, documentID, d.ID, action, old); !Is(err, ErrFailedPrecondition) {
					t.Fatalf("stale %s error = %v", action, err)
				}
				if _, err := review("admin", projectID, documentID, d.ID, action, DraftReviewInput{}); !Is(err, ErrInvalidArgument) {
					t.Fatalf("missing review revision error = %v", err)
				}
				if len(objects.writes) != writes || len(store.audits) != audits || len(store.versions) != 0 || store.drafts[d.ID].Status != DraftStatusSubmitted {
					t.Fatal("rejected review mutated publication state, audit, or objects")
				}
				if _, err := review("admin", projectID, documentID, d.ID, action, DraftReviewInput{ExpectedReviewRevision: current.ReviewRevision()}); err != nil {
					t.Fatalf("fresh review: %v", err)
				}
			})
		}
	}
}

func TestResubmissionOfIdenticalContentInvalidatesOldReview(t *testing.T) {
	store, _, projectID, documentID, branchID := newContractPipelineStore(t)
	d, err := store.CreateDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("unchanged")})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.SubmitDraft("writer", projectID, documentID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := DraftReviewInput{ExpectedReviewRevision: first.ReviewRevision()}
	if _, err := store.ReviewDraft("admin", projectID, documentID, d.ID, "request-changes", old); err != nil {
		t.Fatal(err)
	}
	second, err := store.SubmitDraft("writer", projectID, documentID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision() != second.Revision() {
		t.Fatal("fixture must resubmit identical content and metadata")
	}
	if _, err := store.ReviewDraft("admin", projectID, documentID, d.ID, "approve", old); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("old submission review = %v", err)
	}
	if _, err := store.ReviewDraft("admin", projectID, documentID, d.ID, "approve", DraftReviewInput{ExpectedReviewRevision: second.ReviewRevision()}); err != nil {
		t.Fatal(err)
	}
}

func reviewTestSchema(extraPath string) string {
	operation := `{"get":{"responses":{"200":{"description":"ok"}}}}`
	paths := `"/widgets":` + operation
	if extraPath != "" {
		paths += `,"` + extraPath + `":` + operation
	}
	return `{"openapi":"3.1.0","info":{"title":"Review","version":"1.0.0"},"paths":{` + paths + `}}`
}

func TestReviewPreviewTracksLatestAndBlocksStalePublication(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		for _, initialBaseline := range []bool{false, true} {
			name := "openapi"
			if markdown {
				name = "markdown"
			}
			if initialBaseline {
				name += "/existing-baseline"
			} else {
				name += "/first-publication"
			}
			t.Run(name, func(t *testing.T) {
				store, projectID, documentID, branchID := newOpenAPIDocumentFlowStore(t)
				base, candidate, concurrent := reviewTestSchema(""), reviewTestSchema("/candidate"), reviewTestSchema("/must-keep")
				if markdown {
					store, projectID, documentID, branchID = newMarkdownDocumentFlowStore(t)
					base = "# Guide\n\nCore instruction.\n"
					candidate, concurrent = base+"\nCandidate change.\n", base+"\nMandatory security check.\n"
				}
				create, submit, review, compare := store.CreateDraft, store.SubmitDraft, store.ReviewDraft, store.CompareVersions
				if markdown {
					create, submit, review, compare = store.CreateMarkdownDraft, store.SubmitMarkdownDraft, store.ReviewMarkdownDraft, store.CompareMarkdownVersions
				}
				publish := func(versionName, content string) *ContractVersion {
					t.Helper()
					d, err := create("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: versionName, SchemaContent: content})
					if err != nil {
						t.Fatal(err)
					}
					submitted, err := submit("writer", projectID, documentID, d.ID)
					if err != nil {
						t.Fatal(err)
					}
					v, err := review("admin", projectID, documentID, d.ID, "approve", DraftReviewInput{ExpectedReviewRevision: submitted.ReviewRevision()})
					if err != nil {
						t.Fatal(err)
					}
					return v.(*ContractVersion)
				}
				if initialBaseline {
					publish("1.0.0", base)
				}
				d, err := create("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "1.2.0", SchemaContent: candidate})
				if err != nil {
					t.Fatal(err)
				}
				old, err := submit("writer", projectID, documentID, d.ID)
				if err != nil {
					t.Fatal(err)
				}
				intervening := publish("1.1.0", concurrent)
				if _, err := review("admin", projectID, documentID, d.ID, "approve", DraftReviewInput{ExpectedReviewRevision: old.ReviewRevision()}); !Is(err, ErrFailedPrecondition) {
					t.Fatalf("stale baseline review = %v", err)
				}
				refreshed, content, err := store.ReadDraftContent("admin", projectID, documentID, d.ID, "raw")
				if err != nil {
					t.Fatal(err)
				}
				if refreshed.DiffPreview == nil || refreshed.DiffPreview.FromVersionID != intervening.ID || content.Content != candidate {
					t.Fatal("review snapshot did not refresh preview and preserve content")
				}
				if old.Revision() != refreshed.Revision() || !old.UpdatedAt.Equal(refreshed.UpdatedAt) {
					t.Fatal("cache refresh changed the editor revision or draft timestamp")
				}
				if refreshed.ReviewRevision() == old.ReviewRevision() {
					t.Fatal("new baseline retained stale review revision")
				}
				if !markdown && (refreshed.DiffPreview.Summary.RemovedEndpoints != 1 || refreshed.DiffPreview.Summary.BreakingChanges != 1) {
					t.Fatalf("new preview did not report deletion: %+v", refreshed.DiffPreview.Summary)
				}
				result, err := review("admin", projectID, documentID, d.ID, "approve", DraftReviewInput{ExpectedReviewRevision: refreshed.ReviewRevision()})
				if err != nil {
					t.Fatal(err)
				}
				published := result.(*ContractVersion)
				actual, err := compare("reader", projectID, documentID, intervening.ID, published.ID)
				if err != nil {
					t.Fatal(err)
				}
				if actual.FromVersionID != refreshed.DiffPreview.FromVersionID || actual.Summary != refreshed.DiffPreview.Summary {
					t.Fatalf("published diff disagrees with reviewed preview: actual=%+v preview=%+v", actual, refreshed.DiffPreview)
				}
				publish("1.3.0", base)
				archived, err := store.Draft("reader", projectID, documentID, d.ID)
				if err != nil {
					t.Fatal(err)
				}
				if archived.DiffPreview.FromVersionID != intervening.ID {
					t.Fatal("published draft lost its historical review baseline")
				}
				store.drafts[d.ID].DiffPreview = &Diff{Summary: refreshed.DiffPreview.Summary}
				legacy, err := store.Draft("reader", projectID, documentID, d.ID)
				if err != nil {
					t.Fatal(err)
				}
				if legacy.DiffPreview == nil || legacy.DiffPreview.FromVersionID != intervening.ID || len(legacy.DiffPreview.Items) == 0 {
					t.Fatal("legacy summary-only preview was not restored from publication history")
				}
			})
		}
	}
}
