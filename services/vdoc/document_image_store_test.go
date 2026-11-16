package vdoc

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"vdoc/domain/documentimage"
	repo "vdoc/domain/documentrepo"
	"vdoc/domain/documentshare"
)

func imageTestBytes(t *testing.T, red uint8) (string, []byte) {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: red, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, picture); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes()), b.Bytes()
}

func TestDocumentImagesPreserveBytesDeduplicateAndEnforceOwnership(t *testing.T) {
	for _, objectStorage := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "objects"}[objectStorage], func(t *testing.T) {
			s := newTask5Store()
			doc := createScopedDocument(t, s, "images", "main")
			other := createScopedDocument(t, s, "other-images", "main")
			objects := newRecordingObjectStorage(nil)
			if objectStorage {
				s.objects = objects
			}
			encoded, expected := imageTestBytes(t, 255)
			asset, err := s.UploadDocumentImage("writer", "project-a", doc.Document.ID, "screen.png", encoded)
			if err != nil {
				t.Fatal(err)
			}
			again, err := s.UploadDocumentImage("writer", "project-a", doc.Document.ID, "renamed.png", encoded)
			if err != nil || again.AssetID != asset.AssetID || again.Filename != "screen.png" {
				t.Fatalf("dedup=%+v %v", again, err)
			}
			if objectStorage && len(objects.writes) != 1 {
				t.Fatalf("dedup wrote %d objects", len(objects.writes))
			}
			read, body, err := s.DocumentImage("reader", "project-a", doc.Document.ID, asset.AssetID)
			if err != nil || !bytes.Equal(body, expected) || read.ContentType != "image/png" {
				t.Fatalf("read=%+v %v", read, err)
			}
			body[0] = 0
			_, body, err = s.DocumentImage("reader", "project-a", doc.Document.ID, asset.AssetID)
			if err != nil || !bytes.Equal(body, expected) {
				t.Fatal("returned body aliases stored image")
			}
			if _, _, err = s.DocumentImage("reader", "project-a", other.Document.ID, asset.AssetID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("cross-document read: %v", err)
			}
			if _, _, err = s.DocumentImage("reader", "project-b", doc.Document.ID, asset.AssetID); !errors.Is(err, ErrPermissionDenied) {
				t.Fatalf("cross-project read: %v", err)
			}
			if _, err = s.UploadDocumentImage("reader", "project-a", doc.Document.ID, "bad.png", encoded); !errors.Is(err, ErrPermissionDenied) {
				t.Fatalf("reader upload: %v", err)
			}
			if _, err = s.UploadDocumentImage("writer", "project-a", "missing", "bad.png", encoded); !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing document: %v", err)
			}
			if _, err = s.UploadDocumentImage("writer", "project-a", doc.Document.ID, "bad.png", "bad"); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("bad bytes: %v", err)
			}
			if err = s.RepositoryDeleteDocument("admin", "project-a", doc.Document.ID); err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.DocumentImage("reader", "project-a", doc.Document.ID, asset.AssetID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("deleted document read: %v", err)
			}
		})
	}
}

func TestDocumentImagePartialWriteCleanupPreservesCommittedImage(t *testing.T) {
	s := newTask5Store()
	doc := createScopedDocument(t, s, "cleanup-images", "main")
	objects := &partialFailureObjectStorage{recordingObjectStorage: newRecordingObjectStorage(nil)}
	s.objects = objects
	one, expected := imageTestBytes(t, 1)
	asset, err := s.UploadDocumentImage("writer", "project-a", doc.Document.ID, "one.png", one)
	if err != nil {
		t.Fatal(err)
	}
	objects.failAfterWrite, objects.partialErr = 2, errors.New("upload response lost")
	two, _ := imageTestBytes(t, 2)
	if _, err = s.UploadDocumentImage("writer", "project-a", doc.Document.ID, "two.png", two); err == nil {
		t.Fatal("partial write succeeded")
	}
	if len(objects.objects) != 1 || len(objects.deletes) != 1 {
		t.Fatalf("cleanup retained objects=%d deletes=%v", len(objects.objects), objects.deletes)
	}
	_, body, err := s.DocumentImage("reader", "project-a", doc.Document.ID, asset.AssetID)
	if err != nil || !bytes.Equal(body, expected) {
		t.Fatal("failed upload deleted committed image")
	}
}

func TestPublicShareImagesPinContentPasswordRevocationAndActiveParents(t *testing.T) {
	s, project, branch, commit, _ := newRepositoryShareFixture(t)
	document := commit.Files[0].DocumentID
	one, expected := imageTestBytes(t, 1)
	two, _ := imageTestBytes(t, 2)
	asset, err := s.UploadDocumentImage("admin", project, document, "one.png", one)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := s.UploadDocumentImage("admin", project, document, "staged.png", two)
	if err != nil {
		t.Fatal(err)
	}
	content := "# Images\n\n![one](" + asset.Reference + ")\n\n```md\n![example](" + staged.Reference + ")\n```"
	change := repo.Change{Operation: "upsert", DocumentID: document, Name: "Images", RelativePath: "docs/original.md", DocumentType: DocumentTypeMarkdown, Content: content}
	commit, err = s.RepositorySave("admin", project, branch.ID, repo.SaveInput{ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "images-fixed-content", Changes: []repo.Change{change}})
	if err != nil {
		t.Fatal(err)
	}
	branch.HeadCommitID, branch.Generation = commit.ID, branch.Generation+1
	release, err := s.RepositoryPublish("admin", project, repo.PublishInput{BranchID: branch.ID, CommitID: commit.ID, ExpectedGeneration: branch.Generation})
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.CreateRepositoryShare("admin", project, release.ID, document, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent, Password: "image-share-password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.PublicRepositoryShareImage(share.Share.ID, share.Secret, "", release.ID, asset.AssetID); !errors.Is(err, ErrPublicSharePasswordRequired) {
		t.Fatalf("password bypass: %v", err)
	}
	proof, _, err := s.UnlockPublicRepositoryShare(share.Share.ID, share.Secret, "image-share-password")
	if err != nil {
		t.Fatal(err)
	}
	got, body, err := s.PublicRepositoryShareImage(share.Share.ID, share.Secret, proof, release.ID, asset.AssetID)
	if err != nil || got.AssetID != asset.AssetID || !bytes.Equal(body, expected) {
		t.Fatalf("public read=%+v %v", got, err)
	}
	for _, tc := range []struct{ secret, version, image string }{{"wrong", release.ID, asset.AssetID}, {share.Secret, "wrong", asset.AssetID}, {share.Secret, release.ID, staged.AssetID}, {share.Secret, release.ID, strings.Repeat("f", 64)}} {
		if _, _, err = s.PublicRepositoryShareImage(share.Share.ID, tc.secret, proof, tc.version, tc.image); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unexpected public access: %+v %v", tc, err)
		}
	}
	// 后续提交引用另一张图，旧分享仍只读旧版图。
	change.Content = "![later](" + staged.Reference + ")"
	if _, err = s.RepositorySave("admin", project, branch.ID, repo.SaveInput{ExpectedHeadCommitID: branch.HeadCommitID, ExpectedGeneration: branch.Generation, IdempotencyKey: "images-later-content", Changes: []repo.Change{change}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.PublicRepositoryShareImage(share.Share.ID, share.Secret, proof, release.ID, staged.AssetID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("later image leaked: %v", err)
	}
	if _, err = s.RevokeRepositoryShare("admin", project, release.ID, document, share.Share.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.PublicRepositoryShareImage(share.Share.ID, share.Secret, proof, release.ID, asset.AssetID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked image read: %v", err)
	}
	if !documentimage.ValidID(asset.AssetID) {
		t.Fatal("asset identity is not a hash")
	}
}
