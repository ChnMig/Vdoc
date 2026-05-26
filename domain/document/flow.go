package document

import (
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func Archive(document *Document, now time.Time) error {
	if document == nil {
		return commonvdoc.ErrNotFound
	}
	document.Status = commonvdoc.DocumentStatusArchived
	document.UpdatedAt = now
	return nil
}
