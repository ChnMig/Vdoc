package document

import commonvdoc "vdoc/common/vdoc"

func InProject(document *Document, projectID string) bool {
	return document != nil && document.ProjectID == projectID
}

func ActiveInProject(document *Document, projectID string) bool {
	return InProject(document, projectID) && document.Status != commonvdoc.DocumentStatusArchived
}
