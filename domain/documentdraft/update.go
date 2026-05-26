package documentdraft

import commonvdoc "vdoc/common/vdoc"

func CanBeChangedByWriter(status int) bool {
	return status == commonvdoc.DraftStatusDraft || status == commonvdoc.DraftStatusChangesRequested
}

func EnsureWriterCanChange(status int) error {
	if !CanBeChangedByWriter(status) {
		return commonvdoc.ErrFailedPrecondition
	}
	return nil
}
