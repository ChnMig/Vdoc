package vdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"vdoc/domain/documentimage"
	repo "vdoc/domain/documentrepo"
	"vdoc/utils/id"
)

const documentImageKind = "document_image"

type storedDocumentImage struct {
	documentimage.Asset
	ProjectID      string    `json:"project_id"`
	DocumentID     string    `json:"document_id"`
	ObjectKey      string    `json:"object_key"`
	CreatedAt      time.Time `json:"created_at"`
	LastUploadedAt time.Time `json:"last_uploaded_at"`
}

func documentImageID(document, asset string) string { return document + ":" + asset }

func (s *Store) requireImageDocument(storage repo.Storage, project, document string) error {
	if document == "" {
		return ErrInvalidArgument
	}
	documents, err := s.repositoryDocumentsLocked(storage, project)
	if err != nil {
		return err
	}
	for _, value := range documents {
		if value.ID == document && value.Status == DocumentStatusActive {
			if value.DocumentType != DocumentTypeMarkdown {
				return fmt.Errorf("%w: images require a Markdown document", ErrInvalidArgument)
			}
			return s.repositoryRequireActiveDocument(storage, project, document)
		}
	}
	return ErrNotFound
}

func (s *Store) UploadDocumentImage(actor, project, document, filename, encoded string) (out *documentimage.Asset, err error) {
	s = s.WithRepositoryDocument("")
	keys := []string{}
	err = s.WithRepositoryWrite(actor, project, MemberRoleWriter, func(storage repo.Storage) error {
		if e := s.requireImageDocument(storage, project, document); e != nil {
			return e
		}
		asset, body, e := documentimage.Validate(filename, encoded)
		if e != nil {
			return fmt.Errorf("%w: %v", ErrInvalidArgument, e)
		}
		key := documentImageID(document, asset.AssetID)
		generation := int64(1)
		var expected *int64
		record, e := storage.RepositoryRecord(s.requestContext(), project, documentImageKind, key)
		if e == nil {
			var previous storedDocumentImage
			if e = json.Unmarshal(record.Payload, &previous); e != nil {
				return e
			}
			if previous.ProjectID != project || previous.DocumentID != document || previous.AssetID != asset.AssetID {
				return ErrNotFound
			}
			generation, expected = record.Generation+1, &record.Generation
			if previous.ObjectKey != "" {
				previous.LastUploadedAt = time.Now().UTC()
				if e = repositoryPut(s.requestContext(), storage, project, documentImageKind, key, previous, generation, expected); e != nil {
					return e
				}
				out = &previous.Asset
				return nil
			}
		}
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		if e = validateStoredObjectWriteSize(len(body)); e != nil {
			return e
		}
		objectKey := fmt.Sprintf("projects/%s/documents/%s/images/%s/%s", project, document, id.GenerateID(), asset.AssetID)
		keys = append(keys, objectKey)
		if s.objects != nil {
			if _, e = s.objects.PutObject(s.requestContext(), ObjectWrite{Key: objectKey, ContentType: asset.ContentType, Body: body, Metadata: map[string]string{"project_id": project, "document_id": document, "sha256": asset.AssetID, "owner_type": "document_image"}}); e != nil {
				return e
			}
		} else if s.persistence != nil {
			return fmt.Errorf("object storage is required for persistent document images")
		} else {
			s.repoMemory.blobs[objectKey] = slices.Clone(body)
		}
		now := time.Now().UTC()
		value := storedDocumentImage{Asset: asset, ProjectID: project, DocumentID: document, ObjectKey: objectKey, CreatedAt: now, LastUploadedAt: now}
		if e = repositoryPut(s.requestContext(), storage, project, documentImageKind, key, value, generation, expected); e != nil {
			return e
		}
		out = &asset
		return nil
	})
	if err != nil {
		err = s.repositoryCleanupUploads(err, keys)
		out = nil
	}
	return
}

func (s *Store) imageContent(storage repo.Storage, project, document, assetID string) (*documentimage.Asset, []byte, error) {
	if !documentimage.ValidID(assetID) {
		return nil, nil, ErrNotFound
	}
	value, err := repositoryLoad[storedDocumentImage](s.requestContext(), storage, project, documentImageKind, documentImageID(document, assetID))
	if err != nil {
		return nil, nil, err
	}
	if value.ProjectID != project || value.DocumentID != document || value.AssetID != assetID || value.ObjectKey == "" || value.Reference != documentimage.ReferencePrefix+assetID || value.ByteLength <= 0 || value.ByteLength > documentimage.MaxBytes {
		return nil, nil, ErrNotFound
	}
	var body []byte
	if s.objects != nil {
		body, err = s.readVerifiedObject(s.requestContext(), value.ObjectKey, assetID)
	} else if s.repoMemory != nil {
		body = slices.Clone(s.repoMemory.blobs[value.ObjectKey])
	}
	if err != nil || len(body) != value.ByteLength || repo.Hash(body) != assetID {
		return nil, nil, ErrNotFound
	}
	return &value.Asset, body, nil
}

func (s *Store) DocumentImage(actor, project, document, assetID string) (asset *documentimage.Asset, body []byte, err error) {
	s = s.WithRepositoryDocument("")
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		if e := s.requireImageDocument(storage, project, document); e != nil {
			return e
		}
		asset, body, err = s.imageContent(storage, project, document, assetID)
		return err
	})
	return
}

func (s *Store) PublicRepositoryShareImage(shareID, secret, proof, versionID, assetID string) (asset *documentimage.Asset, body []byte, err error) {
	err = s.withPublicRepositoryShare(shareID, secret, proof, true, func(storage repo.Storage, share *repositoryStoredShare, release *repo.Release, file repo.File) error {
		if versionID != release.ID || file.DocumentType != DocumentTypeMarkdown {
			return publicShareUnavailable()
		}
		content, e := s.repositoryFileContent(file)
		if e != nil || !documentimage.Referenced(content, assetID) {
			return publicShareUnavailable()
		}
		asset, body, e = s.imageContent(storage, share.ProjectID, share.DocumentID, assetID)
		return e
	})
	return
}

// 兼容旧版分享：同样只授权选中的已发布正文，不因图片功能放宽版本范围。
func (s *Store) PublicDocumentShareImage(shareID, secret, proof, versionID, assetID string) (asset *documentimage.Asset, body []byte, err error) {
	if snapshot, supported, e := s.loadPersistentPublicShareSnapshot(shareID); supported {
		if e != nil {
			return nil, nil, publicShareUnavailable()
		}
		share, document, _, e := authorizePublicShareSnapshot(snapshot, secret, proof, true)
		if e != nil {
			return nil, nil, publicShareAccessError(e)
		}
		version := allowedSnapshotVersion(snapshot, share, versionID)
		if document.DocumentType != DocumentTypeMarkdown || version == nil {
			return nil, nil, publicShareUnavailable()
		}
		content, e := s.loadPersistentPublishedContent(version)
		if e != nil || !documentimage.Referenced(content, assetID) {
			return nil, nil, publicShareUnavailable()
		}
		s.mu.Lock()
		asset, body, e = s.imageContent(s.repositoryStorageLocked(), share.ProjectID, share.DocumentID, assetID)
		s.mu.Unlock()
		if e != nil || s.recordPersistentPublicShareAudit(AuditContext{}, share, "document_share.image", version.ID) != nil {
			return nil, nil, publicShareUnavailable()
		}
		return asset, body, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.refreshLocked(); err != nil {
		return nil, nil, publicShareUnavailable()
	}
	share, document, _, err := s.authorizePublicShareLocked(shareID, secret, proof, true)
	if err != nil {
		return nil, nil, publicShareAccessError(err)
	}
	if document.DocumentType != DocumentTypeMarkdown {
		return nil, nil, publicShareUnavailable()
	}
	version := s.allowedPublicShareVersionLocked(share, versionID)
	if version == nil {
		return nil, nil, publicShareUnavailable()
	}
	content, err := s.loadPersistentPublishedContent(version)
	if err != nil || !documentimage.Referenced(content, assetID) {
		return nil, nil, publicShareUnavailable()
	}
	asset, body, err = s.imageContent(s.repositoryStorageLocked(), share.ProjectID, share.DocumentID, assetID)
	if err != nil {
		return nil, nil, publicShareUnavailable()
	}
	return
}
