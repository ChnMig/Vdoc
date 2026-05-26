package document

import "time"

type Document struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Name         string    `json:"name"`
	DocumentType int       `json:"document_type,omitempty"`
	RelativePath string    `json:"relative_path,omitempty"`
	DisplayName  string    `json:"display_name,omitempty"`
	Description  string    `json:"description,omitempty"`
	BasePath     string    `json:"base_path,omitempty"`
	Status       int       `json:"status"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type APIService = Document
