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
	// Validate and assemble the complete update on a copy first. Callers keep a
	// pointer to the aggregate, so mutating fields before every validation has
	// passed would otherwise leave a partially updated document on error.
	updated := *params.Document
	if strings.TrimSpace(params.Name) != "" {
		name, err := normalizeName(params.Name)
		if err != nil {
			return err
		}
		if nameExists(params.Existing, params.Document.ID, params.Document.ProjectID, name) {
			return commonvdoc.ErrAlreadyExists
		}
		updated.Name = name
	}
	if params.RelativePath != "" {
		if err := commonvdoc.ValidateDocumentRelativePath(params.Document.DocumentType, params.RelativePath); err != nil {
			return fmt.Errorf("%w: %v", commonvdoc.ErrInvalidArgument, err)
		}
		updated.RelativePath = params.RelativePath
	}
	updated.DisplayName = params.DisplayName
	updated.Description = params.Description
	updated.BasePath = params.BasePath
	updated.UpdatedAt = params.Now
	*params.Document = updated
	return nil
}
