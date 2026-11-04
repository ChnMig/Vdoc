package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	repo "vdoc/domain/documentrepo"
)

type repositoryDocumentKey struct{}

// WithRepositoryDocument 只绑定本次请求，所有引用仍受项目权限和文档隔离约束。
func (s *Store) WithRepositoryDocument(documentID string) *Store {
	return s.WithContext(context.WithValue(s.requestContext(), repositoryDocumentKey{}, documentID))
}

func (s *Store) repositoryDocumentID() string {
	value, _ := s.requestContext().Value(repositoryDocumentKey{}).(string)
	return value
}

// 文档隔离复用现有事务、不可变记录约束和全局唯一引用，后台投递仍可读取完整事件。
type documentRepositoryStorage struct {
	repo.Storage
	documentID string
}

func repositoryRecordDocument(record repo.Record) string {
	var value struct {
		DocumentID string `json:"document_id"`
	}
	_ = json.Unmarshal(record.Payload, &value)
	return value.DocumentID
}

func (d documentRepositoryStorage) key(kind, key string) string {
	if kind == "metadata" || kind == "idempotency" {
		return "document:" + d.documentID + ":" + key
	}
	return key
}

func (d documentRepositoryStorage) RepositoryRecord(ctx context.Context, project, kind, key string) (*repo.Record, error) {
	r, err := d.Storage.RepositoryRecord(ctx, project, kind, d.key(kind, key))
	if err != nil {
		return nil, err
	}
	if repositoryRecordDocument(*r) != d.documentID {
		return nil, ErrNotFound
	}
	copy := *r
	copy.ID = key
	return &copy, nil
}

func (d documentRepositoryStorage) RepositoryRecords(ctx context.Context, project, kind string, after int64, limit int) ([]repo.Record, error) {
	if scoped, ok := d.Storage.(interface {
		RepositoryDocumentRecords(context.Context, string, string, string, int64, int) ([]repo.Record, error)
	}); ok {
		return scoped.RepositoryDocumentRecords(ctx, project, d.documentID, kind, after, limit)
	}
	if limit < 1 || limit > 500 {
		return nil, ErrInvalidArgument
	}
	out := []repo.Record{}
	for {
		rows, err := d.Storage.RepositoryRecords(ctx, project, kind, after, 500)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			after = row.Sequence
			if repositoryRecordDocument(row) == d.documentID {
				out = append(out, row)
			}
			if len(out) == limit {
				return out, nil
			}
		}
		if len(rows) < 500 {
			return out, nil
		}
	}
}

func (d documentRepositoryStorage) PutRepositoryRecord(ctx context.Context, record repo.Record, expected *int64) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(record.Payload, &payload); err != nil || payload == nil {
		return ErrInvalidArgument
	}
	if owner := repositoryRecordDocument(record); owner != "" && owner != d.documentID {
		return ErrInvalidArgument
	}
	if expected != nil {
		if _, err := d.RepositoryRecord(ctx, record.ProjectID, record.Kind, record.ID); err != nil {
			return err
		}
	}
	payload["document_id"], _ = json.Marshal(d.documentID)
	record.Payload, _ = json.Marshal(payload)
	record.ID = d.key(record.Kind, record.ID)
	return d.Storage.PutRepositoryRecord(ctx, record, expected)
}

func (d documentRepositoryStorage) RepositoryLatestSequence(ctx context.Context, project, kind string) (int64, error) {
	var last int64
	for {
		rows, err := d.RepositoryRecords(ctx, project, kind, last, 500)
		if err != nil {
			return 0, err
		}
		for _, row := range rows {
			last = row.Sequence
		}
		if len(rows) < 500 {
			return last, nil
		}
	}
}

func validRepositoryBranchName(name string) bool {
	return name != "" && len(name) <= 200 && !strings.ContainsAny(name, " \t\n\r~^:?*[\\") && !strings.Contains(name, "..") && !strings.HasPrefix(name, "/") && !strings.HasSuffix(name, "/") && !strings.HasPrefix(name, "legacy/")
}

func (s *Store) repositoryDocumentChanges(changes []repo.Change) error {
	documentID := s.repositoryDocumentID()
	if documentID == "" {
		return nil
	}
	if len(changes) > 1 {
		return fmt.Errorf("%w: one document per commit", ErrInvalidArgument)
	}
	for _, change := range changes {
		if change.DocumentID != documentID {
			return fmt.Errorf("%w: change belongs to another document", ErrInvalidArgument)
		}
	}
	return nil
}

// 已迁移文档只能经文档入口推进，防止旧客户端形成另一条可写历史。
func (s *Store) repositoryCheckLegacyWrite(storage repo.Storage, project string, b *repo.Branch, c *repo.Commit) error {
	if b.DocumentID != s.repositoryDocumentID() {
		return ErrInvalidArgument
	}
	if b.DocumentID != "" {
		return nil
	}
	parent, err := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", b.HeadCommitID)
	if err != nil {
		return err
	}
	for _, file := range append(append([]repo.File{}, parent.Files...), c.Files...) {
		if err := s.repositoryRequireActiveDocument(storage, project, file.DocumentID); err != nil {
			return err
		}
		_, err := storage.RepositoryRecord(s.requestContext(), project, "metadata", "document:"+file.DocumentID+":converted")
		if err == nil {
			return fmt.Errorf("%w: DOCUMENT_REPOSITORY_REQUIRED: use this document's branch", ErrFailedPrecondition)
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}

// 项目目录以默认分支的元数据为准，切换其他分支不会改变文件列表。
func (s *Store) repositoryUpdateCatalog(storage repo.Storage, project string, b *repo.Branch, c *repo.Commit) error {
	if b.DocumentID == "" {
		return nil
	}
	record, err := storage.RepositoryRecord(s.requestContext(), project, "document", b.DocumentID)
	if err != nil {
		return err
	}
	var d repo.Document
	if err = json.Unmarshal(record.Payload, &d); err != nil {
		return err
	}
	if d.DefaultBranchID != b.ID {
		return nil
	}
	file, exists := fileByID(c.Files, d.ID)
	if !exists {
		return nil
	}
	if len(c.ParentIDs) > 0 {
		parent, err := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", c.ParentIDs[0])
		if err != nil {
			return err
		}
		if previous, exists := fileByID(parent.Files, d.ID); exists {
			// 正文变化不能覆盖独立管理的当前目录元数据。
			if previous.RelativePath == file.RelativePath {
				file.RelativePath = d.RelativePath
			}
			if previous.Name == file.Name {
				file.Name = d.Name
			}
			if previous.Description == file.Description {
				file.Description = d.Description
			}
		}
	}
	all := storage
	if scoped, ok := storage.(documentRepositoryStorage); ok {
		all = scoped.Storage
	}
	catalog, err := s.repositoryDocumentsLocked(all, project)
	if err != nil {
		return err
	}
	for _, other := range catalog {
		if other.ID != d.ID && other.Status == 1 && (folderPathContains(other.RelativePath, file.RelativePath) || folderPathContains(file.RelativePath, other.RelativePath)) {
			return ErrAlreadyExists
		}
	}
	file.ObjectKey = ""
	d.File, d.UpdatedAt, d.HeadVersionName = file, c.CreatedAt, c.VersionName
	return repositoryPut(s.requestContext(), storage, project, "document", d.ID, d, record.Generation+1, &record.Generation)
}
