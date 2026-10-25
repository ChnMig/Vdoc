package vdoc

import (
	"testing"
	"time"

	domainshare "vdoc/domain/documentshare"
)

func TestFixedVersionShareKeepsHistoricalContentAndDeniesOtherVersions(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		name := "memory"
		if persistent {
			name = "persistent snapshot"
		}
		t.Run(name, func(t *testing.T) {
			store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
			if persistent {
				store.objects = newRecordingObjectStorage(nil)
			}
			first := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "fixed-first")
			second := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "2.0.0", markdownV2(), "fixed-second")
			created, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{
				BranchID: branchID, VersionScope: DocumentShareScopeFixedVersion, VersionID: &first.ID,
				ExpiryPreset: domainshare.ExpiryPresetPermanent,
			})
			if err != nil {
				t.Fatal(err)
			}
			third := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "3.0.0", "# Third\n", "fixed-third")
			if persistent {
				store.persistence = &postgresPersistence{repo: newRecordingRepository(store.stateLocked())}
			}
			if created.Share.PinnedVersionID == nil || *created.Share.PinnedVersionID != first.ID {
				t.Fatalf("share lost pinned ID: %+v", created.Share)
			}
			metadata, err := store.PublicDocumentShareMetadata(created.Share.ID, created.Secret, "")
			if err != nil || metadata.CurrentVersion.ID != first.ID || metadata.VersionScope != DocumentShareScopeFixedVersion {
				t.Fatalf("metadata = %+v, error %v", metadata, err)
			}
			content, err := store.PublicDocumentShareContent(created.Share.ID, created.Secret, "", first.ID)
			if err != nil || content.VersionID != first.ID || content.Content != markdownV1() {
				t.Fatalf("fixed content = %+v, error %v", content, err)
			}
			download, err := store.PublicDocumentShareDownload(created.Share.ID, created.Secret, "", first.ID)
			if err != nil || download.VersionID != first.ID || string(download.Body) != markdownV1() {
				t.Fatalf("fixed download = %+v, error %v", download, err)
			}
			for _, forbiddenID := range []string{second.ID, third.ID, "missing-version"} {
				if _, err := store.PublicDocumentShareContent(created.Share.ID, created.Secret, "", forbiddenID); !Is(err, ErrNotFound) {
					t.Fatalf("forbidden content %s: %v", forbiddenID, err)
				}
				if _, err := store.PublicDocumentShareDownload(created.Share.ID, created.Secret, "", forbiddenID); !Is(err, ErrNotFound) {
					t.Fatalf("forbidden download %s: %v", forbiddenID, err)
				}
			}
			if _, err := store.PublicDocumentShareVersions(created.Share.ID, created.Secret, ""); !Is(err, ErrNotFound) {
				t.Fatalf("fixed share exposed history: %v", err)
			}
			shares, err := store.ListDocumentShares("admin", projectID, documentID)
			if err != nil || len(shares) != 1 || shares[0].PinnedVersionID == nil || *shares[0].PinnedVersionID != first.ID {
				t.Fatalf("listed shares = %+v, error %v", shares, err)
			}
			revealed, err := store.RevealDocumentShare("admin", projectID, documentID, created.Share.ID)
			if err != nil || revealed.Secret != created.Secret || revealed.Share.PinnedVersionID == nil || *revealed.Share.PinnedVersionID != first.ID {
				t.Fatalf("revealed share = %+v, error %v", revealed, err)
			}
		})
	}
}

func TestFixedVersionShareRejectsWrongTargetAndAmbiguousInput(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "fixed-validation")
	for _, test := range []struct {
		name, id string
		change   func(*ContractVersion)
	}{
		{"other document", "other-document-version", func(v *ContractVersion) { v.ServiceID = "other-document" }},
		{"other branch", "other-branch-version", func(v *ContractVersion) { v.BranchID = "other-branch" }},
		{"not published", "not-published-version", func(v *ContractVersion) { v.Status = 0 }},
	} {
		copy := *store.versions[version.ID]
		copy.ID = test.id
		test.change(&copy)
		store.versions[copy.ID] = &copy
	}
	for _, forbiddenID := range []string{"other-document-version", "other-branch-version", "not-published-version", "draft-id", "missing-version"} {
		_, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeFixedVersion, VersionID: &forbiddenID, ExpiryPreset: domainshare.ExpiryPresetPermanent})
		if !Is(err, ErrNotFound) {
			t.Fatalf("target %s error = %v, want not found", forbiddenID, err)
		}
	}
	for _, scope := range []int{DocumentShareScopeLatest, DocumentShareScopeAllVersions} {
		_, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: scope, VersionID: &version.ID, ExpiryPreset: domainshare.ExpiryPresetPermanent})
		if !Is(err, ErrInvalidArgument) {
			t.Fatalf("scope %d with version ID error = %v", scope, err)
		}
	}
	if _, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeFixedVersion, ExpiryPreset: domainshare.ExpiryPresetPermanent}); !Is(err, ErrInvalidArgument) {
		t.Fatalf("fixed without ID error = %v", err)
	}
	if len(store.shares) != 0 {
		t.Fatalf("invalid requests created %d shares", len(store.shares))
	}
}

func TestFixedVersionShareRetainsPasswordAndLifecycleProtection(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		for _, change := range []string{"revoked", "expired", "project", "document", "branch", "version"} {
			name := "memory/" + change
			if persistent {
				name = "persistent/" + change
			}
			t.Run(name, func(t *testing.T) {
				store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
				if persistent {
					store.objects = newRecordingObjectStorage(nil)
				}
				version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "fixed-protected")
				created, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeFixedVersion, VersionID: &version.ID, ExpiryPreset: domainshare.ExpiryPresetPermanent, Password: "fixed share password"})
				if err != nil {
					t.Fatal(err)
				}
				var repo *recordingRepository
				if persistent {
					repo = newRecordingRepository(store.stateLocked())
					store.persistence = &postgresPersistence{repo: repo}
				}
				if _, err := store.PublicDocumentShareMetadata(created.Share.ID, created.Secret, ""); !Is(err, ErrPublicSharePasswordRequired) {
					t.Fatalf("missing proof error = %v", err)
				}
				proof, _, err := store.UnlockPublicDocumentShare(created.Share.ID, created.Secret, "fixed share password")
				if err != nil || proof == "" {
					t.Fatalf("unlock error = %v", err)
				}
				if _, err := store.PublicDocumentShareContent(created.Share.ID, created.Secret, proof, version.ID); err != nil {
					t.Fatal(err)
				}
				state := store.stateLocked()
				if persistent {
					state = repo.state
				}
				switch change {
				case "revoked":
					state.Shares[created.Share.ID].Status = DocumentShareStatusRevoked
				case "expired":
					expired := time.Now().Add(-time.Second)
					state.Shares[created.Share.ID].ExpiresAt = &expired
				case "project":
					state.Projects[projectID].Status = ProjectStatusArchived
				case "document":
					state.APIServices[documentID].Status = DocumentStatusArchived
				case "branch":
					state.Branches[branchID].Status = BranchStatusArchived
				case "version":
					state.Versions[version.ID].Status = 0
				}
				if _, err := store.PublicDocumentShareMetadata(created.Share.ID, created.Secret, proof); !Is(err, ErrNotFound) {
					t.Fatalf("inactive metadata error = %v", err)
				}
				if _, err := store.PublicDocumentShareContent(created.Share.ID, created.Secret, proof, version.ID); !Is(err, ErrNotFound) {
					t.Fatalf("inactive content error = %v", err)
				}
				if _, err := store.PublicDocumentShareDownload(created.Share.ID, created.Secret, proof, version.ID); !Is(err, ErrNotFound) {
					t.Fatalf("inactive download error = %v", err)
				}
			})
		}
	}
}

func TestFixedVersionShareRechecksTargetAfterPasswordHash(t *testing.T) {
	for _, change := range []string{"deleted", "unpublished", "document", "branch"} {
		t.Run(change, func(t *testing.T) {
			store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
			version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "fixed-hash-recheck")
			resume := pausePasswordHash(t, store, func() error {
				_, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeFixedVersion, VersionID: &version.ID, ExpiryPreset: domainshare.ExpiryPresetPermanent, Password: "fixed share password"})
				return err
			})
			store.mu.Lock()
			switch change {
			case "deleted":
				delete(store.versions, version.ID)
			case "unpublished":
				store.versions[version.ID].Status = 0
			case "document":
				store.versions[version.ID].ServiceID = "other-document"
			case "branch":
				store.versions[version.ID].BranchID = "other-branch"
			}
			store.mu.Unlock()
			if err := resume(); !Is(err, ErrNotFound) {
				t.Fatalf("changed %s error = %v, want not found", change, err)
			}
			if len(store.shares) != 0 {
				t.Fatalf("changed target created %d shares", len(store.shares))
			}
		})
	}
}
