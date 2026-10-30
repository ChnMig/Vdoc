package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	repo "vdoc/domain/documentrepo"
	"vdoc/domain/documentshare"
	"vdoc/utils/id"
)

func newRepositoryShareFixture(t *testing.T) (*Store, string, *repo.Branch, *repo.Commit, *repo.Release) {
	t.Helper()
	s := NewStore()
	project, team := id.GenerateID(), id.GenerateID()
	s.users["admin"] = &User{ID: "admin", Status: UserStatusActive, IsSuperAdmin: true}
	s.teams[team] = &Team{ID: team}
	s.projects[project] = &Project{ID: project, TeamID: team, Status: ProjectStatusActive}
	branches, err := s.RepositoryBranches("admin", project)
	if err != nil {
		t.Fatal(err)
	}
	branch, err := s.RepositoryCreateBranch("admin", project, repo.BranchInput{Name: "share-test", FromCommitID: branches[0].HeadCommitID})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := s.RepositorySave("admin", project, branch.ID, repo.SaveInput{ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "share-initial", Changes: []repo.Change{{Operation: "upsert", Name: "Read me", RelativePath: "docs/original.md", DocumentType: DocumentTypeMarkdown, Content: "# Original\r\n\r\nexact content\r\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	branch.HeadCommitID = commit.ID
	branch.Generation++
	release, err := s.RepositoryPublish("admin", project, repo.PublishInput{BranchID: branch.ID, CommitID: commit.ID, ExpectedGeneration: branch.Generation})
	if err != nil {
		t.Fatal(err)
	}
	return s, project, branch, commit, release
}

func TestRepositorySharePinsPublishedDocumentAndSafeMetadata(t *testing.T) {
	s, project, branch, commit, release := newRepositoryShareFixture(t)
	document := commit.Files[0].DocumentID
	created, err := s.CreateRepositoryShare("admin", project, release.ID, document, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Share.ID) != 32 || created.Share.ReleaseID != release.ID {
		t.Fatal("share identity is not a server-generated fixed release")
	}
	values, err := s.ListRepositoryShares("admin", project, release.ID, document)
	if err != nil || len(values) != 1 {
		t.Fatalf("share list count=%d err=%v", len(values), err)
	}
	raw, _ := json.Marshal(values)
	for _, sensitive := range []string{created.Secret, "capability", "password_verifier", "Ciphertext"} {
		if strings.Contains(string(raw), sensitive) {
			t.Fatal("share metadata leaked capability material")
		}
	}
	meta, err := s.PublicRepositoryShareMetadata(created.Share.ID, created.Secret, "")
	if err != nil || meta.CurrentVersion.ID != release.ID || meta.VersionScope != DocumentShareScopeFixedVersion || meta.DocumentName != "Read me" {
		t.Fatalf("fixed metadata=%+v err=%v", meta, err)
	}
	updated, err := s.RepositorySave("admin", project, branch.ID, repo.SaveInput{ExpectedHeadCommitID: commit.ID, ExpectedGeneration: branch.Generation, IdempotencyKey: "share-update", Changes: []repo.Change{{Operation: "upsert", DocumentID: document, Name: "Changed name", RelativePath: "changed.md", DocumentType: DocumentTypeMarkdown, Content: "# Not the shared body\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	newRelease, err := s.RepositoryPublish("admin", project, repo.PublishInput{BranchID: branch.ID, CommitID: updated.ID, ExpectedGeneration: branch.Generation + 1})
	if err != nil {
		t.Fatal(err)
	}
	body, err := s.PublicRepositoryShareContent(created.Share.ID, created.Secret, "", release.ID)
	if err != nil || body.Content != "# Original\r\n\r\nexact content\r\n" {
		t.Fatalf("fixed body changed err=%v", err)
	}
	download, err := s.PublicRepositoryShareDownload(created.Share.ID, created.Secret, "", release.ID)
	if err != nil || download.Filename != "original.md" || string(download.Body) != body.Content {
		t.Fatalf("fixed download name=%v err=%v", download, err)
	}
	for _, wrongVersion := range []string{newRelease.ID, updated.ID, document} {
		if _, err = s.PublicRepositoryShareContent(created.Share.ID, created.Secret, "", wrongVersion); !errors.Is(err, ErrNotFound) {
			t.Fatal("fixed capability leaked another revision")
		}
	}
	if _, err = s.PublicRepositoryShareMetadata(created.Share.ID, "vdoc_share_"+strings.Repeat("0", 48), ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("wrong capability was accepted")
	}
	if _, err = s.CreateRepositoryShare("admin", project, updated.ID, document, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent}); !errors.Is(err, ErrNotFound) {
		t.Fatal("unpublished commit accepted as release")
	}
	if _, err = s.CreateRepositoryShare("admin", project, release.ID, id.GenerateID(), repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent}); !errors.Is(err, ErrNotFound) {
		t.Fatal("unpublished document accepted")
	}
	for _, audit := range s.AuditLogsForTest() {
		encoded, _ := json.Marshal(audit)
		if strings.Contains(string(encoded), created.Secret) {
			t.Fatal("audit leaked share secret")
		}
	}
}

func TestRepositorySharePasswordRevocationExpiryAndPermissionBoundaries(t *testing.T) {
	s, project, _, commit, release := newRepositoryShareFixture(t)
	document := commit.Files[0].DocumentID
	const password = "share-password-1234"
	created, err := s.CreateRepositoryShare("admin", project, release.ID, document, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetOneMonth, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicRepositoryShareMetadata(created.Share.ID, created.Secret, ""); !errors.Is(err, ErrPublicSharePasswordRequired) {
		t.Fatal("protected metadata bypassed password")
	}
	if _, _, err = s.UnlockPublicRepositoryShare(created.Share.ID, created.Secret, "wrong-password-1234"); !errors.Is(err, ErrNotFound) {
		t.Fatal("wrong password accepted")
	}
	proof, expires, err := s.UnlockPublicRepositoryShare(created.Share.ID, created.Secret, password)
	if err != nil || proof == "" || !expires.After(time.Now()) {
		t.Fatalf("unlock err=%v", err)
	}
	if _, err = s.PublicRepositoryShareContent(created.Share.ID, created.Secret, proof, release.ID); err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateRepositoryShare("admin", project, release.ID, document, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicRepositoryShareContent(second.Share.ID, second.Secret, proof, release.ID); !errors.Is(err, ErrPublicSharePasswordRequired) {
		t.Fatal("unlock proof crossed share boundary")
	}
	s.users["reader"] = &User{ID: "reader", Status: UserStatusActive}
	s.members[memberKey(project, "reader")] = &ProjectMember{ProjectID: project, UserID: "reader", Role: MemberRoleReader, Status: MemberStatusActive}
	if _, err = s.CreateRepositoryShare("reader", project, release.ID, document, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("reader created public share")
	}
	passwordHashed := false
	originalHash := s.hashPassword
	s.hashPassword = func([]byte) (string, error) { passwordHashed = true; return "unused", nil }
	_, readerPasswordErr := s.CreateRepositoryShare("reader", project, release.ID, document, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent, Password: password})
	s.hashPassword = originalHash
	if !errors.Is(readerPasswordErr, ErrPermissionDenied) || passwordHashed {
		t.Fatal("unauthorized reader reached password hashing")
	}
	if _, err = s.RevealRepositoryShare("reader", project, release.ID, document, created.Share.ID); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("reader revealed capability")
	}
	if _, err = s.RevokeRepositoryShare("reader", project, release.ID, document, created.Share.ID); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("reader revoked capability")
	}
	agent := s.WithContext(WithMCPToken(context.Background(), "not-human"))
	if _, err = agent.CreateRepositoryShare("admin", project, release.ID, document, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal("agent created public share")
	}
	if _, err = s.RevokeRepositoryShare("admin", project, release.ID, document, created.Share.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicRepositoryShareContent(created.Share.ID, created.Secret, proof, release.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked share accepted existing proof")
	}
	if _, err = s.RevealRepositoryShare("admin", project, release.ID, document, created.Share.ID); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatal("revoked secret reveal allowed")
	}
	err = s.WithRepositoryWrite("admin", project, MemberRoleAdmin, func(storage repo.Storage) error {
		value, record, e := repositoryManagedShare(s.requestContext(), storage, project, release.ID, document, second.Share.ID)
		if e != nil {
			return e
		}
		past := time.Now().Add(-time.Second)
		value.ExpiresAt = &past
		return repositoryPut(s.requestContext(), storage, project, repositoryShareKind, value.ID, value, record.Generation+1, &record.Generation)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicRepositoryShareMetadata(second.Share.ID, second.Secret, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired share exposed password challenge")
	}
	s.projects[project].Status = ProjectStatusArchived
	if _, err = s.PublicRepositoryShareMetadata(created.Share.ID, created.Secret, proof); !errors.Is(err, ErrNotFound) {
		t.Fatal("archived project exposed a share")
	}
}

func TestRepositoryShareCipherRotationIsAtomicAndKeepsExistingLinks(t *testing.T) {
	s, project, _, commit, release := newRepositoryShareFixture(t)
	const oldKey = "old-repository-share-key-0123456789012345"
	const newKey = "new-repository-share-key-0123456789012345"
	s.cipherKeyring = mustServiceKeyring(t, "shares-old", oldKey, nil)
	created, err := s.CreateRepositoryShare("admin", project, release.ID, commit.Files[0].DocumentID, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatal(err)
	}
	s.cipherKeyring = mustServiceKeyring(t, "shares-new", newKey, nil)
	before := s.repoMemory.clone()
	if err = s.InitRepositoryShares(); err == nil {
		t.Fatal("rotation accepted missing historical key")
	}
	if string(before.records[recordKey(project, repositoryShareKind, created.Share.ID)].Payload) != string(s.repoMemory.records[recordKey(project, repositoryShareKind, created.Share.ID)].Payload) {
		t.Fatal("failed rotation changed persisted secret")
	}
	s.cipherKeyring = mustServiceKeyring(t, "shares-new", newKey, map[string]string{"shares-old": oldKey})
	if err = s.InitRepositoryShares(); err != nil {
		t.Fatal(err)
	}
	s.cipherKeyring = mustServiceKeyring(t, "shares-new", newKey, nil)
	revealed, err := s.RevealRepositoryShare("admin", project, release.ID, commit.Files[0].DocumentID, created.Share.ID)
	if err != nil || revealed.Secret != created.Secret {
		t.Fatal("key rotation broke existing capability")
	}
	if err = s.InitRepositoryShares(); err != nil {
		t.Fatal(err)
	}
}
