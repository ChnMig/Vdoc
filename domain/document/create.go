package document

import (
	"fmt"
	"strings"

	commonvdoc "vdoc/common/vdoc"
)

func Create(params CreateParams) (*Document, error) {
	name, err := normalizeName(params.Name)
	if err != nil {
		return nil, err
	}
	documentType := params.DocumentType
	if documentType == 0 {
		documentType = commonvdoc.DocumentTypeOpenAPI
	}
	if params.RelativePath != "" {
		if err := commonvdoc.ValidateDocumentRelativePath(documentType, params.RelativePath); err != nil {
			return nil, fmt.Errorf("%w: %v", commonvdoc.ErrInvalidArgument, err)
		}
	}
	if nameExists(params.Existing, "", params.ProjectID, name) {
		return nil, commonvdoc.ErrAlreadyExists
	}
	return &Document{ID: params.ID, ProjectID: params.ProjectID, Name: name, DocumentType: documentType, RelativePath: params.RelativePath, DisplayName: params.DisplayName, Description: params.Description, BasePath: params.BasePath, Status: commonvdoc.DocumentStatusActive, CreatedBy: params.ActorID, CreatedAt: params.Now, UpdatedAt: params.Now}, nil
}

func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", commonvdoc.ErrInvalidArgument
	}
	return name, nil
}

func nameExists(documents []*Document, currentID, projectID, name string) bool {
	for _, document := range documents {
		if document == nil || document.ID == currentID || document.ProjectID != projectID {
			continue
		}
		if strings.EqualFold(document.Name, name) {
			return true
		}
	}
	return false
}
