package document

import "time"

type CreateParams struct {
	ID           string
	ProjectID    string
	Name         string
	DocumentType int
	RelativePath string
	DisplayName  string
	Description  string
	BasePath     string
	ActorID      string
	Now          time.Time
	Existing     []*Document
}

type UpdateParams struct {
	Document     *Document
	Name         string
	DisplayName  string
	Description  string
	RelativePath string
	BasePath     string
	Now          time.Time
	Existing     []*Document
}
