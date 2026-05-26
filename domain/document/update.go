package document

import (
	"fmt"
	"strings"

	commonvdoc "vdoc/common/vdoc"
)

func Update(params UpdateParams) error {
	if params.Document == nil {
		return commonvdoc.ErrNotFound
	}
	if params.Document.Status == commonvdoc.DocumentStatusArchived {
		return commonvdoc.ErrFailedPrecondition
	}
	if strings.TrimSpace(params.Name) != "" {
		name, err := normalizeName(params.Name)
		if err != nil {
			return err
		}
		if nameExists(params.Existing, params.Document.ID, params.Document.ProjectID, name) {
			return commonvdoc.ErrAlreadyExists
		}
		params.Document.Name = name
	}
	if params.RelativePath != "" {
		if err := commonvdoc.ValidateDocumentRelativePath(params.Document.DocumentType, params.RelativePath); err != nil {
			return fmt.Errorf("%w: %v", commonvdoc.ErrInvalidArgument, err)
		}
		params.Document.RelativePath = params.RelativePath
	}
	params.Document.DisplayName = params.DisplayName
	params.Document.Description = params.Description
	params.Document.BasePath = params.BasePath
	params.Document.UpdatedAt = params.Now
	return nil
}
