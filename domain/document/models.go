package document

import "time"

type Document struct {
	// 工作流信息只在有权限的读取视图中补充，不写入文档元数据表。
	WorkflowState   string    `json:"workflow_state,omitempty"`
	HasDraft        bool      `json:"has_draft"`
	LatestVersionID string    `json:"latest_version_id,omitempty"`
	LatestDraftID   string    `json:"latest_draft_id,omitempty"`
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	FolderID        string    `json:"folder_id,omitempty"`
	Name            string    `json:"name"`
	DocumentType    int       `json:"document_type,omitempty"`
	RelativePath    string    `json:"relative_path,omitempty"`
	DisplayName     string    `json:"display_name,omitempty"`
	Description     string    `json:"description,omitempty"`
	BasePath        string    `json:"base_path,omitempty"`
	Status          int       `json:"status"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type APIService = Document
