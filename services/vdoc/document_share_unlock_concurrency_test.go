package vdoc

import (
	"context"
	"sync"
	"testing"
	"time"

	domainshare "vdoc/domain/documentshare"
)

func TestPublicShareUnlockReleasesLockAndRechecksAccess(t *testing.T) {
	for _, change := range []string{"unchanged", "revoked", "expired", "project", "document", "branch", "password", "password removed", "capability", "deleted", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
			publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "unlock-concurrency")
			created, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent})
			if err != nil {
				t.Fatal(err)
			}
			initialVerifier := dummyLoginPasswordHash
			store.shares[created.Share.ID].PasswordVerifier = &initialVerifier
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(release) }) }
			t.Cleanup(resume)
			store.verifySharePassword = func(share *DocumentShare, _ string) bool {
				close(started)
				<-release
				// 校验过程使用独立快照，密码字段不能随并发更新一起变化。
				return share.PasswordVerifier != nil && *share.PasswordVerifier == dummyLoginPasswordHash
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type unlockResult struct {
				proof string
				err   error
			}
			finished := make(chan unlockResult, 1)
			go func() {
				proof, _, err := store.WithContext(ctx).UnlockPublicDocumentShare(created.Share.ID, created.Secret, "correct horse battery")
				finished <- unlockResult{proof: proof, err: err}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("share password verification did not start")
			}
			if !store.mu.TryLock() {
				t.Fatal("share password verification holds the shared Store lock")
			}
			switch change {
			case "revoked":
				store.shares[created.Share.ID].Status = DocumentShareStatusRevoked
			case "expired":
				expired := time.Now().Add(-time.Second)
				store.shares[created.Share.ID].ExpiresAt = &expired
			case "project":
				store.projects[projectID].Status = ProjectStatusArchived
			case "document":
				store.apiServices[documentID].Status = DocumentStatusArchived
			case "branch":
				store.branches[branchID].Status = BranchStatusArchived
			case "password":
				changed := "changed password verifier"
				store.shares[created.Share.ID].PasswordVerifier = &changed
			case "password removed":
				store.shares[created.Share.ID].PasswordVerifier = nil
			case "capability":
				store.shares[created.Share.ID].TokenHash = "changed-capability"
			case "deleted":
				delete(store.shares, created.Share.ID)
			case "cancelled":
				cancel()
			}
			store.mu.Unlock()
			if _, err := store.User("admin"); err != nil {
				t.Fatalf("unrelated read during verification: %v", err)
			}
			resume()
			select {
			case result := <-finished:
				if change == "unchanged" {
					if result.err != nil || result.proof == "" {
						t.Fatalf("valid unlock proof=%q error=%v", result.proof, result.err)
					}
				} else if !Is(result.err, ErrNotFound) || result.proof != "" {
					t.Fatalf("changed %s unlock proof=%q error=%v, want uniform unavailable", change, result.proof, result.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("share password verification did not finish")
			}
		})
	}
}
