package vdoc

import (
	"sort"
	"time"
	domain "vdoc/domain/vdoc"
)

// 活动只投影文档工作流字段；不返回审计元数据、令牌、IP 或客户端信息。
type DocumentActivity struct {
	ID           string
	Action       string
	ResourceID   string
	BranchID     string
	BranchName   string
	VersionName  string
	ReviewAction string
	CreatedAt    time.Time
}
type DocumentActivityPage struct {
	Items      []DocumentActivity
	NextCursor string
}

var documentActivityActions = []string{
	"api_service.create", "document.create", "document.update", "document.archive",
	"contract_branch.create", "contract_branch.update", "contract_branch.archive",
	"contract_draft.create", "contract_draft.update", "contract_draft.promote", "contract_draft.submit", "contract_draft.review",
	"markdown_draft.create", "markdown_draft.update", "markdown_draft.submit", "markdown_draft.review", "document_version.publish",
}

func (s *Store) QueryDocumentActivity(actorID, projectID, documentID, cursor string, limit int) (*DocumentActivityPage, error) {
	query, err := normalizeAuditQuery(AuditLogQuery{Limit: limit, Cursor: cursor})
	if err != nil {
		return nil, err
	}
	local, repo, err := s.readScope(domain.ReadScope{ActorID: actorID, ProjectID: projectID, DocumentID: documentID})
	if err != nil {
		return nil, err
	}
	var page *domain.AuditPage
	if local != nil {
		if !local.canReadLocked(actorID, projectID) {
			return nil, ErrPermissionDenied
		}
		if !local.serviceInProjectLocked(documentID, projectID) {
			return nil, ErrNotFound
		}
		page, err = repo.ReadAudits(s.requestContext(), domain.AuditQuery{ProjectID: projectID, DocumentID: documentID, Actions: documentActivityActions, Limit: query.Limit, Cursor: cursor})
		if err != nil {
			return nil, err
		}
	} else {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.refreshLocked(); err != nil {
			return nil, err
		}
		if !s.canReadLocked(actorID, projectID) {
			return nil, ErrPermissionDenied
		}
		if !s.serviceInProjectLocked(documentID, projectID) {
			return nil, ErrNotFound
		}
		local = s
		after, _ := domain.DecodeAuditCursor(cursor)
		allowed := make(map[string]bool, len(documentActivityActions))
		for _, action := range documentActivityActions {
			allowed[action] = true
		}
		items := []*AuditLog{}
		for _, log := range s.audits {
			if log.ProjectID != projectID || log.ServiceID != documentID || !allowed[log.Action] {
				continue
			}
			if cursor != "" && (log.CreatedAt.After(after.Time) || (log.CreatedAt.Equal(after.Time) && log.ID >= after.ID)) {
				continue
			}
			items = append(items, log)
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].ID > items[j].ID
			}
			return items[i].CreatedAt.After(items[j].CreatedAt)
		})
		page = &domain.AuditPage{Items: items[:min(len(items), query.Limit)]}
		if len(items) > query.Limit {
			page.NextCursor = domain.EncodeAuditCursor(page.Items[len(page.Items)-1])
		}
	}
	result := &DocumentActivityPage{Items: make([]DocumentActivity, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, log := range page.Items {
		branchID := log.Metadata["branch_id"]
		if log.ResourceType == "contract_branch" {
			branchID = log.ResourceID
		}
		branchName := ""
		if branch := local.branches[branchID]; branch != nil && branch.ServiceID == documentID {
			branchName = branch.Name
		}
		result.Items = append(result.Items, DocumentActivity{ID: log.ID, Action: log.Action, ResourceID: log.ResourceID, BranchID: branchID, BranchName: branchName, VersionName: log.Metadata["version_name"], ReviewAction: log.Metadata["review_action"], CreatedAt: log.CreatedAt})
	}
	return result, nil
}

func (s *Store) draftHistoryCopyLocked(draft *ContractDraft) *ContractDraft {
	copy := cloneDraft(draft)
	for _, version := range s.versions {
		if version.DraftID == draft.ID && version.ServiceID == draft.ServiceID {
			at := version.PublishedAt
			copy.PublishedAt = &at
			break
		}
	}
	return copy
}

func (s *Store) validateDraftSourceLocked(projectID, documentID string, input *DraftInput) error {
	if input.SourceVersionID == "" {
		if input.SourceBranchID != "" {
			return ErrInvalidArgument
		}
		return nil
	}
	version := s.versions[input.SourceVersionID]
	if version == nil || version.ProjectID != projectID || version.ServiceID != documentID || version.Status != VersionStatusPublished {
		return ErrNotFound
	}
	if input.SourceBranchID != "" && version.BranchID != input.SourceBranchID {
		return ErrInvalidArgument
	}
	input.SourceBranchID = version.BranchID
	return nil
}
