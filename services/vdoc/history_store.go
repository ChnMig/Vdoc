package vdoc

import (
	"sort"
	"strings"
	domain "vdoc/domain/vdoc"
)

func (s *Store) historyReadScope(actorID, projectID, documentID string) (*Store, domain.HistoryReadRepository, error) {
	local, reader, err := s.readScope(domain.ReadScope{ActorID: actorID, ProjectID: projectID, DocumentID: documentID})
	if err != nil {
		return nil, nil, err
	}
	if local == nil {
		return nil, nil, nil
	}
	if !local.canReadLocked(actorID, projectID) {
		return nil, nil, ErrPermissionDenied
	}
	if !local.serviceInProjectLocked(documentID, projectID) {
		return nil, nil, ErrNotFound
	}
	repo, _ := reader.(domain.HistoryReadRepository)
	return local, repo, nil
}

func (s *Store) QueryDrafts(actorID, projectID, documentID, branchID string, page PageQuery) ([]*ContractDraft, int, error) {
	if err := page.Validate(); err != nil {
		return nil, 0, err
	}
	_, repo, err := s.historyReadScope(actorID, projectID, documentID)
	if err != nil {
		return nil, 0, err
	}
	if repo != nil {
		items, total, err := repo.ReadDraftPage(s.requestContext(), documentID, strings.TrimSpace(branchID), page)
		for _, item := range items {
			item.ProjectID = projectID
		}
		return items, total, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, 0, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, 0, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(documentID, projectID) {
		return nil, 0, ErrNotFound
	}
	items := []*ContractDraft{}
	for _, draft := range s.drafts {
		if draft.ServiceID != documentID || (branchID != "" && draft.BranchID != branchID) || !s.branchInServiceLocked(draft.BranchID, documentID) || !strings.Contains(strings.ToLower(draft.VersionName), strings.ToLower(page.Search)) {
			continue
		}
		copy := *draft
		copy.RawSchema, copy.NormalizedSchema, copy.DiffPreview = "", "", nil
		items = append(items, &copy)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	total := len(items)
	return items[min(page.Offset, total):min(page.Offset+page.Limit, total)], total, nil
}

func (s *Store) QueryDocumentDiffs(actorID, projectID, documentID, fromID, toID string, page PageQuery) ([]*Diff, int, error) {
	if err := page.Validate(); err != nil {
		return nil, 0, err
	}
	_, repo, err := s.historyReadScope(actorID, projectID, documentID)
	if err != nil {
		return nil, 0, err
	}
	if repo != nil {
		return repo.ReadDiffPage(s.requestContext(), documentID, strings.TrimSpace(fromID), strings.TrimSpace(toID), page)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, 0, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, 0, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(documentID, projectID) {
		return nil, 0, ErrNotFound
	}
	items := []*Diff{}
	for _, diff := range s.diffs {
		if diff.ServiceID != documentID || (fromID != "" && diff.FromVersionID != fromID) || (toID != "" && diff.ToVersionID != toID) {
			continue
		}
		from, to := s.versions[diff.FromVersionID], s.versions[diff.ToVersionID]
		if from == nil || to == nil || from.ServiceID != documentID || to.ServiceID != documentID {
			continue
		}
		if page.Search != "" && !strings.Contains(strings.ToLower(from.VersionName), strings.ToLower(page.Search)) && !strings.Contains(strings.ToLower(to.VersionName), strings.ToLower(page.Search)) {
			continue
		}
		copy := *diff
		copy.Items = nil
		items = append(items, &copy)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	total := len(items)
	return items[min(page.Offset, total):min(page.Offset+page.Limit, total)], total, nil
}
