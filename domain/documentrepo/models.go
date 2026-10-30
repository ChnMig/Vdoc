package documentrepo

import (
	"context"
	"encoding/json"
	"time"
)

// Record 让提交图独立于旧 Store 全量快照；查询始终限定项目、种类和游标。
type Record struct {
	ProjectID  string
	Kind       string
	ID         string
	Generation int64
	Sequence   int64
	Payload    json.RawMessage
}

type Storage interface {
	LockRepository(context.Context, string) error
	RepositoryRecord(context.Context, string, string, string) (*Record, error)
	RepositoryRecords(context.Context, string, string, int64, int) ([]Record, error)
	PutRepositoryRecord(context.Context, Record, *int64) error
}

type File struct {
	DocumentID   string `json:"document_id"`
	RelativePath string `json:"relative_path"`
	Name         string `json:"name"`
	DocumentType int    `json:"document_type"`
	Description  string `json:"description,omitempty"`
	ContentHash  string `json:"content_hash"`
	ObjectKey    string `json:"object_key,omitempty"`
}

type Commit struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	ParentIDs  []string  `json:"parent_ids"`
	TreeHash   string    `json:"tree_hash"`
	Message    string    `json:"message"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	Files      []File    `json:"files"`
	Provenance string    `json:"provenance,omitempty"`
}

type HistoryPage struct {
	Commits    []Commit `json:"commits"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

type Branch struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Name           string    `json:"name"`
	HeadCommitID   string    `json:"head_commit_id"`
	Generation     int64     `json:"generation"`
	IsProtected    bool      `json:"is_protected"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	ImportRequired bool      `json:"import_required,omitempty"`
}

type BranchInput struct {
	Name         string `json:"name"`
	FromCommitID string `json:"from_commit_id"`
	IsProtected  bool   `json:"is_protected"`
}

type Change struct {
	Operation    string `json:"operation"`
	DocumentID   string `json:"document_id"`
	RelativePath string `json:"relative_path"`
	Name         string `json:"name"`
	DocumentType int    `json:"document_type"`
	Description  string `json:"description,omitempty"`
	Content      string `json:"content"`
}

type SaveInput struct {
	ExpectedHeadCommitID string   `json:"expected_head_commit_id"`
	ExpectedGeneration   int64    `json:"expected_generation"`
	IdempotencyKey       string   `json:"idempotency_key"`
	Message              string   `json:"message,omitempty"`
	Changes              []Change `json:"changes"`
}

type Content struct {
	CommitID string `json:"commit_id"`
	Content  string `json:"content"`
	Document File   `json:"document"`
}

type MergeContent struct {
	MergeRequestID string `json:"merge_request_id"`
	Generation     int64  `json:"generation"`
	Content        string `json:"content"`
	Document       File   `json:"document"`
}

type CompareInput struct {
	FromCommitID string `json:"from_commit_id"`
	ToCommitID   string `json:"to_commit_id"`
	DocumentID   string `json:"document_id,omitempty"`
}

type FileChange struct {
	DocumentID      string `json:"document_id"`
	RelativePath    string `json:"relative_path"`
	OldRelativePath string `json:"old_relative_path,omitempty"`
	ChangeType      string `json:"change_type"`
	OldContent      string `json:"old_content"`
	NewContent      string `json:"new_content"`
	DocumentType    int    `json:"document_type"`
	SemanticStatus  string `json:"semantic_status,omitempty"`
	SemanticDiff    any    `json:"semantic_diff,omitempty"`
}

type Comparison struct {
	ID           string       `json:"id"`
	FromCommitID string       `json:"from_commit_id"`
	ToCommitID   string       `json:"to_commit_id"`
	Files        []FileChange `json:"files"`
}

type MergeInput struct {
	SourceBranchID         string `json:"source_branch_id"`
	TargetBranchID         string `json:"target_branch_id"`
	Title                  string `json:"title,omitempty"`
	ExpectedSourceCommitID string `json:"expected_source_commit_id,omitempty"`
	ExpectedTargetCommitID string `json:"expected_target_commit_id,omitempty"`
}

type MergeRequest struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	Title             string     `json:"title"`
	SourceBranchID    string     `json:"source_branch_id"`
	TargetBranchID    string     `json:"target_branch_id"`
	SourceCommitID    string     `json:"source_commit_id"`
	TargetCommitID    string     `json:"target_commit_id"`
	SourceGeneration  int64      `json:"source_generation"`
	TargetGeneration  int64      `json:"target_generation"`
	BaseCommitID      string     `json:"base_commit_id"`
	ResultFiles       []File     `json:"result_files"`
	Conflicts         []string   `json:"conflicts"`
	Status            string     `json:"status"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	ApprovedBy        string     `json:"approved_by,omitempty"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	MergedCommitID    string     `json:"merged_commit_id,omitempty"`
	Generation        int64      `json:"generation"`
	ResolvesRequestID string     `json:"resolves_request_id,omitempty"`
}

// ResolveInput 是完整的合并结果树；缺席的文档表示删除，结果以新提交和新审核轮次保存。
type ResolveInput struct {
	ExpectedGeneration       int64    `json:"expected_generation"`
	ExpectedSourceCommitID   string   `json:"expected_source_commit_id"`
	ExpectedTargetCommitID   string   `json:"expected_target_commit_id"`
	ExpectedSourceGeneration int64    `json:"expected_source_generation"`
	ExpectedTargetGeneration int64    `json:"expected_target_generation"`
	IdempotencyKey           string   `json:"idempotency_key"`
	Message                  string   `json:"message,omitempty"`
	Files                    []Change `json:"files"`
}

type RevertInput struct {
	CommitID             string `json:"commit_id"`
	ExpectedHeadCommitID string `json:"expected_head_commit_id"`
	ExpectedGeneration   int64  `json:"expected_generation"`
	IdempotencyKey       string `json:"idempotency_key"`
	ParentIndex          int    `json:"parent_index,omitempty"`
}

type PublishInput struct {
	BranchID           string `json:"branch_id"`
	CommitID           string `json:"commit_id"`
	ExpectedGeneration int64  `json:"expected_generation"`
	Message            string `json:"message,omitempty"`
}

type Release struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	BranchID    string    `json:"branch_id"`
	CommitID    string    `json:"commit_id"`
	VersionName string    `json:"version_name"`
	Message     string    `json:"message"`
	PublishedBy string    `json:"published_by"`
	PublishedAt time.Time `json:"published_at"`
}

type Event struct {
	ID             string    `json:"id"`
	Sequence       int64     `json:"sequence"`
	EventType      string    `json:"event_type"`
	ProjectID      string    `json:"project_id"`
	BranchID       string    `json:"branch_id,omitempty"`
	BeforeCommitID string    `json:"before_commit_id,omitempty"`
	AfterCommitID  string    `json:"after_commit_id,omitempty"`
	RefGeneration  int64     `json:"ref_generation,omitempty"`
	ActorID        string    `json:"actor_id"`
	CreatedAt      time.Time `json:"created_at"`
	ResourceID     string    `json:"resource_id,omitempty"`
}
