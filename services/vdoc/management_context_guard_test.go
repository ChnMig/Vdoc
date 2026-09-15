package vdoc

import (
	"context"
	"testing"
	"time"

	domainshare "vdoc/domain/documentshare"
	domainvdoc "vdoc/domain/vdoc"
)

func TestMCPTokenManagementRechecksActorAtCommit(t *testing.T) {
	for _, operation := range []string{"create", "reveal", "revoke_self", "revoke_other", "revoke_user"} {
		changes := []string{"disabled"}
		if operation == "revoke_other" || operation == "revoke_user" {
			changes = append(changes, "demoted")
		}
		for _, change := range changes {
			t.Run(operation+"/"+change, func(t *testing.T) {
				store := newMCPTokenTestStore()
				token, err := store.CreateMCPToken("owner", "existing", []int{ScopeAPIRead}, nil)
				if err != nil {
					t.Fatal(err)
				}
				actorID := "owner"
				wantErr := ErrUnauthenticated
				if operation == "revoke_other" || operation == "revoke_user" {
					actorID, wantErr = "super", ErrPermissionDenied
				}
				repo := newManagementContextRepository(store)
				auditsBefore := len(repo.state.AuditLogs)
				repo.beforeTransaction = func(state *domainvdoc.State) {
					if change == "disabled" {
						state.Users[actorID].Status = UserStatusDisabled
					} else {
						state.Users[actorID].IsSuperAdmin = false
					}
				}
				var result *MCPToken
				switch operation {
				case "create":
					result, err = store.CreateMCPToken(actorID, "must-not-exist", []int{ScopeAPIRead}, nil)
				case "reveal":
					result, err = store.MCPToken(actorID, token.ID)
				case "revoke_self", "revoke_other":
					result, err = store.RevokeMCPToken(actorID, token.ID)
				case "revoke_user":
					result, err = store.RevokeUserMCPToken(actorID, "owner", token.ID)
				}
				if result != nil || !Is(err, wantErr) {
					t.Fatalf("stale %s result present=%t err=%v, want %v", operation, result != nil, err, wantErr)
				}
				if !repo.transactionChanged {
					t.Fatal("operation did not reach the transaction boundary")
				}
				if len(repo.state.Tokens) != 1 || repo.state.Tokens[token.ID].Status != MCPTokenStatusActive || len(repo.state.AuditLogs) != auditsBefore {
					t.Fatalf("denied operation committed tokens=%d status=%d audits=%d, want one active token and %d audits", len(repo.state.Tokens), repo.state.Tokens[token.ID].Status, len(repo.state.AuditLogs), auditsBefore)
				}
			})
		}
	}
}

func TestMCPTokenRevealRechecksTargetAtCommit(t *testing.T) {
	for _, change := range []string{"revoked", "expired"} {
		t.Run(change, func(t *testing.T) {
			store := newMCPTokenTestStore()
			token, err := store.CreateMCPToken("owner", "target-reveal", []int{ScopeAPIRead}, nil)
			if err != nil {
				t.Fatal(err)
			}
			repo := newManagementContextRepository(store)
			auditsBefore := len(repo.state.AuditLogs)
			repo.beforeTransaction = func(state *domainvdoc.State) {
				if change == "revoked" {
					state.Tokens[token.ID].Status = MCPTokenStatusRevoked
				} else {
					expired := time.Now().Add(-time.Second)
					state.Tokens[token.ID].ExpiresAt = &expired
				}
			}
			result, err := store.MCPToken("owner", token.ID)
			if result != nil || !Is(err, ErrFailedPrecondition) || !repo.transactionChanged {
				t.Fatalf("stale reveal result present=%t err=%v changed=%t", result != nil, err, repo.transactionChanged)
			}
			if len(repo.state.AuditLogs) != auditsBefore {
				t.Fatal("denied reveal committed a success audit")
			}
			// 重新读取失效令牌仍返回元数据，不泄露密钥。
			result, err = store.MCPToken("owner", token.ID)
			if err != nil || result == nil || result.Token != "" || result.Status == MCPTokenStatusActive {
				t.Fatalf("inactive token metadata result present=%t err=%v", result != nil, err)
			}
		})
	}
}

func TestSuperAdminCanRevokeOwnTokenAfterDemotion(t *testing.T) {
	store := newMCPTokenTestStore()
	token, err := store.CreateMCPToken("super", "own-token", []int{ScopeAPIRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := newManagementContextRepository(store)
	repo.beforeTransaction = func(state *domainvdoc.State) { state.Users["super"].IsSuperAdmin = false }
	revoked, err := store.RevokeMCPToken("super", token.ID)
	if err != nil || revoked == nil || revoked.Status != MCPTokenStatusRevoked || !repo.transactionChanged {
		t.Fatalf("self revoke after demotion result present=%t err=%v", revoked != nil, err)
	}
}

func TestDocumentShareManagementRechecksContextAtCommit(t *testing.T) {
	for _, operation := range []string{"create", "reveal", "revoke"} {
		changes := []string{"disabled", "demoted", "membership_removed"}
		if operation != "revoke" {
			changes = append(changes, "project_archived", "document_archived", "branch_archived")
		}
		if operation == "reveal" {
			changes = append(changes, "share_revoked", "share_expired")
		}
		for _, change := range changes {
			t.Run(operation+"/"+change, func(t *testing.T) {
				store, projectID, documentID, branchID, share := newManagedShareContextFixture(t)
				repo := newManagementContextRepository(store)
				auditsBefore := len(repo.state.AuditLogs)
				repo.beforeTransaction = func(state *domainvdoc.State) {
					changeManagedShareContext(state, projectID, documentID, branchID, share.Share.ID, change)
				}
				wantErr := ErrFailedPrecondition
				if change == "disabled" || change == "demoted" || change == "membership_removed" {
					wantErr = ErrPermissionDenied
				}
				var err error
				var returned bool
				switch operation {
				case "create":
					result, callErr := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent})
					returned, err = result != nil, callErr
				case "reveal":
					result, callErr := store.RevealDocumentShare("admin", projectID, documentID, share.Share.ID)
					returned, err = result != nil, callErr
				case "revoke":
					result, callErr := store.RevokeDocumentShare("admin", projectID, documentID, share.Share.ID)
					returned, err = result != nil, callErr
				}
				if returned || !Is(err, wantErr) || !repo.transactionChanged {
					t.Fatalf("stale %s returned=%t err=%v changed=%t, want %v", operation, returned, err, repo.transactionChanged, wantErr)
				}
				if len(repo.state.Shares) != 1 || len(repo.state.AuditLogs) != auditsBefore {
					t.Fatalf("denied %s committed shares=%d audits=%d, want one share and %d audits", operation, len(repo.state.Shares), len(repo.state.AuditLogs), auditsBefore)
				}
				if change != "share_revoked" && repo.state.Shares[share.Share.ID].Status != DocumentShareStatusActive {
					t.Fatal("denied operation changed the share status")
				}
			})
		}
	}
}

func TestDocumentShareRevokeAllowsParentsArchivedBeforeCommit(t *testing.T) {
	for _, change := range []string{"project_archived", "document_archived", "branch_archived"} {
		t.Run(change, func(t *testing.T) {
			store, projectID, documentID, branchID, share := newManagedShareContextFixture(t)
			repo := newManagementContextRepository(store)
			repo.beforeTransaction = func(state *domainvdoc.State) {
				changeManagedShareContext(state, projectID, documentID, branchID, share.Share.ID, change)
			}
			revoked, err := store.RevokeDocumentShare("admin", projectID, documentID, share.Share.ID)
			if err != nil || revoked == nil || revoked.Status != DocumentShareStatusRevoked || !repo.transactionChanged {
				t.Fatalf("revoke after %s result present=%t err=%v", change, revoked != nil, err)
			}
			if repo.state.Shares[share.Share.ID].Status != DocumentShareStatusRevoked {
				t.Fatal("share revocation was not persisted")
			}
		})
	}
}

func newManagedShareContextFixture(t *testing.T) (*Store, string, string, string, *DocumentShareSecret) {
	t.Helper()
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", markdownV1(), "share-context")
	share, err := store.CreateDocumentShare("admin", projectID, documentID, DocumentShareInput{BranchID: branchID, VersionScope: DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatal(err)
	}
	return store, projectID, documentID, branchID, share
}

func changeManagedShareContext(state *domainvdoc.State, projectID, documentID, branchID, shareID, change string) {
	switch change {
	case "disabled":
		state.Users["admin"].Status = UserStatusDisabled
	case "demoted":
		state.Members[memberKey(projectID, "admin")].Role = MemberRoleReader
	case "membership_removed":
		delete(state.Members, memberKey(projectID, "admin"))
	case "project_archived":
		state.Projects[projectID].Status = ProjectStatusArchived
	case "document_archived":
		state.APIServices[documentID].Status = DocumentStatusArchived
	case "branch_archived":
		state.Branches[branchID].Status = BranchStatusArchived
	case "share_revoked":
		revokedAt, revokedBy := time.Now().UTC(), "admin"
		share := state.Shares[shareID]
		share.Status, share.RevokedAt, share.RevokedBy, share.UpdatedAt = DocumentShareStatusRevoked, &revokedAt, &revokedBy, revokedAt
	case "share_expired":
		expired := time.Now().Add(-time.Second)
		state.Shares[shareID].ExpiresAt = &expired
	}
}

// 在业务已读取快照后、事务开始前模拟另一实例提交状态变化。
type managementContextRepository struct {
	*recordingRepository
	beforeTransaction  func(*domainvdoc.State)
	transactionChanged bool
}

func newManagementContextRepository(store *Store) *managementContextRepository {
	state := store.cloneStateLocked()
	repo := &managementContextRepository{recordingRepository: newRecordingRepository(state)}
	store.persistence = &postgresPersistence{repo: repo}
	store.persisted = state
	return repo
}

func (r *managementContextRepository) WithinTransaction(ctx context.Context, apply func(domainvdoc.Repository) error) error {
	if r.beforeTransaction != nil {
		change := r.beforeTransaction
		r.beforeTransaction = nil
		change(r.state)
		r.transactionChanged = true
	}
	tx := &managementContextRepository{recordingRepository: newRecordingRepository(r.state)}
	if err := apply(tx); err != nil {
		return err
	}
	r.recordingRepository = tx.recordingRepository
	return nil
}

func (r *managementContextRepository) LockMutationContext(context.Context, domainvdoc.MutationGuard) (*domainvdoc.State, error) {
	return cloneRepositoryStateWithoutBodies(r.state), nil
}

func (r *managementContextRepository) UpsertDocumentShare(_ context.Context, share *domainvdoc.DocumentShare) error {
	r.state.Shares[share.ID] = domainshare.Clone(share)
	return nil
}
