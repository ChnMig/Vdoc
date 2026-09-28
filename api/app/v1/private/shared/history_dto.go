package shared

import (
	"time"
	app "vdoc/appstore"
)

type DraftListDTO struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	DocumentID     string     `json:"document_id"`
	BranchID       string     `json:"branch_id"`
	VersionName    string     `json:"version_name"`
	Changelog      string     `json:"changelog,omitempty"`
	DocumentFormat int        `json:"document_format"`
	SourceType     int        `json:"source_type"`
	Status         int        `json:"status"`
	CreatedBy      string     `json:"created_by"`
	SubmittedAt    *time.Time `json:"submitted_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func DraftList(values []*app.ContractDraft) []DraftListDTO {
	out := make([]DraftListDTO, 0, len(values))
	for _, v := range values {
		out = append(out, DraftListDTO{ID: v.ID, ProjectID: v.ProjectID, DocumentID: v.DocumentID, BranchID: v.BranchID, VersionName: v.VersionName, Changelog: v.Changelog, DocumentFormat: v.SchemaFormat, SourceType: v.SourceType, Status: v.Status, CreatedBy: v.CreatedBy, SubmittedAt: v.SubmittedAt, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt})
	}
	return out
}

type DiffListDTO struct {
	ID            string    `json:"id"`
	DocumentID    string    `json:"document_id"`
	FromVersionID string    `json:"from_version_id"`
	ToVersionID   string    `json:"to_version_id"`
	DiffStatus    int       `json:"diff_status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func DiffList(values []*app.Diff) []DiffListDTO {
	out := make([]DiffListDTO, 0, len(values))
	for _, v := range values {
		out = append(out, DiffListDTO{ID: v.ID, DocumentID: v.DocumentID, FromVersionID: v.FromVersionID, ToVersionID: v.ToVersionID, DiffStatus: v.DiffStatus, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt})
	}
	return out
}
