package vdoc

import (
	"fmt"
	"strings"

	domain "vdoc/domain/vdoc"
)

type PageQuery = domain.PageQuery
type AuditLogPage = domain.AuditPage

func (s *Store) readScope(scope domain.ReadScope) (*Store, domain.ReadRepository, error) {
	if s.persistence == nil {
		return nil, nil, nil
	}
	repo, ok := s.persistence.repo.(domain.ReadRepository)
	if !ok {
		return nil, nil, nil
	}
	state, err := repo.LoadReadState(s.requestContext(), scope)
	if err != nil {
		return nil, nil, err
	}
	local := &Store{storeState: &storeState{}, ctx: s.requestContext()}
	local.applyStateLocked(state)
	local.objects = s.objects
	return local, repo, nil
}

func (s *Store) QueryDocumentVersions(actorID, projectID, documentID, branchID string, query PageQuery) ([]*ContractVersion, int, error) {
	if err := query.Validate(); err != nil {
		return nil, 0, err
	}
	local, repo, err := s.readScope(domain.ReadScope{ActorID: actorID, ProjectID: projectID, DocumentID: documentID})
	if err != nil {
		return nil, 0, err
	}
	if local != nil {
		if !local.canReadLocked(actorID, projectID) {
			return nil, 0, ErrPermissionDenied
		}
		if !local.serviceInProjectLocked(documentID, projectID) {
			return nil, 0, ErrNotFound
		}
		return repo.ReadVersions(s.requestContext(), documentID, branchID, query)
	}
	items, err := s.ListDocumentVersions(actorID, projectID, documentID, branchID)
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]*ContractVersion, 0)
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.VersionName), strings.ToLower(query.Search)) {
			filtered = append(filtered, item)
		}
	}
	total := len(filtered)
	return filtered[min(query.Offset, total):min(query.Offset+query.Limit, total)], total, nil
}

func (s *Store) QueryDocumentEndpoints(actorID, projectID, documentID, versionID string, query PageQuery) ([]*Endpoint, int, error) {
	if err := query.Validate(); err != nil {
		return nil, 0, err
	}
	local, repo, err := s.readScope(domain.ReadScope{ActorID: actorID, ProjectID: projectID, DocumentID: documentID, VersionID: versionID})
	if err != nil {
		return nil, 0, err
	}
	if local != nil {
		if !local.canReadLocked(actorID, projectID) {
			return nil, 0, ErrPermissionDenied
		}
		if !local.serviceInProjectLocked(documentID, projectID) || !local.versionInProjectLocked(projectID, documentID, versionID) {
			return nil, 0, ErrNotFound
		}
		if err := local.ensureOpenAPIDocumentLocked(documentID); err != nil {
			return nil, 0, err
		}
		return repo.ReadEndpoints(s.requestContext(), versionID, query)
	}
	items, err := s.ListDocumentEndpoints(actorID, projectID, documentID, versionID, "")
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]*Endpoint, 0)
	for _, item := range items {
		text := item.Method + " " + item.Path + " " + item.Summary + " " + item.OperationID + " " + strings.Join(item.Tags, " ")
		if strings.Contains(item.Path, query.Path) && strings.Contains(strings.ToLower(text), strings.ToLower(query.Search)) {
			filtered = append(filtered, item)
		}
	}
	total := len(filtered)
	return filtered[min(query.Offset, total):min(query.Offset+query.Limit, total)], total, nil
}

func normalizeAuditQuery(query AuditLogQuery) (AuditLogQuery, error) {
	query.ProjectID = strings.TrimSpace(query.ProjectID)
	query.Action = strings.TrimSpace(query.Action)
	query.ResourceType = strings.TrimSpace(query.ResourceType)
	query.ResourceID = strings.TrimSpace(query.ResourceID)
	if query.Limit < 0 {
		return query, ErrInvalidArgument
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	query.Limit = min(query.Limit, 200)
	if _, err := domain.DecodeAuditCursor(query.Cursor); err != nil {
		return query, err
	}
	if query.From != nil && query.To != nil && !query.From.Before(*query.To) {
		return query, fmt.Errorf("%w: from must precede to", ErrInvalidArgument)
	}
	return query, nil
}

func (s *Store) QueryAuditLogs(actorID string, query AuditLogQuery) ([]*AuditLog, error) {
	page, err := s.QueryAuditLogPage(actorID, query)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (s *Store) QueryAuditLogPage(actorID string, query AuditLogQuery) (*AuditLogPage, error) {
	query, err := normalizeAuditQuery(query)
	if err != nil {
		return nil, err
	}
	local, repo, err := s.readScope(domain.ReadScope{ActorID: actorID, ProjectID: query.ProjectID})
	if err != nil {
		return nil, err
	}
	if local != nil {
		actor := local.users[actorID]
		if actor == nil || actor.Status != UserStatusActive {
			return nil, ErrUnauthenticated
		}
		if !actor.IsSuperAdmin {
			if query.ProjectID == "" {
				return nil, ErrInvalidArgument
			}
			if !local.canManageProjectLocked(actorID, query.ProjectID) {
				return nil, ErrPermissionDenied
			}
		}
		return repo.ReadAudits(s.requestContext(), domain.AuditQuery{ProjectID: query.ProjectID, Action: query.Action, ResourceType: query.ResourceType, ResourceID: query.ResourceID, Limit: query.Limit, Cursor: query.Cursor, From: query.From, To: query.To})
	}
	limit := query.Limit
	query.Limit++
	items, err := s.queryAuditLogsMemory(actorID, query)
	if err != nil {
		return nil, err
	}
	page := &AuditLogPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = domain.EncodeAuditCursor(page.Items[limit-1])
	}
	return page, nil
}

func (s *Store) QueryMCPUsage(actorID string, query MCPUsageQuery) ([]*AuditLog, error) {
	query.TokenID = strings.TrimSpace(query.TokenID)
	if query.Limit < 0 {
		return nil, ErrInvalidArgument
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	query.Limit = min(query.Limit, 200)
	local, repo, err := s.readScope(domain.ReadScope{ActorID: actorID, Tokens: true, TokenID: query.TokenID})
	if err != nil {
		return nil, err
	}
	if local == nil {
		return s.queryMCPUsageMemory(actorID, query)
	}
	actor := local.users[actorID]
	if actor == nil || actor.Status != UserStatusActive {
		return nil, ErrUnauthenticated
	}
	tokenIDs := make([]string, 0)
	if query.TokenID != "" {
		token := local.tokens[query.TokenID]
		if token == nil {
			return nil, ErrNotFound
		}
		if !actor.IsSuperAdmin && token.UserID != actorID {
			return nil, ErrPermissionDenied
		}
		tokenIDs = append(tokenIDs, token.ID)
	} else {
		for _, token := range local.tokens {
			if token.UserID == actorID {
				tokenIDs = append(tokenIDs, token.ID)
			}
		}
	}
	page, err := repo.ReadAudits(s.requestContext(), domain.AuditQuery{Action: "mcp.tool_call", TokenIDs: tokenIDs, Limit: query.Limit})
	if err != nil {
		return nil, err
	}
	for i, audit := range page.Items {
		page.Items[i] = sanitizedMCPUsageAudit(audit)
	}
	return page.Items, nil
}
