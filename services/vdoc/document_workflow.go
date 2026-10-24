package vdoc

import "time"

func (s *Store) projectWithTeamLocked(project *Project) *Project {
	copied := cloneProject(project)
	if copied != nil {
		if team := s.teams[copied.TeamID]; team != nil {
			copied.TeamName = team.Name
		}
	}
	return copied
}

func (s *Store) documentWithWorkflowLocked(actorID string, document *APIService) *APIService {
	copied := cloneService(document)
	if copied != nil {
		s.enrichDocumentWorkflowLocked(actorID, copied.ProjectID, []*APIService{copied})
	}
	return copied
}

// 每个项目只扫描一次草稿和版本集合，不为目录树中的每个文件额外查询。
func (s *Store) enrichDocumentWorkflowLocked(actorID, projectID string, documents []*APIService) {
	if !s.canReadLocked(actorID, projectID) {
		return
	}
	byID := make(map[string]*APIService, len(documents))
	for _, document := range documents {
		document.WorkflowState, document.HasDraft = "empty", false
		document.LatestVersionID, document.LatestDraftID = "", ""
		byID[document.ID] = document
	}
	latestVersionTimes, latestDraftTimes := map[string]time.Time{}, map[string]time.Time{}
	for _, version := range s.versions {
		document := byID[version.DocumentID]
		if document == nil {
			document = byID[version.ServiceID]
		}
		if document == nil || version.ProjectID != projectID || version.Status != VersionStatusPublished {
			continue
		}
		if document.LatestVersionID == "" || version.PublishedAt.After(latestVersionTimes[document.ID]) || (version.PublishedAt.Equal(latestVersionTimes[document.ID]) && version.ID > document.LatestVersionID) {
			document.LatestVersionID, latestVersionTimes[document.ID] = version.ID, version.PublishedAt
		}
		document.WorkflowState = "published"
	}
	for _, draft := range s.drafts {
		document := byID[draft.DocumentID]
		if document == nil {
			document = byID[draft.ServiceID]
		}
		if document == nil || draft.ProjectID != projectID || draft.Status == DraftStatusPublished || !s.branchInServiceLocked(draft.BranchID, document.ID) {
			continue
		}
		document.HasDraft = true
		if document.LatestDraftID == "" || draft.UpdatedAt.After(latestDraftTimes[document.ID]) || (draft.UpdatedAt.Equal(latestDraftTimes[document.ID]) && draft.ID > document.LatestDraftID) {
			document.LatestDraftID, latestDraftTimes[document.ID] = draft.ID, draft.UpdatedAt
		}
		if document.LatestVersionID == "" {
			document.WorkflowState = "draft"
		}
	}
}
