package vdoc

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"

	domainshare "vdoc/domain/documentshare"
	domainvdoc "vdoc/domain/vdoc"
	"vdoc/utils/authentication"
	"vdoc/utils/encryption"
	"vdoc/utils/id"
)

type DocumentShareInput struct {
	BranchID     string
	VersionScope int
	ExpiryPreset domainshare.ExpiryPreset
	Password     string
}

type DocumentShareSecret struct {
	Share  *DocumentShare
	Secret string
}

type PublicShareVersion struct {
	ID          string    `json:"id"`
	VersionName string    `json:"version_name"`
	Changelog   string    `json:"changelog,omitempty"`
	PublishedAt time.Time `json:"published_at"`
}

type PublicShareMetadata struct {
	DocumentName   string             `json:"document_name"`
	DocumentType   int                `json:"document_type"`
	VersionScope   int                `json:"version_scope"`
	ExpiresAt      *time.Time         `json:"expires_at,omitempty"`
	CurrentVersion PublicShareVersion `json:"current_version"`
}

type PublicShareContent struct {
	VersionID string `json:"version_id"`
	Content   string `json:"content"`
}

type PublicShareDownload struct {
	VersionID   string
	Filename    string
	ContentType string
	Body        []byte
}

func (s *Store) CreateDocumentShare(actorID, projectID, documentID string, input DocumentShareInput, auditCtx ...AuditContext) (*DocumentShareSecret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	document, branch, err := s.activeShareParentsLocked(projectID, documentID, input.BranchID)
	if err != nil {
		return nil, err
	}
	if s.latestVersionLocked(document.ID, branch.ID) == nil {
		return nil, fmt.Errorf("%w: document branch has no published version", ErrFailedPrecondition)
	}

	shareID := id.GenerateID()
	secret, capability, err := encryption.GenerateDocumentShareCapability(shareID, mcpTokenCipherKey())
	if err != nil {
		return nil, err
	}
	var passwordVerifier *string
	if input.Password != "" {
		parsed, parseErr := domainshare.ParsePassword(input.Password)
		if parseErr != nil {
			return nil, parseErr
		}
		encoded, hashErr := encryption.HashPasswordBytesWithBcrypt(parsed.Bytes())
		if hashErr != nil {
			return nil, hashErr
		}
		passwordVerifier = &encoded
	}
	now := time.Now().UTC()
	share, err := domainshare.Create(domainshare.CreateParams{
		ID: shareID, ProjectID: projectID, DocumentID: documentID, BranchID: branch.ID,
		TokenHash: capability.Hash, TokenCiphertext: capability.Ciphertext, CipherKID: capability.KID,
		PasswordVerifier: passwordVerifier, VersionScope: input.VersionScope, ExpiryPreset: input.ExpiryPreset,
		CreatedBy: actorID, Now: now,
	})
	if err != nil {
		return nil, err
	}
	s.shares[share.ID] = share
	s.auditLocked(ctx, AuditActorUser, actorID, "document_share.create", "document_share", share.ID, projectID, documentID, documentShareAuditMetadata(share, "success", ""))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return &DocumentShareSecret{Share: domainshare.Clone(share), Secret: secret}, nil
}

func (s *Store) ListDocumentShares(actorID, projectID, documentID string) ([]*DocumentShare, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if document := s.apiServices[documentID]; document == nil || document.ProjectID != projectID {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	shares := make([]*DocumentShare, 0)
	for _, share := range s.shares {
		if share.ProjectID != projectID || share.DocumentID != documentID {
			continue
		}
		copy := domainshare.Clone(share)
		if status, err := domainshare.EffectiveStatus(share, now); err == nil {
			copy.Status = status
		}
		shares = append(shares, copy)
	}
	sort.Slice(shares, func(i, j int) bool { return shares[i].CreatedAt.After(shares[j].CreatedAt) })
	return shares, nil
}

func (s *Store) RevealDocumentShare(actorID, projectID, documentID, shareID string, auditCtx ...AuditContext) (*DocumentShareSecret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	share, err := s.managedDocumentShareLocked(projectID, documentID, shareID)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.activeShareParentsLocked(projectID, documentID, share.BranchID); err != nil {
		return nil, ErrFailedPrecondition
	}
	if err := domainshare.EnsureRevealable(share, time.Now().UTC()); err != nil {
		return nil, err
	}
	secret, err := encryption.RevealDocumentShareCapability(share.ID, mcpTokenCipherKey(), encryption.DocumentShareCapabilityRecord{Hash: share.TokenHash, Ciphertext: share.TokenCiphertext, KID: share.CipherKID})
	if err != nil {
		return nil, err
	}
	s.auditLocked(ctx, AuditActorUser, actorID, "document_share.reveal", "document_share", share.ID, projectID, documentID, documentShareAuditMetadata(share, "success", ""))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return &DocumentShareSecret{Share: domainshare.Clone(share), Secret: secret}, nil
}

func (s *Store) RevokeDocumentShare(actorID, projectID, documentID, shareID string, auditCtx ...AuditContext) (*DocumentShare, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	share, err := s.managedDocumentShareLocked(projectID, documentID, shareID)
	if err != nil {
		return nil, err
	}
	revoked, transitioned, err := domainshare.Revoke(share, actorID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if transitioned {
		s.shares[share.ID] = revoked
		s.auditLocked(ctx, AuditActorUser, actorID, "document_share.revoke", "document_share", share.ID, projectID, documentID, documentShareAuditMetadata(revoked, "success", ""))
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	}
	return domainshare.Clone(revoked), nil
}

func (s *Store) UnlockPublicDocumentShare(shareID, secret, password string, auditCtx ...AuditContext) (string, time.Time, error) {
	if snapshot, supported, err := s.loadPersistentPublicShareSnapshot(shareID); supported {
		if err != nil {
			consumeDummyPublicSharePasswordCheck(password)
			return "", time.Time{}, publicShareUnavailable()
		}
		share, _, _, authorizeErr := authorizePublicShareSnapshot(snapshot, secret, "", false)
		if authorizeErr != nil || !share.PasswordProtected() {
			consumeDummyPublicSharePasswordCheck(password)
			return "", time.Time{}, publicShareUnavailable()
		}
		return unlockPersistentPublicShare(share, password)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return "", time.Time{}, publicShareUnavailable()
	}
	share, _, _, err := s.authorizePublicShareLocked(shareID, secret, "", false)
	if err != nil || !share.PasswordProtected() {
		consumeDummyPublicSharePasswordCheck(password)
		return "", time.Time{}, publicShareUnavailable()
	}
	if !verifyPublicSharePassword(share, password) {
		return "", time.Time{}, publicShareUnavailable()
	}
	proof, expiresAt, err := authentication.SignDocumentShareUnlockProof(share.ID, time.Now().UTC(), share.ExpiresAt)
	if err != nil {
		return "", time.Time{}, publicShareUnavailable()
	}
	return proof, expiresAt, nil
}

func (s *Store) PublicDocumentShareMetadata(shareID, secret, unlockProof string, auditCtx ...AuditContext) (*PublicShareMetadata, error) {
	if snapshot, supported, err := s.loadPersistentPublicShareSnapshot(shareID); supported {
		if err != nil {
			return nil, publicShareUnavailable()
		}
		return s.publicShareMetadataFromSnapshot(snapshot, secret, unlockProof, auditContext(auditCtx))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, publicShareUnavailable()
	}
	share, document, _, err := s.authorizePublicShareLocked(shareID, secret, unlockProof, true)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	version := s.latestVersionLocked(document.ID, share.BranchID)
	if version == nil {
		return nil, publicShareUnavailable()
	}
	s.auditLocked(ctx, AuditActorAnonymous, "", "document_share.view", "document_share", share.ID, share.ProjectID, share.DocumentID, documentShareAuditMetadata(share, "success", version.ID))
	if err := s.persistLocked(); err != nil {
		return nil, publicShareUnavailable()
	}
	return &PublicShareMetadata{DocumentName: document.Name, DocumentType: document.DocumentType, VersionScope: share.VersionScope, ExpiresAt: copyTimePtr(share.ExpiresAt), CurrentVersion: publicShareVersion(version)}, nil
}

func (s *Store) PublicDocumentShareVersions(shareID, secret, unlockProof string, auditCtx ...AuditContext) ([]PublicShareVersion, error) {
	if snapshot, supported, err := s.loadPersistentPublicShareSnapshot(shareID); supported {
		if err != nil {
			return nil, publicShareUnavailable()
		}
		return s.publicShareVersionsFromSnapshot(snapshot, secret, unlockProof, auditContext(auditCtx))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, publicShareUnavailable()
	}
	share, document, _, err := s.authorizePublicShareLocked(shareID, secret, unlockProof, true)
	if err != nil || share.VersionScope != DocumentShareScopeAllVersions {
		return nil, publicShareUnavailable()
	}
	versions := s.publishedShareVersionsLocked(document.ID, share.BranchID)
	out := make([]PublicShareVersion, 0, len(versions))
	for _, version := range versions {
		out = append(out, publicShareVersion(version))
	}
	s.auditLocked(ctx, AuditActorAnonymous, "", "document_share.view", "document_share", share.ID, share.ProjectID, share.DocumentID, documentShareAuditMetadata(share, "success", ""))
	if err := s.persistLocked(); err != nil {
		return nil, publicShareUnavailable()
	}
	return out, nil
}

func (s *Store) PublicDocumentShareContent(shareID, secret, unlockProof, versionID string, auditCtx ...AuditContext) (*PublicShareContent, error) {
	if snapshot, supported, err := s.loadPersistentPublicShareSnapshot(shareID); supported {
		if err != nil {
			return nil, publicShareUnavailable()
		}
		return s.publicShareContentFromSnapshot(snapshot, secret, unlockProof, versionID, auditContext(auditCtx))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, publicShareUnavailable()
	}
	share, _, _, err := s.authorizePublicShareLocked(shareID, secret, unlockProof, true)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	version := s.allowedPublicShareVersionLocked(share, versionID)
	if version == nil {
		return nil, publicShareUnavailable()
	}
	content, err := s.loadPersistentPublishedContent(version)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	s.auditLocked(ctx, AuditActorAnonymous, "", "document_share.view", "document_share", share.ID, share.ProjectID, share.DocumentID, documentShareAuditMetadata(share, "success", version.ID))
	if err := s.persistLocked(); err != nil {
		return nil, publicShareUnavailable()
	}
	return &PublicShareContent{VersionID: version.ID, Content: content}, nil
}

func (s *Store) PublicDocumentShareDownload(shareID, secret, unlockProof, versionID string, auditCtx ...AuditContext) (*PublicShareDownload, error) {
	if snapshot, supported, err := s.loadPersistentPublicShareSnapshot(shareID); supported {
		if err != nil {
			return nil, publicShareUnavailable()
		}
		return s.publicShareDownloadFromSnapshot(snapshot, secret, unlockProof, versionID, auditContext(auditCtx))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, publicShareUnavailable()
	}
	share, document, _, err := s.authorizePublicShareLocked(shareID, secret, unlockProof, true)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	version := s.allowedPublicShareVersionLocked(share, versionID)
	if version == nil {
		return nil, publicShareUnavailable()
	}
	content, err := s.loadPersistentPublishedContent(version)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	filename, contentType := publicShareDownloadMetadata(document, version, content)
	s.auditLocked(ctx, AuditActorAnonymous, "", "document_share.download", "document_share", share.ID, share.ProjectID, share.DocumentID, documentShareAuditMetadata(share, "success", version.ID))
	if err := s.persistLocked(); err != nil {
		return nil, publicShareUnavailable()
	}
	return &PublicShareDownload{VersionID: version.ID, Filename: filename, ContentType: contentType, Body: []byte(content)}, nil
}

func (s *Store) authorizePublicShareLocked(shareID, secret, unlockProof string, requireUnlock bool) (*DocumentShare, *APIService, *ContractBranch, error) {
	share := s.shares[shareID]
	if share == nil || !encryption.VerifyDocumentShareCapability(secret, share.TokenHash) {
		return nil, nil, nil, publicShareUnavailable()
	}
	if err := domainshare.EnsureRevealable(share, time.Now().UTC()); err != nil {
		return nil, nil, nil, publicShareUnavailable()
	}
	document, branch, err := s.activeShareParentsLocked(share.ProjectID, share.DocumentID, share.BranchID)
	if err != nil {
		return nil, nil, nil, publicShareUnavailable()
	}
	if requireUnlock && share.PasswordProtected() && authentication.ValidateDocumentShareUnlockProof(unlockProof, share.ID, time.Now().UTC()) != nil {
		return nil, nil, nil, publicShareUnavailable()
	}
	return share, document, branch, nil
}

func (s *Store) activeShareParentsLocked(projectID, documentID, branchID string) (*APIService, *ContractBranch, error) {
	project := s.projects[projectID]
	document := s.apiServices[documentID]
	branch := s.branches[branchID]
	if project == nil || document == nil || document.ProjectID != projectID || branch == nil || branch.DocumentID != documentID {
		return nil, nil, ErrNotFound
	}
	if project.Status != ProjectStatusActive || document.Status != DocumentStatusActive || branch.Status != BranchStatusActive {
		return nil, nil, ErrFailedPrecondition
	}
	return document, branch, nil
}

func (s *Store) managedDocumentShareLocked(projectID, documentID, shareID string) (*DocumentShare, error) {
	share := s.shares[shareID]
	if share == nil || share.ProjectID != projectID || share.DocumentID != documentID {
		return nil, ErrNotFound
	}
	return share, nil
}

func (s *Store) publishedShareVersionsLocked(documentID, branchID string) []*ContractVersion {
	versions := make([]*ContractVersion, 0)
	for _, version := range s.versions {
		if version.ServiceID == documentID && version.BranchID == branchID && version.Status == VersionStatusPublished {
			versions = append(versions, version)
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].PublishedAt.Equal(versions[j].PublishedAt) {
			return versions[i].ID > versions[j].ID
		}
		return versions[i].PublishedAt.After(versions[j].PublishedAt)
	})
	return versions
}

func (s *Store) allowedPublicShareVersionLocked(share *DocumentShare, versionID string) *ContractVersion {
	version := s.versions[versionID]
	if version == nil || version.ServiceID != share.DocumentID || version.BranchID != share.BranchID || version.Status != VersionStatusPublished {
		return nil
	}
	if share.VersionScope == DocumentShareScopeLatest {
		latest := s.latestVersionLocked(share.DocumentID, share.BranchID)
		if latest == nil || latest.ID != version.ID {
			return nil
		}
	}
	return version
}

func publicShareVersion(version *ContractVersion) PublicShareVersion {
	return PublicShareVersion{ID: version.ID, VersionName: version.VersionName, Changelog: version.Changelog, PublishedAt: version.PublishedAt}
}

func publicShareUnavailable() error {
	return fmt.Errorf("%w: public document unavailable", ErrNotFound)
}

func documentShareAuditMetadata(share *DocumentShare, result, versionID string) map[string]string {
	metadata := auditMetadata(
		"result", result,
		"branch_id", share.BranchID,
		"version_scope", fmt.Sprint(share.VersionScope),
		"password_protected", fmt.Sprint(share.PasswordProtected()),
		"status", fmt.Sprint(share.Status),
		"expires_at", timePtrString(share.ExpiresAt),
	)
	if versionID != "" {
		metadata["version_id"] = versionID
	}
	return metadata
}

func (s *Store) loadPersistentPublicShareSnapshot(shareID string) (*domainvdoc.PublicDocumentShareSnapshot, bool, error) {
	if s.persistence == nil {
		return nil, false, nil
	}
	return s.persistence.loadPublicDocumentShareSnapshot(context.Background(), shareID)
}

func unlockPersistentPublicShare(share *DocumentShare, password string) (string, time.Time, error) {
	if !verifyPublicSharePassword(share, password) {
		return "", time.Time{}, publicShareUnavailable()
	}
	proof, expiresAt, err := authentication.SignDocumentShareUnlockProof(share.ID, time.Now().UTC(), share.ExpiresAt)
	if err != nil {
		return "", time.Time{}, publicShareUnavailable()
	}
	return proof, expiresAt, nil
}

func authorizePublicShareSnapshot(snapshot *domainvdoc.PublicDocumentShareSnapshot, secret, unlockProof string, requireUnlock bool) (*DocumentShare, *APIService, *ContractBranch, error) {
	if snapshot == nil || snapshot.Share == nil || snapshot.Project == nil || snapshot.Document == nil || snapshot.Branch == nil {
		return nil, nil, nil, publicShareUnavailable()
	}
	share := snapshot.Share
	if !encryption.VerifyDocumentShareCapability(secret, share.TokenHash) || domainshare.EnsureRevealable(share, time.Now().UTC()) != nil {
		return nil, nil, nil, publicShareUnavailable()
	}
	project, document, branch := snapshot.Project, snapshot.Document, snapshot.Branch
	if project.ID != share.ProjectID || project.Status != ProjectStatusActive ||
		document.ID != share.DocumentID || document.ProjectID != project.ID || document.Status != DocumentStatusActive ||
		branch.ID != share.BranchID || branch.DocumentID != document.ID || branch.Status != BranchStatusActive {
		return nil, nil, nil, publicShareUnavailable()
	}
	if requireUnlock && share.PasswordProtected() && authentication.ValidateDocumentShareUnlockProof(unlockProof, share.ID, time.Now().UTC()) != nil {
		return nil, nil, nil, publicShareUnavailable()
	}
	return share, document, branch, nil
}

func publishedSnapshotVersions(snapshot *domainvdoc.PublicDocumentShareSnapshot, share *DocumentShare) []*ContractVersion {
	versions := make([]*ContractVersion, 0, len(snapshot.Versions))
	for _, version := range snapshot.Versions {
		if version != nil && version.ServiceID == share.DocumentID && version.BranchID == share.BranchID && version.Status == VersionStatusPublished {
			versions = append(versions, version)
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].PublishedAt.Equal(versions[j].PublishedAt) {
			return versions[i].ID > versions[j].ID
		}
		return versions[i].PublishedAt.After(versions[j].PublishedAt)
	})
	return versions
}

func allowedSnapshotVersion(snapshot *domainvdoc.PublicDocumentShareSnapshot, share *DocumentShare, versionID string) *ContractVersion {
	versions := publishedSnapshotVersions(snapshot, share)
	for index, version := range versions {
		if version.ID == versionID && (share.VersionScope == DocumentShareScopeAllVersions || index == 0) {
			return version
		}
	}
	return nil
}

func (s *Store) recordPersistentPublicShareAudit(ctx AuditContext, share *DocumentShare, action, versionID string) error {
	audits := make(map[string]*AuditLog, 1)
	audit := appendAuditToState(audits, ctx, AuditActorAnonymous, "", action, "document_share", share.ID, share.ProjectID, share.DocumentID, documentShareAuditMetadata(share, "success", versionID))
	return s.persistence.recordPublicDocumentShareAccess(context.Background(), share.ID, audit)
}

func (s *Store) publicShareMetadataFromSnapshot(snapshot *domainvdoc.PublicDocumentShareSnapshot, secret, unlockProof string, ctx AuditContext) (*PublicShareMetadata, error) {
	share, document, _, err := authorizePublicShareSnapshot(snapshot, secret, unlockProof, true)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	versions := publishedSnapshotVersions(snapshot, share)
	if len(versions) == 0 {
		return nil, publicShareUnavailable()
	}
	if err := s.recordPersistentPublicShareAudit(ctx, share, "document_share.view", versions[0].ID); err != nil {
		return nil, publicShareUnavailable()
	}
	return &PublicShareMetadata{DocumentName: document.Name, DocumentType: document.DocumentType, VersionScope: share.VersionScope, ExpiresAt: copyTimePtr(share.ExpiresAt), CurrentVersion: publicShareVersion(versions[0])}, nil
}

func (s *Store) publicShareVersionsFromSnapshot(snapshot *domainvdoc.PublicDocumentShareSnapshot, secret, unlockProof string, ctx AuditContext) ([]PublicShareVersion, error) {
	share, _, _, err := authorizePublicShareSnapshot(snapshot, secret, unlockProof, true)
	if err != nil || share.VersionScope != DocumentShareScopeAllVersions {
		return nil, publicShareUnavailable()
	}
	versions := publishedSnapshotVersions(snapshot, share)
	out := make([]PublicShareVersion, 0, len(versions))
	for _, version := range versions {
		out = append(out, publicShareVersion(version))
	}
	if err := s.recordPersistentPublicShareAudit(ctx, share, "document_share.view", ""); err != nil {
		return nil, publicShareUnavailable()
	}
	return out, nil
}

func (s *Store) loadPersistentPublishedContent(version *ContractVersion) (string, error) {
	if version.RawSchema != "" {
		if err := validateStoredObjectBody(version.RawSchemaObjectKey, version.RawSchemaHash, []byte(version.RawSchema)); err != nil {
			return "", publicShareUnavailable()
		}
		return version.RawSchema, nil
	}
	if s.objects == nil || strings.TrimSpace(version.RawSchemaObjectKey) == "" {
		return "", publicShareUnavailable()
	}
	body, err := s.readVerifiedObject(context.Background(), version.RawSchemaObjectKey, version.RawSchemaHash)
	if err != nil {
		return "", publicShareUnavailable()
	}
	return string(body), nil
}

func (s *Store) publicShareContentFromSnapshot(snapshot *domainvdoc.PublicDocumentShareSnapshot, secret, unlockProof, versionID string, ctx AuditContext) (*PublicShareContent, error) {
	share, _, _, err := authorizePublicShareSnapshot(snapshot, secret, unlockProof, true)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	version := allowedSnapshotVersion(snapshot, share, versionID)
	if version == nil {
		return nil, publicShareUnavailable()
	}
	content, err := s.loadPersistentPublishedContent(version)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	if err := s.recordPersistentPublicShareAudit(ctx, share, "document_share.view", version.ID); err != nil {
		return nil, publicShareUnavailable()
	}
	return &PublicShareContent{VersionID: version.ID, Content: content}, nil
}

func (s *Store) publicShareDownloadFromSnapshot(snapshot *domainvdoc.PublicDocumentShareSnapshot, secret, unlockProof, versionID string, ctx AuditContext) (*PublicShareDownload, error) {
	share, document, _, err := authorizePublicShareSnapshot(snapshot, secret, unlockProof, true)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	version := allowedSnapshotVersion(snapshot, share, versionID)
	if version == nil {
		return nil, publicShareUnavailable()
	}
	content, err := s.loadPersistentPublishedContent(version)
	if err != nil {
		return nil, publicShareUnavailable()
	}
	filename, contentType := publicShareDownloadMetadata(document, version, content)
	if err := s.recordPersistentPublicShareAudit(ctx, share, "document_share.download", version.ID); err != nil {
		return nil, publicShareUnavailable()
	}
	return &PublicShareDownload{VersionID: version.ID, Filename: filename, ContentType: contentType, Body: []byte(content)}, nil
}

func verifyPublicSharePassword(share *DocumentShare, password string) bool {
	parsed, err := domainshare.ParsePassword(password)
	if err != nil || share == nil || share.PasswordVerifier == nil {
		consumeDummyPublicSharePasswordCheck(password)
		return false
	}
	return encryption.VerifyBcryptPasswordBytes(parsed.Bytes(), *share.PasswordVerifier)
}

func consumeDummyPublicSharePasswordCheck(password string) {
	value := []byte(password)
	if len(value) > maxUserPasswordBytes {
		value = value[:maxUserPasswordBytes]
	}
	_ = encryption.VerifyBcryptPasswordBytes(value, dummyLoginPasswordHash)
}

func publicShareDownloadMetadata(document *APIService, version *ContractVersion, content string) (string, string) {
	contentType := "application/yaml; charset=utf-8"
	fallback := "document.yaml"
	if document.DocumentType == DocumentTypeMarkdown {
		contentType = "text/markdown; charset=utf-8"
		fallback = "document.md"
	} else if strings.HasPrefix(strings.TrimSpace(content), "{") {
		contentType = "application/json; charset=utf-8"
		fallback = "document.json"
	}
	candidate := path.Base(firstNonEmpty(firstNonEmpty(version.RelativePath, document.RelativePath), document.Name))
	filename := strings.TrimSpace(strings.Map(func(character rune) rune {
		if character == '/' || character == '\\' || unicode.IsControl(character) {
			return -1
		}
		return character
	}, candidate))
	if filename == "" || filename == "." || filename == ".." {
		return fallback, contentType
	}
	const maxFilenameRunes = 180
	characters := []rune(filename)
	if len(characters) > maxFilenameRunes {
		extension := []rune(path.Ext(filename))
		if len(extension) > 32 {
			extension = nil
		}
		stem := []rune(strings.TrimSuffix(filename, string(extension)))
		stem = stem[:maxFilenameRunes-len(extension)]
		filename = string(stem) + string(extension)
	}
	return filename, contentType
}
