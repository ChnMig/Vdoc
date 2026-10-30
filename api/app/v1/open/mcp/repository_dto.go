package mcp

import (
	"time"
	app "vdoc/appstore"
	"vdoc/domain/documentrepo"
)

// MCP DTO 显式选择对外字段，提交文件不能泄露内部对象存储键。
type mcpRepositoryFile struct {
	DocumentID   string `json:"document_id"`
	RelativePath string `json:"relative_path"`
	Name         string `json:"name"`
	DocumentType int    `json:"document_type"`
	Description  string `json:"description,omitempty"`
	ContentHash  string `json:"content_hash"`
}

type mcpRepositoryCommit struct {
	ID            string              `json:"id"`
	ProjectID     string              `json:"project_id"`
	ParentIDs     []string            `json:"parent_ids"`
	TreeHash      string              `json:"tree_hash"`
	Message       string              `json:"message"`
	CreatedBy     string              `json:"created_by"`
	CreatedAt     time.Time           `json:"created_at"`
	Provenance    string              `json:"provenance,omitempty"`
	Files         []mcpRepositoryFile `json:"files"`
	FilesFiltered bool                `json:"files_filtered"`
}

type mcpRepositoryBranch struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Name         string    `json:"name"`
	HeadCommitID string    `json:"head_commit_id"`
	Generation   int64     `json:"generation"`
	IsProtected  bool      `json:"is_protected"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
}

type mcpRepositoryHistory struct {
	Commits    []mcpRepositoryCommit `json:"commits"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

type mcpRepositoryContent struct {
	CommitID string            `json:"commit_id"`
	Content  string            `json:"content"`
	Document mcpRepositoryFile `json:"document"`
}

type mcpRepositoryComparisonFile struct {
	DocumentID      string      `json:"document_id"`
	RelativePath    string      `json:"relative_path"`
	OldRelativePath string      `json:"old_relative_path,omitempty"`
	ChangeType      string      `json:"change_type"`
	OldContent      string      `json:"old_content"`
	NewContent      string      `json:"new_content"`
	DocumentType    int         `json:"document_type"`
	SemanticStatus  string      `json:"semantic_status,omitempty"`
	SemanticDiff    *mcpDiffDTO `json:"semantic_diff,omitempty"`
}

type mcpRepositoryComparison struct {
	ID            string                        `json:"id"`
	FromCommitID  string                        `json:"from_commit_id"`
	ToCommitID    string                        `json:"to_commit_id"`
	Files         []mcpRepositoryComparisonFile `json:"files"`
	FilesFiltered bool                          `json:"files_filtered"`
}

type mcpRepositoryRelease struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	BranchID    string    `json:"branch_id"`
	CommitID    string    `json:"commit_id"`
	VersionName string    `json:"version_name"`
	Message     string    `json:"message"`
	PublishedBy string    `json:"published_by"`
	PublishedAt time.Time `json:"published_at"`
}

type mcpRepositoryRef struct {
	Kind    string                `json:"kind"`
	Branch  *mcpRepositoryBranch  `json:"branch,omitempty"`
	Release *mcpRepositoryRelease `json:"release,omitempty"`
	Commit  mcpRepositoryCommit   `json:"commit"`
}

type mcpRepositoryMerge struct {
	ID               string              `json:"id"`
	ProjectID        string              `json:"project_id"`
	Title            string              `json:"title"`
	SourceBranchID   string              `json:"source_branch_id"`
	TargetBranchID   string              `json:"target_branch_id"`
	SourceCommitID   string              `json:"source_commit_id"`
	TargetCommitID   string              `json:"target_commit_id"`
	SourceGeneration int64               `json:"source_generation"`
	TargetGeneration int64               `json:"target_generation"`
	BaseCommitID     string              `json:"base_commit_id"`
	ResultFiles      []mcpRepositoryFile `json:"result_files"`
	Conflicts        []string            `json:"conflicts"`
	Status           string              `json:"status"`
	CreatedBy        string              `json:"created_by"`
	CreatedAt        time.Time           `json:"created_at"`
	ApprovedBy       string              `json:"approved_by,omitempty"`
	ApprovedAt       *time.Time          `json:"approved_at,omitempty"`
	MergedCommitID   string              `json:"merged_commit_id,omitempty"`
	Generation       int64               `json:"generation"`
	FilesFiltered    bool                `json:"files_filtered"`
}

type mcpRepositoryEvent struct {
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

func repositoryFileDTO(file documentrepo.File) mcpRepositoryFile {
	return mcpRepositoryFile{DocumentID: file.DocumentID, RelativePath: file.RelativePath, Name: file.Name, DocumentType: file.DocumentType, Description: file.Description, ContentHash: file.ContentHash}
}

func repositoryFilesDTO(files []documentrepo.File, scopes []int) []mcpRepositoryFile {
	result := make([]mcpRepositoryFile, 0, len(files))
	for _, file := range files {
		if repositoryReadScope(scopes, file.DocumentType) {
			result = append(result, repositoryFileDTO(file))
		}
	}
	return result
}

func repositoryCommitDTO(commit *documentrepo.Commit, scopes []int) mcpRepositoryCommit {
	files := repositoryFilesDTO(commit.Files, scopes)
	return mcpRepositoryCommit{ID: commit.ID, ProjectID: commit.ProjectID, ParentIDs: append([]string{}, commit.ParentIDs...), TreeHash: commit.TreeHash, Message: commit.Message, CreatedBy: commit.CreatedBy, CreatedAt: commit.CreatedAt, Provenance: commit.Provenance, Files: files, FilesFiltered: len(files) != len(commit.Files)}
}

func repositoryBranchDTO(branch documentrepo.Branch) mcpRepositoryBranch {
	return mcpRepositoryBranch{ID: branch.ID, ProjectID: branch.ProjectID, Name: branch.Name, HeadCommitID: branch.HeadCommitID, Generation: branch.Generation, IsProtected: branch.IsProtected, CreatedBy: branch.CreatedBy, CreatedAt: branch.CreatedAt}
}

func repositoryReleaseDTO(release documentrepo.Release) mcpRepositoryRelease {
	return mcpRepositoryRelease{ID: release.ID, ProjectID: release.ProjectID, BranchID: release.BranchID, CommitID: release.CommitID, VersionName: release.VersionName, Message: release.Message, PublishedBy: release.PublishedBy, PublishedAt: release.PublishedAt}
}

func repositoryMergeDTO(request documentrepo.MergeRequest, scopes []int) mcpRepositoryMerge {
	files := repositoryFilesDTO(request.ResultFiles, scopes)
	visibleIDs := map[string]bool{}
	for _, file := range files {
		visibleIDs[file.DocumentID] = true
	}
	conflicts := make([]string, 0, len(request.Conflicts))
	filtered := len(files) != len(request.ResultFiles)
	for _, conflict := range request.Conflicts {
		if conflict == "path-conflict" || visibleIDs[conflict] || repositoryReadScope(scopes, app.DocumentTypeOpenAPI) && repositoryReadScope(scopes, app.DocumentTypeMarkdown) {
			conflicts = append(conflicts, conflict)
		} else {
			filtered = true
		}
	}
	return mcpRepositoryMerge{ID: request.ID, ProjectID: request.ProjectID, Title: request.Title, SourceBranchID: request.SourceBranchID, TargetBranchID: request.TargetBranchID, SourceCommitID: request.SourceCommitID, TargetCommitID: request.TargetCommitID, SourceGeneration: request.SourceGeneration, TargetGeneration: request.TargetGeneration, BaseCommitID: request.BaseCommitID, ResultFiles: files, Conflicts: conflicts, Status: request.Status, CreatedBy: request.CreatedBy, CreatedAt: request.CreatedAt, ApprovedBy: request.ApprovedBy, ApprovedAt: request.ApprovedAt, MergedCommitID: request.MergedCommitID, Generation: request.Generation, FilesFiltered: filtered}
}
