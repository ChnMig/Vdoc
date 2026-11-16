package vdoc

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	repo "vdoc/domain/documentrepo"
)

func TestDocumentImageReferencesRejectCrossDocumentAndCollectedUploads(t *testing.T) {
	s := newTask5Store()
	document := createScopedDocument(t, s, "image-reference", "main")
	other := createScopedDocument(t, s, "image-other", "main")
	objects := newRecordingObjectStorage(nil)
	s.objects = objects
	encoded, _ := imageTestBytes(t, 42)
	asset, err := s.UploadDocumentImage("writer", "project-a", document.Document.ID, "reference.png", encoded)
	if err != nil {
		t.Fatal(err)
	}
	save := func(target *repo.CreatedDocument, content, key string) (*repo.Commit, error) {
		return s.WithRepositoryDocument(target.Document.ID).RepositorySave("writer", "project-a", target.Branch.ID, repo.SaveInput{
			ExpectedHeadCommitID: target.Branch.HeadCommitID, ExpectedGeneration: target.Branch.Generation, IdempotencyKey: key,
			Changes: []repo.Change{{Operation: "upsert", DocumentID: target.Document.ID, RelativePath: target.Document.RelativePath, Name: target.Document.Name, DocumentType: DocumentTypeMarkdown, Content: content}},
		})
	}
	if _, err := save(other, "![cross]("+asset.Reference+")", "image-cross-reference"); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("cross-document image reference was saved: %v", err)
	}
	if _, err := save(other, "```md\n![example]("+asset.Reference+")\n```", "image-code-reference"); err != nil {
		t.Fatalf("code sample should not require an image upload: %v", err)
	}
	var oldKey string
	if err := s.WithRepositoryWrite("admin", "project-a", MemberRoleAdmin, func(storage repo.Storage) error {
		record, err := storage.RepositoryRecord(s.requestContext(), "project-a", documentImageKind, documentImageID(document.Document.ID, asset.AssetID))
		if err != nil {
			return err
		}
		var value storedDocumentImage
		if err := json.Unmarshal(record.Payload, &value); err != nil {
			return err
		}
		oldKey = value.ObjectKey
		value.ObjectKey = "" // A committed collection tombstone.
		return repositoryPut(s.requestContext(), storage, "project-a", documentImageKind, record.ID, value, record.Generation+1, &record.Generation)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := save(document, "![stale]("+asset.Reference+")", "image-stale-reference"); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("collected image reference was saved: %v", err)
	}
	branches, err := s.WithRepositoryDocument(document.Document.ID).RepositoryBranches("reader", "project-a")
	if err != nil || len(branches) != 1 || branches[0].HeadCommitID != document.Commit.ID || branches[0].Generation != document.Branch.Generation {
		t.Fatal("rejected image reference moved the branch")
	}
	restored, err := s.UploadDocumentImage("writer", "project-a", document.Document.ID, "again.png", encoded)
	if err != nil || restored.AssetID != asset.AssetID || restored.Reference != asset.Reference {
		t.Fatalf("reupload did not restore stable image identity: %+v %v", restored, err)
	}
	imageWrites := []ObjectWrite{}
	for _, write := range objects.writes {
		if write.Metadata["owner_type"] == "document_image" {
			imageWrites = append(imageWrites, write)
		}
	}
	if len(imageWrites) != 2 || imageWrites[1].Key == oldKey {
		t.Fatal("reupload reused the retired object's deletion key")
	}
	if _, err := save(document, "![restored]("+restored.Reference+")", "image-restored-reference"); err != nil {
		t.Fatalf("restored image could not be saved: %v", err)
	}
}

func TestDocumentImageDedupRefreshesUploadGrace(t *testing.T) {
	s := newTask5Store()
	document := createScopedDocument(t, s, "image-grace", "main")
	encoded, _ := imageTestBytes(t, 99)
	asset, err := s.UploadDocumentImage("writer", "project-a", document.Document.ID, "grace.png", encoded)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour)
	if err := s.WithRepositoryWrite("admin", "project-a", MemberRoleAdmin, func(storage repo.Storage) error {
		record, err := storage.RepositoryRecord(s.requestContext(), "project-a", documentImageKind, documentImageID(document.Document.ID, asset.AssetID))
		if err != nil {
			return err
		}
		var value storedDocumentImage
		if err := json.Unmarshal(record.Payload, &value); err != nil {
			return err
		}
		value.CreatedAt, value.LastUploadedAt = old, old
		return repositoryPut(s.requestContext(), storage, "project-a", documentImageKind, record.ID, value, record.Generation+1, &record.Generation)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadDocumentImage("writer", "project-a", document.Document.ID, "same.png", encoded); err != nil {
		t.Fatal(err)
	}
	if err := s.WithRepositoryRead("reader", "project-a", func(storage repo.Storage) error {
		value, err := repositoryLoad[storedDocumentImage](s.requestContext(), storage, "project-a", documentImageKind, documentImageID(document.Document.ID, asset.AssetID))
		if err != nil {
			return err
		}
		if !value.CreatedAt.Equal(old) || time.Since(value.LastUploadedAt) > time.Minute {
			t.Fatal("deduplicated reupload did not grant a fresh upload grace period")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
