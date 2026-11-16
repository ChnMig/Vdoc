package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"vdoc/domain/documentimage"
	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
)

const (
	documentImageGCKind        = "document_image_gc"
	documentImageDeletedKind   = "document_image_deleted"
	documentImageUploadGrace   = 24 * time.Hour
	documentImageSweepInterval = time.Minute
)

type documentImageDeletion struct {
	ProjectID  string    `json:"project_id"`
	DocumentID string    `json:"document_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// 每个任务固定旧对象的唯一 key；相同图片重新上传不会被旧任务删除。
type documentImageGC struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	DocumentID    string     `json:"document_id"`
	AssetID       string     `json:"asset_id"`
	ObjectKey     string     `json:"object_key"`
	CreatedAt     time.Time  `json:"created_at"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

func (s *Store) repositoryVisitImageRecords(storage repo.Storage, project, kind string, visit func(repo.Record) error) error {
	var after int64
	for {
		if err := s.requestContext().Err(); err != nil {
			return err
		}
		rows, err := storage.RepositoryRecords(s.requestContext(), project, kind, after, 100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Sequence <= after {
				return ErrFailedPrecondition
			}
			after = row.Sequence
			if err := visit(row); err != nil {
				return err
			}
		}
		if len(rows) < 100 {
			return nil
		}
	}
}

func validDocumentImageObject(project, document, asset, key string) bool {
	return project != "" && document != "" && documentimage.ValidID(asset) &&
		strings.HasPrefix(key, "projects/"+project+"/documents/"+document+"/images/") &&
		strings.HasSuffix(key, "/"+asset)
}

func (s *Store) repositoryRetireDocumentImage(storage repo.Storage, project string, record repo.Record, value storedDocumentImage, now time.Time) error {
	if value.ObjectKey == "" {
		return nil
	}
	if value.ProjectID != project || record.ID != documentImageID(value.DocumentID, value.AssetID) ||
		!validDocumentImageObject(project, value.DocumentID, value.AssetID, value.ObjectKey) {
		return ErrFailedPrecondition
	}
	job := documentImageGC{ID: repo.Hash([]byte(value.ObjectKey)), ProjectID: project, DocumentID: value.DocumentID, AssetID: value.AssetID, ObjectKey: value.ObjectKey, CreatedAt: now}
	previous, err := repositoryLoad[documentImageGC](s.requestContext(), storage, project, documentImageGCKind, job.ID)
	if errors.Is(err, ErrNotFound) {
		if err = repositoryPut(s.requestContext(), storage, project, documentImageGCKind, job.ID, job, 1, nil); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if previous.ObjectKey != job.ObjectKey || previous.DocumentID != job.DocumentID || previous.AssetID != job.AssetID || previous.CompletedAt != nil {
		return ErrFailedPrecondition
	}
	value.ObjectKey = ""
	return repositoryPut(s.requestContext(), storage, project, documentImageKind, record.ID, value, record.Generation+1, &record.Generation)
}

// 删除意图和图片墓碑与文档删除同事务提交，事务外才会删除对象。
func (s *Store) repositoryMarkDocumentImagesDeleted(storage repo.Storage, project, document string, now time.Time) error {
	marked := false
	return s.repositoryVisitImageRecords(storage, project, documentImageKind, func(record repo.Record) error {
		var value storedDocumentImage
		if err := json.Unmarshal(record.Payload, &value); err != nil {
			return err
		}
		if value.DocumentID != document || value.ObjectKey == "" {
			return nil
		}
		if !marked {
			marker, err := repositoryLoad[documentImageDeletion](s.requestContext(), storage, project, documentImageDeletedKind, document)
			if errors.Is(err, ErrNotFound) {
				marker = &documentImageDeletion{ProjectID: project, DocumentID: document, CreatedAt: now}
				if err = repositoryPut(s.requestContext(), storage, project, documentImageDeletedKind, document, marker, 1, nil); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if marker.ProjectID != project || marker.DocumentID != document {
				return ErrFailedPrecondition
			}
			marked = true
		}
		return s.repositoryRetireDocumentImage(storage, project, record, value, now)
	})
}

// 在项目锁后重新读取旧版工作状态，避免跨实例保存与回收之间出现旧快照窗口。
func (s *Store) repositoryImageMaintenance(project string, fn func(repo.Storage, *domain.State) error) error {
	var database domain.Repository
	return s.repositoryAutomationTransaction(project, func(value domain.Repository) error {
		database = value
		return nil
	}, func(storage repo.Storage) error {
		state := s.stateLocked()
		if database != nil {
			var err error
			if working, ok := database.(interface {
				LoadWorkingStateWithRevision(context.Context) (*domain.State, string, error)
			}); ok {
				state, _, err = working.LoadWorkingStateWithRevision(s.requestContext())
			} else {
				state, err = database.LoadState(s.requestContext())
			}
			if err != nil {
				return err
			}
		}
		if state == nil {
			return ErrFailedPrecondition
		}
		return fn(storage, state)
	})
}

func (s *Store) repositorySweepDocumentImages(project string, now time.Time) error {
	return s.repositoryImageMaintenance(project, func(storage repo.Storage, state *domain.State) error {
		type candidate struct {
			record repo.Record
			image  storedDocumentImage
		}
		candidates := map[string]candidate{}
		active := map[string]bool{}
		if p := state.Projects[project]; p != nil && p.Status == ProjectStatusActive {
			for _, document := range state.APIServices {
				if document.ProjectID == project {
					active[document.ID] = document.Status == DocumentStatusActive && document.DocumentType == DocumentTypeMarkdown
				}
			}
			if err := s.repositoryVisitImageRecords(storage, project, "document", func(record repo.Record) error {
				var document repo.Document
				if err := json.Unmarshal(record.Payload, &document); err != nil {
					return err
				}
				visible := document.Status == DocumentStatusActive && document.DocumentType == DocumentTypeMarkdown
				if legacy := state.APIServices[document.ID]; legacy != nil && legacy.ProjectID == project && legacy.Status != DocumentStatusActive {
					visible = false
				}
				active[document.ID] = visible
				return nil
			}); err != nil {
				return err
			}
		}
		if err := s.repositoryVisitImageRecords(storage, project, documentImageKind, func(record repo.Record) error {
			var value storedDocumentImage
			if err := json.Unmarshal(record.Payload, &value); err != nil {
				return err
			}
			if value.ObjectKey == "" {
				return nil
			}
			marker, err := repositoryLoad[documentImageDeletion](s.requestContext(), storage, project, documentImageDeletedKind, value.DocumentID)
			if err == nil {
				if marker.ProjectID != project || marker.DocumentID != value.DocumentID {
					return ErrFailedPrecondition
				}
				return s.repositoryRetireDocumentImage(storage, project, record, value, now)
			}
			if !errors.Is(err, ErrNotFound) {
				return err
			}
			uploaded := value.LastUploadedAt
			if uploaded.IsZero() {
				uploaded = value.CreatedAt
			}
			if active[value.DocumentID] && !uploaded.IsZero() && !now.Before(uploaded.Add(documentImageUploadGrace)) {
				candidates[record.ID] = candidate{record, value}
			}
			return nil
		}); err != nil || len(candidates) == 0 {
			return err
		}
		preserve := func(document, content string) error {
			ids, err := documentimage.References(content)
			if err != nil {
				return err
			}
			for _, asset := range ids {
				delete(candidates, documentImageID(document, asset))
			}
			return nil
		}
		seen := map[string]bool{}
		preserveFiles := func(files []repo.File) error {
			for _, file := range files {
				if !active[file.DocumentID] || file.DocumentType != DocumentTypeMarkdown {
					continue
				}
				key := file.DocumentID + ":" + file.ObjectKey + ":" + file.ContentHash
				if seen[key] {
					continue
				}
				content, err := s.repositoryFileContent(file)
				if err != nil {
					return err
				}
				if err = preserve(file.DocumentID, content); err != nil {
					return err
				}
				seen[key] = true
			}
			return nil
		}
		for _, kind := range []string{"commit", "merge_request"} {
			if err := s.repositoryVisitImageRecords(storage, project, kind, func(record repo.Record) error {
				var value struct {
					Files       []repo.File `json:"files"`
					ResultFiles []repo.File `json:"result_files"`
				}
				if err := json.Unmarshal(record.Payload, &value); err != nil {
					return err
				}
				return preserveFiles(append(value.Files, value.ResultFiles...))
			}); err != nil {
				return err
			}
		}
		legacy := func(projectID, document, service, raw, rawKey, rawHash, stable, stableKey, stableHash string) error {
			if document == "" {
				document = service
			}
			if projectID != project || !active[document] {
				return nil
			}
			for _, value := range [][3]string{{raw, rawKey, rawHash}, {stable, stableKey, stableHash}} {
				content := value[0]
				if content == "" && value[1] == "" && value[2] != "" && value[2] != sha("") {
					return fmt.Errorf("%w: historical Markdown content is unavailable", ErrFailedPrecondition)
				}
				if err := s.hydrateStoredContentLocked(s.requestContext(), value[1], value[2], &content); err != nil {
					return err
				}
				if err := preserve(document, content); err != nil {
					return err
				}
			}
			return nil
		}
		for _, draft := range state.Drafts {
			if err := legacy(draft.ProjectID, draft.DocumentID, draft.ServiceID, draft.RawSchema, draft.RawSchemaObjectKey, draft.RawSchemaHash, draft.NormalizedSchema, draft.NormalizedObjectKey, draft.NormalizedSchemaHash); err != nil {
				return err
			}
		}
		for _, version := range state.Versions {
			if err := legacy(version.ProjectID, version.DocumentID, version.ServiceID, version.RawSchema, version.RawSchemaObjectKey, version.RawSchemaHash, version.NormalizedSchema, version.NormalizedObjectKey, version.NormalizedSchemaHash); err != nil {
				return err
			}
		}
		for _, value := range candidates {
			if err := s.repositoryRetireDocumentImage(storage, project, value.record, value.image, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func documentImageGCRetryDelay(attempts int) time.Duration {
	delay := 3 * time.Second
	for i := 1; i < attempts && delay < time.Hour; i++ {
		delay *= 2
	}
	return min(delay, time.Hour)
}

func (s *Store) repositoryRunDocumentImageGC(project, id string, now time.Time) (bool, error) {
	var job *documentImageGC
	// 独立读回已提交墓碑；上一次事务提交结果不确定时也不能直接删除对象。
	if err := s.repositoryAutomationTransaction(project, nil, func(storage repo.Storage) error {
		value, err := repositoryLoad[documentImageGC](s.requestContext(), storage, project, documentImageGCKind, id)
		if err != nil {
			return err
		}
		if value.CompletedAt != nil || now.Before(value.NextAttemptAt) {
			return nil
		}
		if value.ID != id || value.ProjectID != project || repo.Hash([]byte(value.ObjectKey)) != id || !validDocumentImageObject(project, value.DocumentID, value.AssetID, value.ObjectKey) {
			return ErrFailedPrecondition
		}
		image, err := repositoryLoad[storedDocumentImage](s.requestContext(), storage, project, documentImageKind, documentImageID(value.DocumentID, value.AssetID))
		if err != nil {
			return err
		}
		if image.ProjectID != project || image.DocumentID != value.DocumentID || image.AssetID != value.AssetID || image.ObjectKey == value.ObjectKey {
			return ErrFailedPrecondition
		}
		job = value
		return nil
	}); err != nil || job == nil {
		return false, err
	}
	var deleteErr error
	if s.objects != nil {
		deleteErr = s.objects.DeleteObject(s.requestContext(), job.ObjectKey)
	} else {
		s.mu.Lock()
		if s.persistence != nil || s.repoMemory == nil {
			deleteErr = ErrFailedPrecondition
		} else {
			delete(s.repoMemory.blobs, job.ObjectKey)
		}
		s.mu.Unlock()
	}
	ackErr := s.repositoryAutomationTransaction(project, nil, func(storage repo.Storage) error {
		record, err := storage.RepositoryRecord(s.requestContext(), project, documentImageGCKind, id)
		if err != nil {
			return err
		}
		var current documentImageGC
		if err = json.Unmarshal(record.Payload, &current); err != nil {
			return err
		}
		if current.CompletedAt != nil {
			return nil
		}
		if current.ObjectKey != job.ObjectKey {
			return ErrFailedPrecondition
		}
		current.Attempts++
		if deleteErr == nil {
			current.CompletedAt = &now
		} else {
			current.NextAttemptAt = now.Add(documentImageGCRetryDelay(current.Attempts))
		}
		return repositoryPut(s.requestContext(), storage, project, documentImageGCKind, id, current, record.Generation+1, &record.Generation)
	})
	return true, errors.Join(deleteErr, ackErr)
}

func (s *Store) repositoryDrainDocumentImageGC(project string, now time.Time) error {
	var after int64
	var failures []error
	attempted := 0
	for {
		var rows []repo.Record
		if err := s.repositoryAutomationTransaction(project, nil, func(storage repo.Storage) error {
			var err error
			rows, err = storage.RepositoryRecords(s.requestContext(), project, documentImageGCKind, after, 100)
			return err
		}); err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, row := range rows {
			after = row.Sequence
			didAttempt, err := s.repositoryRunDocumentImageGC(project, row.ID, now)
			if err != nil {
				failures = append(failures, fmt.Errorf("image cleanup %s: %w", row.ID, err))
			}
			if didAttempt {
				attempted++
			}
			if attempted >= 32 {
				return errors.Join(failures...)
			}
		}
		if len(rows) < 100 {
			return errors.Join(failures...)
		}
	}
}

func (s *Store) repositoryCleanupDeletedDocumentImages(project string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = s.WithContext(ctx).repositoryDrainDocumentImageGC(project, time.Now().UTC())
}
