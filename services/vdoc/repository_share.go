package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"time"

	repo "vdoc/domain/documentrepo"
	"vdoc/domain/documentshare"
	domain "vdoc/domain/vdoc"
	"vdoc/utils/authentication"
	"vdoc/utils/encryption"
	"vdoc/utils/id"
)

const repositoryShareKind = "release_share"

var repositoryShareIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type repositoryStoredShare struct {
	repo.Share
	Capability       encryption.DocumentShareCapabilityRecord `json:"capability"`
	PasswordVerifier *string                                  `json:"password_verifier,omitempty"`
}

type RepositoryShareSecret struct {
	Share  repo.Share
	Secret string
}

func (m *repositoryMemory) PutRepositoryShareIndex(context.Context, string, string) error { return nil }
func (m *repositoryMemory) RepositoryShareProject(_ context.Context, id string) (string, bool, error) {
	for _, record := range m.records {
		if record.Kind == repositoryShareKind && record.ID == id {
			return record.ProjectID, true, nil
		}
	}
	return "", false, ErrNotFound
}

func (s *Store) repositoryShareRelease(storage repo.Storage, project, releaseID, documentID string) (*repo.Release, repo.File, error) {
	release, err := repositoryLoad[repo.Release](s.requestContext(), storage, project, "release", releaseID)
	if err != nil {
		return nil, repo.File{}, err
	}
	if release.ProjectID != project || release.ID != releaseID || release.PublishedAt.IsZero() {
		return nil, repo.File{}, ErrNotFound
	}
	commit, err := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", release.CommitID)
	if err != nil {
		return nil, repo.File{}, err
	}
	file, ok := fileByID(commit.Files, documentID)
	if !ok || commit.ProjectID != project || commit.ID != release.CommitID {
		return nil, repo.File{}, ErrNotFound
	}
	return release, file, nil
}

func (s *Store) CreateRepositoryShare(actor, project, releaseID, documentID string, input repo.ShareInput, audit ...AuditContext) (out *RepositoryShareSecret, err error) {
	if mcpMutationTokenID(s.requestContext()) != "" {
		return nil, ErrPermissionDenied
	}
	now := time.Now().UTC()
	expires, err := repo.ShareExpiry(input.ExpiryPreset, now)
	if err != nil {
		return nil, err
	}
	var verifier *string
	if input.Password != "" {
		// 昂贵的 bcrypt 之前先验证权限，完成后写事务仍重新检查当前权限。
		if e := s.WithRepositoryWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
			_, _, e := s.repositoryShareRelease(storage, project, releaseID, documentID)
			return e
		}); e != nil {
			return nil, e
		}
		password, e := documentshare.ParsePassword(input.Password)
		if e != nil {
			return nil, e
		}
		s.mu.RLock()
		hash := s.hashPassword
		s.mu.RUnlock()
		if hash == nil {
			hash = encryption.HashPasswordBytesWithBcrypt
		}
		encoded, e := hash(password.Bytes())
		if e != nil {
			return nil, e
		}
		verifier = &encoded
	}
	err = s.WithRepositoryWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		if _, _, e := s.repositoryShareRelease(storage, project, releaseID, documentID); e != nil {
			return e
		}
		index, ok := storage.(repo.ShareIndex)
		if !ok {
			return fmt.Errorf("%w: repository share index unavailable", ErrFailedPrecondition)
		}
		shareID := id.GenerateID()
		if s.shares[shareID] != nil {
			return ErrAlreadyExists
		}
		if _, _, e := index.RepositoryShareProject(s.requestContext(), shareID); e == nil {
			return ErrAlreadyExists
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		secret, capability, e := encryption.GenerateDocumentShareCapability(shareID, s.cipherKeyring)
		if e != nil {
			return e
		}
		value := repositoryStoredShare{Share: repo.Share{ID: shareID, ProjectID: project, ReleaseID: releaseID, DocumentID: documentID, Status: DocumentShareStatusActive, PasswordProtected: verifier != nil, ExpiresAt: expires, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}, Capability: capability, PasswordVerifier: verifier}
		if e = index.PutRepositoryShareIndex(s.requestContext(), shareID, project); e != nil {
			return e
		}
		if e = repositoryPut(s.requestContext(), storage, project, repositoryShareKind, shareID, value, 1, nil); e != nil {
			return e
		}
		if e = s.repositoryShareAudit(storage, value.Share, actor, "repository_share.create", auditContext(audit)); e != nil {
			return e
		}
		out = &RepositoryShareSecret{Share: value.Share, Secret: secret}
		return nil
	})
	return
}

func (s *Store) ListRepositoryShares(actor, project, releaseID, documentID string) (out []repo.Share, err error) {
	if mcpMutationTokenID(s.requestContext()) != "" {
		return nil, ErrPermissionDenied
	}
	err = s.WithRepositoryWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		if _, _, e := s.repositoryShareRelease(storage, project, releaseID, documentID); e != nil {
			return e
		}
		values, e := repositoryList[repositoryStoredShare](s.requestContext(), storage, project, repositoryShareKind)
		if e != nil {
			return e
		}
		out = []repo.Share{}
		for _, value := range values {
			if value.ReleaseID == releaseID && value.DocumentID == documentID {
				out = append(out, value.Share)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
		return nil
	})
	return
}

func repositoryManagedShare(ctx context.Context, storage repo.Storage, project, releaseID, documentID, shareID string) (*repositoryStoredShare, *repo.Record, error) {
	record, err := storage.RepositoryRecord(ctx, project, repositoryShareKind, shareID)
	if err != nil {
		return nil, nil, err
	}
	var value repositoryStoredShare
	if err = json.Unmarshal(record.Payload, &value); err != nil {
		return nil, nil, err
	}
	if value.ProjectID != project || value.ID != shareID || value.ReleaseID != releaseID || value.DocumentID != documentID {
		return nil, nil, ErrNotFound
	}
	return &value, record, nil
}

func (s *Store) RevealRepositoryShare(actor, project, releaseID, documentID, shareID string, audit ...AuditContext) (out *RepositoryShareSecret, err error) {
	if mcpMutationTokenID(s.requestContext()) != "" {
		return nil, ErrPermissionDenied
	}
	err = s.WithRepositoryWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		value, _, e := repositoryManagedShare(s.requestContext(), storage, project, releaseID, documentID, shareID)
		if e != nil {
			return e
		}
		if !value.Active(time.Now()) {
			return ErrFailedPrecondition
		}
		if _, _, e = s.repositoryShareRelease(storage, project, releaseID, documentID); e != nil {
			return e
		}
		secret, e := encryption.RevealDocumentShareCapability(value.ID, s.cipherKeyring, value.Capability)
		if e != nil {
			return e
		}
		if e = s.repositoryShareAudit(storage, value.Share, actor, "repository_share.reveal", auditContext(audit)); e != nil {
			return e
		}
		out = &RepositoryShareSecret{Share: value.Share, Secret: secret}
		return nil
	})
	return
}

func (s *Store) RevokeRepositoryShare(actor, project, releaseID, documentID, shareID string, audit ...AuditContext) (out *repo.Share, err error) {
	if mcpMutationTokenID(s.requestContext()) != "" {
		return nil, ErrPermissionDenied
	}
	err = s.WithRepositoryWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		value, record, e := repositoryManagedShare(s.requestContext(), storage, project, releaseID, documentID, shareID)
		if e != nil {
			return e
		}
		if value.Status == DocumentShareStatusRevoked {
			out = &value.Share
			return nil
		}
		now := time.Now().UTC()
		value.Status = DocumentShareStatusRevoked
		value.RevokedBy = actor
		value.RevokedAt = &now
		value.UpdatedAt = now
		if e = repositoryPut(s.requestContext(), storage, project, repositoryShareKind, shareID, value, record.Generation+1, &record.Generation); e != nil {
			return e
		}
		if e = s.repositoryShareAudit(storage, value.Share, actor, "repository_share.revoke", auditContext(audit)); e != nil {
			return e
		}
		out = &value.Share
		return nil
	})
	return
}

func (s *Store) repositoryShareAudit(storage repo.Storage, share repo.Share, actor, action string, ctx AuditContext) error {
	actorType := AuditActorUser
	if actor == "" {
		actorType = AuditActorAnonymous
	}
	audits := map[string]*AuditLog{}
	// 新文件不依赖旧 documents 表；在元数据中记录身份，避免旧审计外键制造伪文档。
	value := appendAuditToState(audits, ctx, actorType, actor, action, "repository_share", share.ID, share.ProjectID, "", map[string]string{"document_id": share.DocumentID, "release_id": share.ReleaseID, "version_scope": "3", "password_protected": fmt.Sprint(share.PasswordProtected)})
	if database, ok := storage.(interface {
		RecordAudit(context.Context, *AuditLog) error
	}); ok {
		return database.RecordAudit(s.requestContext(), value)
	}
	s.audits[value.ID] = value
	return nil
}

// IsRepositoryShare 只用于内部路由分派，结果绝不直接返回匿名调用方。
func (s *Store) IsRepositoryShare(shareID string) (bool, error) {
	if !repositoryShareIDPattern.MatchString(shareID) {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	storage := s.repositoryStorageLocked()
	index, ok := storage.(repo.ShareIndex)
	if !ok {
		return false, nil
	}
	_, _, err := index.RepositoryShareProject(s.requestContext(), shareID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) withPublicRepositoryShare(shareID, secret, proof string, requireUnlock bool, fn func(repo.Storage, *repositoryStoredShare, *repo.Release, repo.File) error) error {
	if !repositoryShareIDPattern.MatchString(shareID) {
		return publicShareUnavailable()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	read := func(storage repo.Storage) error {
		index, ok := storage.(repo.ShareIndex)
		if !ok {
			return publicShareUnavailable()
		}
		project, active, err := index.RepositoryShareProject(s.requestContext(), shareID)
		if err != nil || !active {
			return publicShareUnavailable()
		}
		if s.persistence == nil {
			p := s.projects[project]
			if p == nil || p.Status != ProjectStatusActive {
				return publicShareUnavailable()
			}
		}
		if err = storage.LockRepository(s.requestContext(), project); err != nil {
			return publicShareUnavailable()
		}
		value, err := repositoryLoad[repositoryStoredShare](s.requestContext(), storage, project, repositoryShareKind, shareID)
		if err != nil || value.ID != shareID || value.ProjectID != project || value.PasswordProtected != (value.PasswordVerifier != nil) || !value.Active(time.Now()) || !encryption.VerifyDocumentShareCapability(secret, value.Capability.Hash) {
			return publicShareUnavailable()
		}
		release, file, err := s.repositoryShareRelease(storage, project, value.ReleaseID, value.DocumentID)
		if err != nil {
			return publicShareUnavailable()
		}
		if requireUnlock && value.PasswordProtected && authentication.ValidateDocumentShareUnlockProof(proof, shareID, time.Now().UTC()) != nil {
			return ErrPublicSharePasswordRequired
		}
		return fn(storage, value, release, file)
	}
	if s.persistence == nil {
		return read(s.repositoryStorageLocked())
	}
	transaction, ok := s.persistence.repo.(transactionalRepository)
	if !ok {
		return publicShareUnavailable()
	}
	err := transaction.WithinTransaction(s.requestContext(), func(database domain.Repository) error {
		storage, ok := database.(repo.Storage)
		if !ok {
			return publicShareUnavailable()
		}
		return read(storage)
	})
	if errors.Is(err, ErrPublicSharePasswordRequired) {
		return err
	}
	if err != nil {
		return publicShareUnavailable()
	}
	return nil
}

func repositoryPublicVersion(release *repo.Release) PublicShareVersion {
	return PublicShareVersion{ID: release.ID, VersionName: release.VersionName, Changelog: release.Message, PublishedAt: release.PublishedAt}
}

func (s *Store) PublicRepositoryShareMetadata(shareID, secret, proof string, audit ...AuditContext) (out *PublicShareMetadata, err error) {
	err = s.withPublicRepositoryShare(shareID, secret, proof, true, func(storage repo.Storage, share *repositoryStoredShare, release *repo.Release, file repo.File) error {
		out = &PublicShareMetadata{DocumentName: file.Name, DocumentType: file.DocumentType, VersionScope: DocumentShareScopeFixedVersion, ExpiresAt: share.ExpiresAt, CurrentVersion: repositoryPublicVersion(release)}
		return s.repositoryShareAudit(storage, share.Share, "", "repository_share.view", auditContext(audit))
	})
	return
}

func (s *Store) PublicRepositoryShareContent(shareID, secret, proof, versionID string, audit ...AuditContext) (out *PublicShareContent, err error) {
	err = s.withPublicRepositoryShare(shareID, secret, proof, true, func(storage repo.Storage, share *repositoryStoredShare, release *repo.Release, file repo.File) error {
		if versionID != release.ID {
			return publicShareUnavailable()
		}
		body, e := s.repositoryFileContent(file)
		if e != nil {
			return publicShareUnavailable()
		}
		out = &PublicShareContent{VersionID: release.ID, Content: body}
		return s.repositoryShareAudit(storage, share.Share, "", "repository_share.view", auditContext(audit))
	})
	return
}

func (s *Store) PublicRepositoryShareDownload(shareID, secret, proof, versionID string, audit ...AuditContext) (out *PublicShareDownload, err error) {
	err = s.withPublicRepositoryShare(shareID, secret, proof, true, func(storage repo.Storage, share *repositoryStoredShare, release *repo.Release, file repo.File) error {
		if versionID != release.ID {
			return publicShareUnavailable()
		}
		body, e := s.repositoryFileContent(file)
		if e != nil {
			return publicShareUnavailable()
		}
		filename, contentType := publicShareDownloadMetadata(&APIService{Name: file.Name, DocumentType: file.DocumentType, RelativePath: file.RelativePath}, &ContractVersion{RelativePath: file.RelativePath}, body)
		out = &PublicShareDownload{VersionID: release.ID, Filename: filename, ContentType: contentType, Body: []byte(body)}
		return s.repositoryShareAudit(storage, share.Share, "", "repository_share.download", auditContext(audit))
	})
	return
}

func (s *Store) UnlockPublicRepositoryShare(shareID, secret, password string, audit ...AuditContext) (proof string, expires time.Time, err error) {
	var verifier string
	err = s.withPublicRepositoryShare(shareID, secret, "", false, func(_ repo.Storage, share *repositoryStoredShare, _ *repo.Release, _ repo.File) error {
		if share.PasswordVerifier == nil || !share.PasswordProtected {
			return publicShareUnavailable()
		}
		verifier = *share.PasswordVerifier
		return nil
	})
	if err != nil {
		consumeDummyPublicSharePasswordCheck(password)
		return "", time.Time{}, publicShareUnavailable()
	}
	parsed, e := documentshare.ParsePassword(password)
	if e != nil {
		consumeDummyPublicSharePasswordCheck(password)
		return "", time.Time{}, publicShareUnavailable()
	}
	if !encryption.VerifyBcryptPasswordBytes(parsed.Bytes(), verifier) {
		return "", time.Time{}, publicShareUnavailable()
	}
	err = s.withPublicRepositoryShare(shareID, secret, "", false, func(storage repo.Storage, share *repositoryStoredShare, _ *repo.Release, _ repo.File) error {
		if share.PasswordVerifier == nil || *share.PasswordVerifier != verifier {
			return publicShareUnavailable()
		}
		var e error
		proof, expires, e = authentication.SignDocumentShareUnlockProof(shareID, time.Now().UTC(), share.ExpiresAt)
		if e != nil {
			return publicShareUnavailable()
		}
		return s.repositoryShareAudit(storage, share.Share, "", "repository_share.unlock", auditContext(audit))
	})
	return
}

// InitRepositoryShares 在单个事务中验证所有密文再旋转，缺失历史密钥时拒绝启动。
func (s *Store) InitRepositoryShares() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persistence != nil {
		if _, supported := s.persistence.repo.(repo.Storage); !supported {
			return nil
		}
	}
	rotate := func(storage repo.Storage) error {
		projects := make([]string, 0, len(s.projects))
		for project := range s.projects {
			projects = append(projects, project)
		}
		slices.Sort(projects)
		for _, project := range projects {
			if err := storage.LockRepository(s.requestContext(), project); err != nil {
				return err
			}
			var after int64
			for {
				records, err := storage.RepositoryRecords(s.requestContext(), project, repositoryShareKind, after, 500)
				if err != nil {
					return err
				}
				for _, record := range records {
					var value repositoryStoredShare
					if err = json.Unmarshal(record.Payload, &value); err != nil {
						return err
					}
					if _, err = encryption.RevealDocumentShareCapability(value.ID, s.cipherKeyring, value.Capability); err != nil {
						return fmt.Errorf("validate repository share %s ciphertext: %w", value.ID, err)
					}
					if value.Capability.KID != s.cipherKeyring.ActiveKID() {
						value.Capability, err = encryption.ReencryptDocumentShareCapability(value.ID, s.cipherKeyring, value.Capability)
						if err != nil {
							return err
						}
						if err = repositoryPut(s.requestContext(), storage, project, repositoryShareKind, value.ID, value, record.Generation+1, &record.Generation); err != nil {
							return err
						}
					}
					after = record.Sequence
				}
				if len(records) < 500 {
					break
				}
			}
		}
		return nil
	}
	if s.persistence == nil {
		before := s.repositoryStorageLocked().(*repositoryMemory)
		s.repoMemory = before.clone()
		if err := rotate(s.repoMemory); err != nil {
			s.repoMemory = before
			return err
		}
		return nil
	}
	transaction, ok := s.persistence.repo.(transactionalRepository)
	if !ok {
		return fmt.Errorf("repository share rotation requires transactions")
	}
	return transaction.WithinTransaction(s.requestContext(), func(database domain.Repository) error {
		storage, ok := database.(repo.Storage)
		if !ok {
			return fmt.Errorf("repository share storage unavailable")
		}
		return rotate(storage)
	})
}
