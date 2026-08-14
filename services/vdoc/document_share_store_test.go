package vdoc

import (
	"context"
	"strings"
	"testing"
	"time"

	domainshare "vdoc/domain/documentshare"
	"vdoc/utils/authentication"
)

func TestDocumentSharePermissionsScopesAndRevocation(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	first := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "share-v1")

	if _, err := store.CreateDocumentShare("writer", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent}); !Is(err, ErrPermissionDenied) {
		t.Fatalf("writer CreateDocumentShare() error = %v, want permission denied", err)
	}
	if _, err := store.CreateDocumentShare("reader", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent}); !Is(err, ErrPermissionDenied) {
		t.Fatalf("reader CreateDocumentShare() error = %v, want permission denied", err)
	}

	latest, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatalf("CreateDocumentShare(latest) error = %v", err)
	}
	if latest.Secret == "" || strings.Contains(latest.Secret, latest.Share.TokenHash) {
		t.Fatalf("latest share capability is invalid")
	}
	if _, err := store.RevealDocumentShare("super", projectID, documentID, latest.Share.ID); err != nil {
		t.Fatalf("super RevealDocumentShare() error = %v", err)
	}
	if content, err := store.PublicDocumentShareContent(latest.Share.ID, latest.Secret, "", first.ID); err != nil || content.Content != markdownV1() {
		t.Fatalf("latest v1 content = (%+v, %v)", content, err)
	}

	all, err := store.CreateDocumentShare("super", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeAllVersions, ExpiryPreset: domainshare.ExpiryPresetOneMonth})
	if err != nil {
		t.Fatalf("CreateDocumentShare(all versions) error = %v", err)
	}
	second := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.1.0", markdownV2(), "share-v2")
	if _, err := store.PublicDocumentShareContent(latest.Share.ID, latest.Secret, "", first.ID); !Is(err, ErrNotFound) {
		t.Fatalf("latest link old version error = %v, want unavailable", err)
	}
	if content, err := store.PublicDocumentShareContent(latest.Share.ID, latest.Secret, "", second.ID); err != nil || content.Content != markdownV2() {
		t.Fatalf("latest link did not follow v2: content=%+v error=%v", content, err)
	}
	versions, err := store.PublicDocumentShareVersions(all.Share.ID, all.Secret, "")
	if err != nil || len(versions) != 2 || versions[0].ID != second.ID || versions[1].ID != first.ID {
		t.Fatalf("all-version history = %+v error=%v", versions, err)
	}
	if _, err := store.PublicDocumentShareContent(all.Share.ID, all.Secret, "", first.ID); err != nil {
		t.Fatalf("all-version historical content error = %v", err)
	}

	revoked, err := store.RevokeDocumentShare("admin", projectID, documentID, latest.Share.ID)
	if err != nil || revoked.Status != DocumentShareStatusRevoked {
		t.Fatalf("RevokeDocumentShare() = (%+v, %v)", revoked, err)
	}
	if _, err := store.PublicDocumentShareMetadata(latest.Share.ID, latest.Secret, ""); !Is(err, ErrNotFound) {
		t.Fatalf("revoked public metadata error = %v, want unavailable", err)
	}
	if _, err := store.RevealDocumentShare("admin", projectID, documentID, latest.Share.ID); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("revoked reveal error = %v, want failed precondition", err)
	}
}

func TestProtectedDocumentShareProofIsolationExpiryAndParentState(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "protected-v1")
	password := "correct horse battery"
	protected, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeAllVersions, ExpiryPreset: domainshare.ExpiryPresetPermanent, Password: password})
	if err != nil {
		t.Fatalf("CreateDocumentShare(protected) error = %v", err)
	}
	other, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeAllVersions, ExpiryPreset: domainshare.ExpiryPresetPermanent, Password: password})
	if err != nil {
		t.Fatalf("CreateDocumentShare(other) error = %v", err)
	}

	for name, credentials := range map[string][2]string{
		"wrong capability": {"vdoc_share_000000000000000000000000000000000000000000000000", password},
		"wrong password":   {protected.Secret, "this is not correct"},
	} {
		if _, _, unlockErr := store.UnlockPublicDocumentShare(protected.Share.ID, credentials[0], credentials[1]); !Is(unlockErr, ErrNotFound) {
			t.Fatalf("%s unlock error = %v, want uniform unavailable", name, unlockErr)
		}
	}
	if _, err := store.PublicDocumentShareMetadata(protected.Share.ID, protected.Secret, ""); !Is(err, ErrNotFound) {
		t.Fatalf("missing proof metadata error = %v, want unavailable", err)
	}
	proof, expiresAt, err := store.UnlockPublicDocumentShare(protected.Share.ID, protected.Secret, password)
	if err != nil || proof == "" || time.Until(expiresAt) <= 0 {
		t.Fatalf("UnlockPublicDocumentShare() proof=%q expires=%v error=%v", proof, expiresAt, err)
	}
	if metadata, err := store.PublicDocumentShareMetadata(protected.Share.ID, protected.Secret, proof); err != nil || metadata.CurrentVersion.ID != version.ID {
		t.Fatalf("protected metadata = (%+v, %v)", metadata, err)
	}
	if _, err := store.PublicDocumentShareMetadata(other.Share.ID, other.Secret, proof); !Is(err, ErrNotFound) {
		t.Fatalf("cross-share proof error = %v, want unavailable", err)
	}
	expiredProof, _, err := authentication.SignDocumentShareUnlockProof(protected.Share.ID, time.Now().Add(-20*time.Minute), nil)
	if err != nil {
		t.Fatalf("SignDocumentShareUnlockProof(expired) error = %v", err)
	}
	if _, err := store.PublicDocumentShareMetadata(protected.Share.ID, protected.Secret, expiredProof); !Is(err, ErrNotFound) {
		t.Fatalf("expired proof error = %v, want unavailable", err)
	}

	store.branches[branchID].Status = BranchStatusArchived
	if _, err := store.PublicDocumentShareMetadata(protected.Share.ID, protected.Secret, proof); !Is(err, ErrNotFound) {
		t.Fatalf("archived parent branch error = %v, want unavailable", err)
	}
}

func TestDocumentShareAuditDoesNotLeakSecrets(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "audit-share")
	password := "audit password value"
	created, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeAllVersions, ExpiryPreset: domainshare.ExpiryPresetPermanent, Password: password})
	if err != nil {
		t.Fatalf("CreateDocumentShare() error = %v", err)
	}
	if _, err := store.RevealDocumentShare("admin", projectID, documentID, created.Share.ID); err != nil {
		t.Fatalf("RevealDocumentShare() error = %v", err)
	}
	proof, _, err := store.UnlockPublicDocumentShare(created.Share.ID, created.Secret, password)
	if err != nil {
		t.Fatalf("UnlockPublicDocumentShare() error = %v", err)
	}
	if _, err := store.PublicDocumentShareContent(created.Share.ID, created.Secret, proof, version.ID, AuditContext{RequestID: "public-view"}); err != nil {
		t.Fatalf("PublicDocumentShareContent() error = %v", err)
	}
	if _, err := store.PublicDocumentShareDownload(created.Share.ID, created.Secret, proof, version.ID, AuditContext{RequestID: "public-download"}); err != nil {
		t.Fatalf("PublicDocumentShareDownload() error = %v", err)
	}
	if _, err := store.RevokeDocumentShare("admin", projectID, documentID, created.Share.ID); err != nil {
		t.Fatalf("RevokeDocumentShare() error = %v", err)
	}

	for _, audit := range store.AuditLogsForTest() {
		for key, value := range audit.Metadata {
			if strings.Contains(value, created.Secret) || strings.Contains(value, password) || strings.Contains(key, "secret") || key == "password" || strings.Contains(key, "verifier") {
				t.Fatalf("audit leaked capability data: action=%s metadata=%+v", audit.Action, audit.Metadata)
			}
		}
	}
}

func TestDocumentShareCreationRequiresPublishedBranch(t *testing.T) {
	store, projectID, documentID, _ := newMarkdownDocumentFlowStore(t)
	branches, err := store.ListBranches("admin", projectID, documentID)
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	for _, branch := range branches {
		if _, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branch.ID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent}); !Is(err, ErrFailedPrecondition) {
			t.Fatalf("branch %s create share error = %v, want no-published-version failure", branch.Name, err)
		}
	}
}

func TestPersistentPublicShareUsesNarrowSnapshotAndAppendOnlyAudit(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "persistent-public-share")
	created, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatalf("CreateDocumentShare() error = %v", err)
	}
	repo := newRecordingRepository(store.stateLocked())
	repo.resetEvents()
	store.persistence = &postgresPersistence{repo: repo}

	content, err := store.PublicDocumentShareContent(created.Share.ID, created.Secret, "", version.ID, AuditContext{RequestID: "narrow-public-read"})
	if err != nil || content.Content != markdownV1() {
		t.Fatalf("PublicDocumentShareContent() = (%+v, %v)", content, err)
	}
	if repo.publicLoads != 1 || repo.publicAccesses != 1 || repo.loads != 0 || repo.saves != 0 {
		t.Fatalf("repository activity publicLoads=%d publicAccesses=%d fullLoads=%d saves=%d", repo.publicLoads, repo.publicAccesses, repo.loads, repo.saves)
	}
	if len(repo.events) != 1 || repo.events[0] != "audit:document_share.view" {
		t.Fatalf("repository events = %v, want one append-only view audit", repo.events)
	}
}

func TestPersistentPublicShareRechecksRevocationAfterLoadingSnapshot(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	store.objects = newRecordingObjectStorage(nil)
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "revoked-after-snapshot")
	created, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatalf("CreateDocumentShare() error = %v", err)
	}
	repo := newRecordingRepository(store.stateLocked())
	store.persistence = &postgresPersistence{repo: repo}
	snapshot, err := repo.LoadPublicDocumentShareSnapshot(context.Background(), created.Share.ID)
	if err != nil {
		t.Fatalf("LoadPublicDocumentShareSnapshot() error = %v", err)
	}
	repo.state.Shares[created.Share.ID].Status = DocumentShareStatusRevoked

	if _, err := store.publicShareContentFromSnapshot(snapshot, created.Secret, "", version.ID, AuditContext{}); !Is(err, ErrNotFound) {
		t.Fatalf("stale snapshot access error = %v, want unavailable after final revocation check", err)
	}
	if repo.publicAccesses != 1 || len(repo.events) != 0 {
		t.Fatalf("revoked access activity publicAccesses=%d events=%v, want one rejected check and no success audit", repo.publicAccesses, repo.events)
	}
}

func TestPersistentPublicShareRejectsTamperedPublishedObject(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	objects := newRecordingObjectStorage(nil)
	store.objects = objects
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "tampered-public-share")
	created, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatalf("CreateDocumentShare() error = %v", err)
	}
	repo := newRecordingRepository(store.stateLocked())
	store.persistence = &postgresPersistence{repo: repo}
	objects.objects[version.RawSchemaObjectKey] = []byte("# tampered\n")

	if _, err := store.PublicDocumentShareContent(created.Share.ID, created.Secret, "", version.ID); !Is(err, ErrNotFound) {
		t.Fatalf("PublicDocumentShareContent() tampered object error = %v, want unavailable", err)
	}
	for _, event := range repo.events {
		if event == "audit:document_share.view" {
			t.Fatal("tampered content must not produce a successful view audit")
		}
	}
}

func TestInMemoryPublicShareRejectsTamperedPublishedContent(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "tampered-in-memory-share")
	created, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatalf("CreateDocumentShare() error = %v", err)
	}
	store.versions[version.ID].RawSchema = "# tampered\n"
	auditCount := len(store.audits)

	if _, err := store.PublicDocumentShareContent(created.Share.ID, created.Secret, "", version.ID); !Is(err, ErrNotFound) {
		t.Fatalf("PublicDocumentShareContent() tampered error = %v, want unavailable", err)
	}
	if _, err := store.PublicDocumentShareDownload(created.Share.ID, created.Secret, "", version.ID); !Is(err, ErrNotFound) {
		t.Fatalf("PublicDocumentShareDownload() tampered error = %v, want unavailable", err)
	}
	if len(store.audits) != auditCount {
		t.Fatalf("tampered access audits = %d, want unchanged %d", len(store.audits), auditCount)
	}
}

func TestPublicShareDownloadMetadataSanitizesUntrustedFilenames(t *testing.T) {
	document := &APIService{Name: "Guide", DocumentType: DocumentTypeMarkdown}
	version := &ContractVersion{RelativePath: "docs/unsafe\r\nname.md"}

	filename, contentType := publicShareDownloadMetadata(document, version, "# Guide")
	if filename != "unsafename.md" || contentType != "text/markdown; charset=utf-8" {
		t.Fatalf("download metadata = (%q, %q), want sanitized Markdown filename", filename, contentType)
	}

	version.RelativePath = strings.Repeat("a", 240) + ".md"
	filename, _ = publicShareDownloadMetadata(document, version, "# Guide")
	if len([]rune(filename)) != 180 || !strings.HasSuffix(filename, ".md") {
		t.Fatalf("bounded filename = %q (%d runes), want 180 runes with extension", filename, len([]rune(filename)))
	}

	version.RelativePath = ".."
	filename, _ = publicShareDownloadMetadata(document, version, "# Guide")
	if filename != "document.md" {
		t.Fatalf("fallback filename = %q, want document.md", filename)
	}
}
