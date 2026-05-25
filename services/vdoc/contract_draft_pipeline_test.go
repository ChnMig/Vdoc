package vdoc

import "testing"

func TestCreateDraftNoChangeDoesNotPersistNewDraft(t *testing.T) {
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)

	initial := publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.0.0", testOpenAPI("same"))
	beforeDrafts := len(store.drafts)
	beforeVersions := len(store.versions)

	_, err := store.CreateDraft("writer", projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.1", SchemaContent: testOpenAPI("same")})
	if !Is(err, ErrFailedPrecondition) {
		t.Fatalf("duplicate CreateDraft() error = %v, want failed precondition", err)
	}
	if len(store.drafts) != beforeDrafts || len(store.versions) != beforeVersions {
		t.Fatalf("duplicate upload mutated state: drafts %d->%d versions %d->%d", beforeDrafts, len(store.drafts), beforeVersions, len(store.versions))
	}
	if latest := store.latestVersionLocked(serviceID, branchID); latest == nil || latest.ID != initial.ID {
		t.Fatalf("latest version changed after no-change upload: %+v", latest)
	}
}

func TestUpdateDraftNoChangeKeepsExistingDraftUntouched(t *testing.T) {
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)

	publishContractDraft(t, store, "admin", projectID, serviceID, branchID, "1.0.0", testOpenAPI("baseline"))
	draft, err := store.CreateDraft("writer", projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.1", SchemaContent: testOpenAPI("changed")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	originalHash := draft.NormalizedSchemaHash
	originalVersionName := draft.VersionName

	_, err = store.UpdateDraft("writer", projectID, serviceID, draft.ID, DraftInput{VersionName: "1.0.1-no-change", SchemaContent: testOpenAPI("baseline")})
	if !Is(err, ErrFailedPrecondition) {
		t.Fatalf("no-change UpdateDraft() error = %v, want failed precondition", err)
	}
	current := store.drafts[draft.ID]
	if current.VersionName != originalVersionName || current.NormalizedSchemaHash != originalHash {
		t.Fatalf("no-change update mutated draft: got version=%q hash=%q want version=%q hash=%q", current.VersionName, current.NormalizedSchemaHash, originalVersionName, originalHash)
	}
}

func TestSubmittedAndPublishedDraftsCannotBeChangedByWriter(t *testing.T) {
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)

	submitted, err := store.CreateDraft("writer", projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("submitted")})
	if err != nil {
		t.Fatalf("CreateDraft(submitted) error = %v", err)
	}
	if _, err := store.SubmitDraft("writer", projectID, serviceID, submitted.ID); err != nil {
		t.Fatalf("SubmitDraft(submitted) error = %v", err)
	}
	if _, err := store.UpdateDraft("writer", projectID, serviceID, submitted.ID, DraftInput{VersionName: "1.0.0-edit", SchemaContent: testOpenAPI("submittedEdit")}); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("UpdateDraft(submitted) error = %v, want failed precondition", err)
	}

	published := publishContractDraft(t, store, "writer", projectID, serviceID, branchID, "2.0.0", testOpenAPI("published"))
	var publishedDraftID string
	for _, draft := range store.drafts {
		if draft.VersionName == published.VersionName {
			publishedDraftID = draft.ID
			break
		}
	}
	if publishedDraftID == "" {
		t.Fatal("published draft not found")
	}
	if _, err := store.SubmitDraft("writer", projectID, serviceID, publishedDraftID); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("SubmitDraft(published) error = %v, want failed precondition", err)
	}
}

func TestDraftReviewAndPromotePipelineRecordsMetadata(t *testing.T) {
	store, branches, projectID, serviceID, devBranchID := newContractPipelineStore(t)
	targetBranchID := branches["test"].ID

	sourceVersion := publishContractDraft(t, store, "admin", projectID, serviceID, devBranchID, "1.0.0", testOpenAPI("source"))
	targetBaseline := publishContractDraft(t, store, "admin", projectID, serviceID, targetBranchID, "0.9.0", testOpenAPI("targetBaseline"))

	promoted, err := store.PromoteDraft("admin", projectID, serviceID, PromoteInput{SourceBranchID: devBranchID, TargetBranchID: targetBranchID, VersionName: "1.0.0-test", Changelog: "promote dev to test"})
	if err != nil {
		t.Fatalf("PromoteDraft() error = %v", err)
	}
	if promoted.SourceType != SourceTypePromote || promoted.BranchID != targetBranchID || promoted.SourceBranchID != devBranchID || promoted.SourceVersionID != sourceVersion.ID || promoted.BaseVersionID != targetBaseline.ID {
		t.Fatalf("promoted draft metadata = %+v, want source/target/base metadata", promoted)
	}
	if promoted.DiffPreview == nil || diffPreviewChangeCount(promoted.DiffPreview.Summary) == 0 {
		t.Fatalf("promoted diff preview = %+v, want non-empty diff preview against target baseline", promoted.DiffPreview)
	}

	if submitted, err := store.SubmitDraft("writer", projectID, serviceID, promoted.ID); err != nil {
		t.Fatalf("SubmitDraft(promoted) error = %v", err)
	} else if submitted.Status != DraftStatusSubmitted {
		t.Fatalf("submitted status = %d, want submitted", submitted.Status)
	}
	changes, err := store.ReviewDraft("admin", projectID, serviceID, promoted.ID, "request-changes")
	if err != nil {
		t.Fatalf("request changes error = %v", err)
	}
	if changes.(*ContractDraft).Status != DraftStatusChangesRequested {
		t.Fatalf("request changes status = %d", changes.(*ContractDraft).Status)
	}
	if _, err := store.SubmitDraft("writer", projectID, serviceID, promoted.ID); err != nil {
		t.Fatalf("resubmit promoted draft error = %v", err)
	}
	rejected, err := store.ReviewDraft("admin", projectID, serviceID, promoted.ID, "reject")
	if err != nil {
		t.Fatalf("reject error = %v", err)
	}
	if rejected.(*ContractDraft).Status != DraftStatusRejected {
		t.Fatalf("reject status = %d", rejected.(*ContractDraft).Status)
	}

	publishCandidate, err := store.PromoteDraft("admin", projectID, serviceID, PromoteInput{SourceBranchID: devBranchID, TargetBranchID: targetBranchID, VersionName: "1.0.1-test"})
	if err != nil {
		t.Fatalf("PromoteDraft(publish candidate) error = %v", err)
	}
	if _, err := store.SubmitDraft("writer", projectID, serviceID, publishCandidate.ID); err != nil {
		t.Fatalf("SubmitDraft(publish candidate) error = %v", err)
	}
	publishedAny, err := store.ReviewDraft("admin", projectID, serviceID, publishCandidate.ID, "approve")
	if err != nil {
		t.Fatalf("approve promoted draft error = %v", err)
	}
	published := publishedAny.(*ContractVersion)
	if published.SourceBranchID != devBranchID || published.SourceVersionID != sourceVersion.ID || published.BaseVersionID != targetBaseline.ID {
		t.Fatalf("published promote metadata = %+v", published)
	}
}

func TestSchemaRetrievalChecksOwnershipAndKind(t *testing.T) {
	store, _, projectID, serviceID, branchID := newContractPipelineStore(t)
	draft, err := store.CreateDraft("writer", projectID, serviceID, DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: testOpenAPI("retrieve")})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}

	rawDraft, err := store.DraftSchema("reader", projectID, serviceID, draft.ID, "raw")
	if err != nil {
		t.Fatalf("DraftSchema(raw) error = %v", err)
	}
	if rawDraft.Kind != "raw" || rawDraft.Content != draft.RawSchema || rawDraft.ObjectKey != draft.RawSchemaObjectKey || rawDraft.Hash != draft.RawSchemaHash {
		t.Fatalf("raw draft schema = %+v, want draft raw schema", rawDraft)
	}
	normalizedDraft, err := store.DraftSchema("reader", projectID, serviceID, draft.ID, "normalized")
	if err != nil {
		t.Fatalf("DraftSchema(normalized) error = %v", err)
	}
	if normalizedDraft.Kind != "normalized" || normalizedDraft.Content != draft.NormalizedSchema || normalizedDraft.ObjectKey != draft.NormalizedObjectKey || normalizedDraft.Hash != draft.NormalizedSchemaHash {
		t.Fatalf("normalized draft schema = %+v, want draft normalized schema", normalizedDraft)
	}
	if _, err := store.DraftSchema("admin", "project-b", serviceID, draft.ID, "raw"); !Is(err, ErrNotFound) {
		t.Fatalf("cross-project DraftSchema() error = %v, want not found", err)
	}
	if _, err := store.DraftSchema("reader", projectID, serviceID, draft.ID, "parsed"); !Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid DraftSchema kind error = %v, want invalid argument", err)
	}

	if _, err := store.SubmitDraft("writer", projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft() error = %v", err)
	}
	publishedAny, err := store.ReviewDraft("admin", projectID, serviceID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("approve draft error = %v", err)
	}
	version := publishedAny.(*ContractVersion)
	rawVersion, err := store.VersionSchema("reader", projectID, serviceID, version.ID, "raw")
	if err != nil {
		t.Fatalf("VersionSchema(raw) error = %v", err)
	}
	if rawVersion.Kind != "raw" || rawVersion.Content != version.RawSchema || rawVersion.ObjectKey != version.RawSchemaObjectKey || rawVersion.Hash != version.RawSchemaHash {
		t.Fatalf("raw version schema = %+v, want version raw schema", rawVersion)
	}
}

func newContractPipelineStore(t *testing.T) (*Store, map[string]*ContractBranch, string, string, string) {
	t.Helper()
	store := newTask5Store()
	service, err := store.CreateService("admin", "project-a", "checkout", "Checkout", "", "/checkout")
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	branches, err := store.ListBranches("admin", "project-a", service.ID)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	byName := map[string]*ContractBranch{}
	for _, branch := range branches {
		byName[branch.Name] = branch
	}
	return store, byName, "project-a", service.ID, byName["dev"].ID
}

func publishContractDraft(t *testing.T, store *Store, actorID, projectID, serviceID, branchID, versionName, schema string) *ContractVersion {
	t.Helper()
	draft, err := store.CreateDraft(actorID, projectID, serviceID, DraftInput{BranchID: branchID, VersionName: versionName, SchemaContent: schema})
	if err != nil {
		t.Fatalf("CreateDraft(%s) error = %v", versionName, err)
	}
	if _, err := store.SubmitDraft(actorID, projectID, serviceID, draft.ID); err != nil {
		t.Fatalf("SubmitDraft(%s) error = %v", versionName, err)
	}
	published, err := store.ReviewDraft("admin", projectID, serviceID, draft.ID, "approve")
	if err != nil {
		t.Fatalf("ReviewDraft approve(%s) error = %v", versionName, err)
	}
	version, ok := published.(*ContractVersion)
	if !ok {
		t.Fatalf("published result = %T, want *ContractVersion", published)
	}
	return version
}

func diffPreviewChangeCount(summary DiffSummary) int {
	return summary.AddedEndpoints + summary.RemovedEndpoints + summary.ModifiedEndpoints
}
