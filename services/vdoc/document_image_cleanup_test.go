package vdoc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"vdoc/domain/documentimage"
	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
)

type cleanupImageObjects struct {
	*recordingObjectStorage
	failDeletes int
	checkDelete func(string)
}

func (s *cleanupImageObjects) DeleteObject(ctx context.Context, key string) error {
	if s.checkDelete != nil {
		s.checkDelete(key)
	}
	if s.failDeletes > 0 {
		s.failDeletes--
		return errors.New("object service unavailable")
	}
	return s.recordingObjectStorage.DeleteObject(ctx, key)
}

func imageCleanupFixture(t *testing.T) (*Store, *repo.CreatedDocument, *cleanupImageObjects, *documentimage.Asset) {
	t.Helper()
	s := newTask5Store()
	objects := &cleanupImageObjects{recordingObjectStorage: newRecordingObjectStorage(nil)}
	s.objects = objects
	doc := createScopedDocument(t, s, "cleanup", "main")
	encoded, _ := imageTestBytes(t, 10)
	asset, err := s.UploadDocumentImage("writer", "project-a", doc.Document.ID, "image.png", encoded)
	if err != nil {
		t.Fatal(err)
	}
	return s, doc, objects, asset
}

func cleanupImageMetadata(t *testing.T, s *Store, document, asset string) *storedDocumentImage {
	t.Helper()
	var out *storedDocumentImage
	if err := s.repositoryAutomationTransaction("project-a", nil, func(storage repo.Storage) error {
		var err error
		out, err = repositoryLoad[storedDocumentImage](context.Background(), storage, "project-a", documentImageKind, documentImageID(document, asset))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func cleanupAgeImage(t *testing.T, s *Store, document, asset string, uploaded time.Time) {
	t.Helper()
	if err := s.repositoryAutomationTransaction("project-a", nil, func(storage repo.Storage) error {
		id := documentImageID(document, asset)
		record, err := storage.RepositoryRecord(context.Background(), "project-a", documentImageKind, id)
		if err != nil {
			return err
		}
		var value storedDocumentImage
		if err = json.Unmarshal(record.Payload, &value); err != nil {
			return err
		}
		value.CreatedAt, value.LastUploadedAt = uploaded, uploaded
		return repositoryPut(context.Background(), storage, "project-a", documentImageKind, id, value, record.Generation+1, &record.Generation)
	}); err != nil {
		t.Fatal(err)
	}
}

func cleanupSaveImageContent(t *testing.T, s *Store, doc *repo.CreatedDocument, content, idempotency string) {
	t.Helper()
	commit, err := s.WithRepositoryDocument(doc.Document.ID).RepositorySave("writer", "project-a", doc.Branch.ID, repo.SaveInput{
		ExpectedHeadCommitID: doc.Branch.HeadCommitID, ExpectedGeneration: doc.Branch.Generation, IdempotencyKey: idempotency,
		Changes: []repo.Change{{Operation: "upsert", DocumentID: doc.Document.ID, Name: doc.Document.Name, RelativePath: doc.Document.RelativePath, DocumentType: DocumentTypeMarkdown, Content: content}},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc.Branch.HeadCommitID, doc.Branch.Generation = commit.ID, doc.Branch.Generation+1
}

func cleanupJobs(t *testing.T, s *Store) []documentImageGC {
	t.Helper()
	var jobs []documentImageGC
	if err := s.repositoryAutomationTransaction("project-a", nil, func(storage repo.Storage) error {
		var err error
		jobs, err = repositoryList[documentImageGC](context.Background(), storage, "project-a", documentImageGCKind)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return jobs
}

func TestDocumentImageDeletePurgesOnlyOwnedObjectsAfterCommit(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory=%t", memory), func(t *testing.T) {
			s, doc, objects, asset := imageCleanupFixture(t)
			other := createScopedDocument(t, s, "other-cleanup", "main")
			encoded, _ := imageTestBytes(t, 10)
			otherImage, err := s.UploadDocumentImage("writer", "project-a", other.Document.ID, "same.png", encoded)
			if err != nil {
				t.Fatal(err)
			}
			cleanupSaveImageContent(t, s, doc, "![kept in history]("+asset.Reference+")", "delete-image-history")
			owned := cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey
			otherKey := cleanupImageMetadata(t, s, other.Document.ID, otherImage.AssetID).ObjectKey
			if memory {
				for key, body := range objects.objects {
					s.repoMemory.blobs[key] = bytes.Clone(body)
				}
				s.objects = nil
			} else {
				objects.checkDelete = func(key string) {
					if key != owned {
						t.Fatalf("deleted unrelated object %q", key)
					}
					metadata, e := repositoryLoad[storedDocumentImage](context.Background(), s.repoMemory, "project-a", documentImageKind, documentImageID(doc.Document.ID, asset.AssetID))
					catalog, _ := repositoryLoad[repo.Document](context.Background(), s.repoMemory, "project-a", "document", doc.Document.ID)
					if e != nil || metadata.ObjectKey != "" || catalog.Status != DocumentStatusArchived {
						t.Fatal("object deleted before document and tombstone commit")
					}
				}
			}
			if err = s.RepositoryDeleteDocument("admin", "project-a", doc.Document.ID); err != nil {
				t.Fatal(err)
			}
			if memory {
				if _, ok := s.repoMemory.blobs[owned]; ok {
					t.Fatal("deleted document retained image bytes")
				}
				if _, ok := s.repoMemory.blobs[otherKey]; !ok {
					t.Fatal("deleted another document's identical image")
				}
			} else if _, ok := objects.objects[owned]; ok || objects.objects[otherKey] == nil {
				t.Fatal("object cleanup did not preserve ownership")
			}
			if _, err = s.repositoryFileContent(doc.Commit.Files[0]); err != nil {
				t.Fatalf("image cleanup removed immutable Markdown: %v", err)
			}
			jobs := cleanupJobs(t, s)
			if len(jobs) != 1 || jobs[0].CompletedAt == nil || jobs[0].Attempts != 1 {
				t.Fatalf("cleanup ledger=%+v", jobs)
			}
			if err = s.RepositoryDeleteDocument("admin", "project-a", doc.Document.ID); err != nil {
				t.Fatal(err)
			}
			if len(cleanupJobs(t, s)) != 1 || len(objects.deletes) > 1 {
				t.Fatal("repeat deletion duplicated cleanup")
			}
		})
	}
}

type uncertainImageCleanupRepository struct {
	*folderRenameRepository
	commitErr error
}

func (r *uncertainImageCleanupRepository) WithinTransaction(ctx context.Context, fn func(domain.Repository) error) error {
	if err := r.folderRenameRepository.WithinTransaction(ctx, fn); err != nil {
		return err
	}
	return r.commitErr
}

func TestDocumentImageDeleteRollbackAndUnknownCommitKeepBytes(t *testing.T) {
	for _, outcome := range []string{"rollback", "unknown-committed"} {
		t.Run(outcome, func(t *testing.T) {
			s, doc, objects, asset := imageCleanupFixture(t)
			key := cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey
			p := &uncertainImageCleanupRepository{folderRenameRepository: &folderRenameRepository{state: s.cloneStateLocked(), repositoryMemory: s.repoMemory.clone()}}
			s.persistence = &postgresPersistence{repo: p}
			want := errInjectedAuditFailure
			if outcome == "rollback" {
				p.failCommit = true
			} else {
				want = domain.ErrCommitOutcomeUnknown
				p.commitErr = want
			}
			if err := s.RepositoryDeleteDocument("admin", "project-a", doc.Document.ID); !errors.Is(err, want) {
				t.Fatalf("delete error=%v want=%v", err, want)
			}
			if len(objects.deletes) != 0 || objects.objects[key] == nil {
				t.Fatal("failed or unknown metadata commit deleted image bytes")
			}
			if outcome == "unknown-committed" {
				if err := s.repositoryDrainDocumentImageGC("project-a", time.Now().UTC()); !errors.Is(err, domain.ErrCommitOutcomeUnknown) {
					t.Fatalf("uncertain read-back error=%v", err)
				}
				if len(objects.deletes) != 0 || objects.objects[key] == nil {
					t.Fatal("unconfirmed read-back deleted image bytes")
				}
			}
			p.failCommit, p.commitErr = false, nil
			if err := s.repositoryDrainDocumentImageGC("project-a", time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			if outcome == "rollback" {
				if objects.objects[key] == nil || cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey != key {
					t.Fatal("rollback did not retain image metadata and bytes")
				}
			} else if objects.objects[key] != nil {
				t.Fatal("confirmed committed cleanup was not retried")
			}
		})
	}
}

func TestDocumentImageCleanupRetriesAfterRestartWithoutActiveActor(t *testing.T) {
	s, doc, objects, asset := imageCleanupFixture(t)
	key := cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey
	p := &folderRenameRepository{state: s.cloneStateLocked(), repositoryMemory: s.repoMemory.clone()}
	s.persistence = &postgresPersistence{repo: p}
	objects.failDeletes = 1
	if err := s.RepositoryDeleteDocument("admin", "project-a", doc.Document.ID); err != nil {
		t.Fatal(err)
	}
	jobs := cleanupJobs(t, s)
	if len(jobs) != 1 || jobs[0].CompletedAt != nil || jobs[0].Attempts != 1 || jobs[0].NextAttemptAt.IsZero() || objects.objects[key] == nil {
		t.Fatalf("retry was not persisted: %+v", jobs)
	}
	// 模拟重启前重试到期，同时归档项目并移除所有操作者。
	job := jobs[0]
	job.NextAttemptAt = time.Time{}
	record, _ := p.RepositoryRecord(context.Background(), "project-a", documentImageGCKind, job.ID)
	if err := repositoryPut(context.Background(), p, "project-a", documentImageGCKind, job.ID, job, record.Generation+1, &record.Generation); err != nil {
		t.Fatal(err)
	}
	p.state.Projects["project-a"].Status = ProjectStatusArchived
	p.state.Users = map[string]*User{}
	p.state.Members = map[string]*ProjectMember{}
	restarted := NewStore()
	restarted.persistence, restarted.objects = &postgresPersistence{repo: p}, objects
	restarted.repositoryDrainProjects(false)
	jobs = cleanupJobs(t, restarted)
	if objects.objects[key] != nil || len(jobs) != 1 || jobs[0].CompletedAt == nil || jobs[0].Attempts != 2 {
		t.Fatalf("restart failed to retry archived project's cleanup: %+v", jobs)
	}
}

func TestDocumentImageAbandonedUploadGraceAndHistoricalRetention(t *testing.T) {
	for _, source := range []string{"abandoned", "commit", "commit-entity", "commit-escape", "draft", "version", "merge-result"} {
		t.Run(source, func(t *testing.T) {
			s, doc, objects, asset := imageCleanupFixture(t)
			content := "![history](" + asset.Reference + ")"
			switch source {
			case "commit", "commit-entity", "commit-escape":
				if source == "commit-entity" {
					content = "![history](vdoc-asset&#58;" + asset.AssetID + ")"
				} else if source == "commit-escape" {
					content = "![history](vdoc-asset\\:" + asset.AssetID + ")"
				}
				cleanupSaveImageContent(t, s, doc, content, "image-history-retained")
				cleanupSaveImageContent(t, s, doc, "# Link removed", "image-history-removed")
			case "draft":
				s.drafts["historical-draft"] = &ContractDraft{ID: "historical-draft", ProjectID: "project-a", DocumentID: doc.Document.ID, RawSchema: content, RawSchemaHash: sha(content)}
			case "version":
				s.versions["historical-version"] = &ContractVersion{ID: "historical-version", ProjectID: "project-a", DocumentID: doc.Document.ID, RawSchema: content, RawSchemaHash: sha(content)}
			case "merge-result":
				if err := s.WithRepositoryWrite("admin", "project-a", MemberRoleAdmin, func(storage repo.Storage) error {
					file, err := s.repositoryStoreFile(storage, "project-a", doc.Document.File, content)
					if err != nil {
						return err
					}
					return repositoryPut(context.Background(), storage, "project-a", "merge_request", "pending-image-result", repo.MergeRequest{ID: "pending-image-result", ProjectID: "project-a", ResultFiles: []repo.File{file}, Status: "open"}, 1, nil)
				}); err != nil {
					t.Fatal(err)
				}
			}
			key := cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey
			now := time.Now().UTC()
			cleanupAgeImage(t, s, doc.Document.ID, asset.AssetID, now.Add(-documentImageUploadGrace))
			if err := s.repositorySweepDocumentImages("project-a", now.Add(-time.Nanosecond)); err != nil {
				t.Fatal(err)
			}
			if cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey == "" {
				t.Fatal("image retired before grace boundary")
			}
			if err := s.repositorySweepDocumentImages("project-a", now); err != nil {
				t.Fatal(err)
			}
			if err := s.repositoryDrainDocumentImageGC("project-a", now); err != nil {
				t.Fatal(err)
			}
			if source == "abandoned" {
				if objects.objects[key] != nil || cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey != "" {
					t.Fatal("abandoned upload was not reclaimed")
				}
			} else if objects.objects[key] == nil || cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey != key {
				t.Fatal("historical image was reclaimed")
			}
		})
	}
}

func TestDocumentImageCleanupReuploadCannotDeleteReplacement(t *testing.T) {
	s, doc, objects, asset := imageCleanupFixture(t)
	oldKey := cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey
	now := time.Now().UTC()
	cleanupAgeImage(t, s, doc.Document.ID, asset.AssetID, now.Add(-25*time.Hour))
	if err := s.repositorySweepDocumentImages("project-a", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WithRepositoryDocument(doc.Document.ID).RepositorySave("writer", "project-a", doc.Branch.ID, repo.SaveInput{ExpectedHeadCommitID: doc.Branch.HeadCommitID, ExpectedGeneration: doc.Branch.Generation, IdempotencyKey: "stale-image-save-reject", Changes: []repo.Change{{Operation: "upsert", DocumentID: doc.Document.ID, Name: doc.Document.Name, RelativePath: doc.Document.RelativePath, DocumentType: DocumentTypeMarkdown, Content: "![stale](" + asset.Reference + ")"}}}); err == nil {
		t.Fatal("save adopted a retired image")
	}
	encoded, expected := imageTestBytes(t, 10)
	if _, err := s.UploadDocumentImage("writer", "project-a", doc.Document.ID, "again.png", encoded); err != nil {
		t.Fatal(err)
	}
	newKey := cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey
	if newKey == oldKey || newKey == "" {
		t.Fatal("re-upload reused the retired object's key")
	}
	if err := s.repositoryDrainDocumentImageGC("project-a", now); err != nil {
		t.Fatal(err)
	}
	_, body, err := s.DocumentImage("reader", "project-a", doc.Document.ID, asset.AssetID)
	if err != nil || !bytes.Equal(body, expected) || objects.objects[oldKey] != nil || objects.objects[newKey] == nil {
		t.Fatal("old cleanup deleted replacement image")
	}
	cleanupSaveImageContent(t, s, doc, "![replacement]("+asset.Reference+")", "replacement-image-save")
}

func TestDocumentImageDedupRenewsGraceAndFailedHistoryReadRetainsBytes(t *testing.T) {
	for _, scenario := range []string{"dedup", "missing-history", "missing-legacy-content", "archived-document", "archived-project"} {
		t.Run(scenario, func(t *testing.T) {
			s, doc, objects, asset := imageCleanupFixture(t)
			key := cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey
			now := time.Now().UTC()
			cleanupAgeImage(t, s, doc.Document.ID, asset.AssetID, now.Add(-25*time.Hour))
			switch scenario {
			case "dedup":
				encoded, _ := imageTestBytes(t, 10)
				if _, err := s.UploadDocumentImage("writer", "project-a", doc.Document.ID, "again.png", encoded); err != nil {
					t.Fatal(err)
				}
			case "missing-history":
				delete(objects.objects, doc.Commit.Files[0].ObjectKey)
			case "missing-legacy-content":
				s.drafts["missing-image-content"] = &ContractDraft{ID: "missing-image-content", ProjectID: "project-a", DocumentID: doc.Document.ID, RawSchemaHash: sha("![unavailable](" + asset.Reference + ")")}
			case "archived-document":
				if err := s.WithRepositoryWrite("admin", "project-a", MemberRoleAdmin, func(storage repo.Storage) error {
					value, err := repositoryLoad[repo.Document](context.Background(), storage, "project-a", "document", doc.Document.ID)
					if err != nil {
						return err
					}
					value.Status = DocumentStatusArchived
					return s.repositoryPutDocument(storage, "project-a", value)
				}); err != nil {
					t.Fatal(err)
				}
			case "archived-project":
				s.projects["project-a"].Status = ProjectStatusArchived
			}
			err := s.repositorySweepDocumentImages("project-a", now)
			wantError := scenario == "missing-history" || scenario == "missing-legacy-content"
			if wantError && err == nil {
				t.Fatal("missing historical source was treated as unreferenced")
			}
			if !wantError && err != nil {
				t.Fatal(err)
			}
			if err = s.repositoryDrainDocumentImageGC("project-a", now); err != nil {
				t.Fatal(err)
			}
			if objects.objects[key] == nil || cleanupImageMetadata(t, s, doc.Document.ID, asset.AssetID).ObjectKey != key {
				t.Fatal("uncertain or archived image was reclaimed")
			}
		})
	}
}

func TestDocumentImageLegacyArchiveKeepsImagesDespiteActiveCatalogProjection(t *testing.T) {
	s := newTask5Store()
	objects := &cleanupImageObjects{recordingObjectStorage: newRecordingObjectStorage(nil)}
	s.objects = objects
	document, err := s.CreateDocument("admin", "project-a", "Legacy images", DocumentTypeMarkdown, "legacy-images.md", "")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := imageTestBytes(t, 22)
	asset, err := s.UploadDocumentImage("writer", "project-a", document.ID, "legacy.png", encoded)
	if err != nil {
		t.Fatal(err)
	}
	key := cleanupImageMetadata(t, s, document.ID, asset.AssetID).ObjectKey
	now := time.Now().UTC()
	cleanupAgeImage(t, s, document.ID, asset.AssetID, now.Add(-25*time.Hour))
	if err = s.WithRepositoryWrite("admin", "project-a", MemberRoleAdmin, func(storage repo.Storage) error {
		return repositoryPut(context.Background(), storage, "project-a", "document", document.ID, repo.Document{ID: document.ID, ProjectID: "project-a", File: repo.File{DocumentID: document.ID, DocumentType: DocumentTypeMarkdown}, Status: DocumentStatusActive}, 1, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArchiveDocument("admin", "project-a", document.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.repositorySweepDocumentImages("project-a", now); err != nil {
		t.Fatal(err)
	}
	if len(cleanupJobs(t, s)) != 0 || objects.objects[key] == nil || cleanupImageMetadata(t, s, document.ID, asset.AssetID).ObjectKey != key {
		t.Fatal("legacy archive's retained image was reclaimed")
	}
}
