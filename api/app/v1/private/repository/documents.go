package repository

import (
	"path"

	"github.com/gin-gonic/gin"
	"vdoc/api/app/v1/private/shared"
	app "vdoc/appstore"
	repo "vdoc/domain/documentrepo"
)

type documentDTO struct {
	ID                string `json:"id"`
	ProjectID         string `json:"project_id"`
	Name              string `json:"name"`
	DocumentType      int    `json:"document_type"`
	RelativePath      string `json:"relative_path"`
	FileName          string `json:"file_name"`
	Description       string `json:"description"`
	DefaultBranchID   string `json:"default_branch_id"`
	DefaultBranchName string `json:"default_branch_name"`
	HeadVersionName   string `json:"head_version_name,omitempty"`
	LatestVersionID   string `json:"latest_version_id,omitempty"`
	LatestDraftID     string `json:"latest_draft_id,omitempty"`
	Status            int    `json:"status"`
	CreatedBy         string `json:"created_by"`
	CreatedAt         any    `json:"created_at"`
	UpdatedAt         any    `json:"updated_at"`
}

func documentView(d repo.Document) documentDTO {
	return documentDTO{d.ID, d.ProjectID, d.Name, d.DocumentType, d.RelativePath, path.Base(d.RelativePath), d.Description, d.DefaultBranchID, d.DefaultBranchName, d.HeadVersionName, d.LatestVersionID, d.LatestDraftID, d.Status, d.CreatedBy, d.CreatedAt, d.UpdatedAt}
}

func documents(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	values, err := shared.Store(c).RepositoryDocuments(a, c.Param("project_id"))
	out := []documentDTO{}
	for _, d := range values {
		out = append(out, documentView(d))
	}
	result(c, out, err)
}

func createDocument(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input repo.DocumentInput
	if !bind(c, &input) {
		return
	}
	value, err := shared.Store(c).RepositoryCreateDocument(a, c.Param("project_id"), input)
	if err != nil {
		result(c, nil, err)
		return
	}
	result(c, struct {
		Document documentDTO `json:"document"`
		Branch   branchDTO   `json:"branch"`
		Commit   commitDTO   `json:"commit"`
	}{documentView(value.Document), branchView(value.Branch), commitView(&value.Commit)}, nil)
}

func document(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	value, err := shared.Store(c).RepositoryDocument(a, c.Param("project_id"), c.Param("document_id"))
	if err != nil {
		result(c, nil, err)
		return
	}
	result(c, documentView(*value), nil)
}

func updateDocument(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input app.RepositoryDocumentPatchInput
	if !bind(c, &input) {
		return
	}
	value, err := shared.Store(c).RepositoryUpdateDocument(a, c.Param("project_id"), c.Param("document_id"), input, shared.AuditContextFromGin(c))
	if err != nil {
		result(c, nil, err)
		return
	}
	result(c, documentView(*value), nil)
}

func deleteDocument(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	err := shared.Store(c).RepositoryDeleteDocument(a, c.Param("project_id"), c.Param("document_id"), shared.AuditContextFromGin(c))
	result(c, struct {
		Deleted bool `json:"deleted"`
	}{err == nil}, err)
}
