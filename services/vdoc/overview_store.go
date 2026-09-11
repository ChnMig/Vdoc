package vdoc

import (
	"slices"
	"sort"
	"strings"
	"time"

	domain "vdoc/domain/vdoc"
)

func (s *Store) DocumentOverview(actorID, projectID, documentID string) (*domain.DocumentOverview, error) {
	local, repo, err := s.readScope(domain.ReadScope{ActorID: actorID, ProjectID: projectID, DocumentID: documentID})
	if err != nil {
		return nil, err
	}
	if local != nil {
		if !local.canReadLocked(actorID, projectID) {
			return nil, ErrPermissionDenied
		}
		if !local.serviceInProjectLocked(documentID, projectID) {
			return nil, ErrNotFound
		}
		if overviewRepo, ok := repo.(domain.OverviewRepository); ok {
			out, err := overviewRepo.ReadDocumentOverview(s.requestContext(), documentID)
			if err != nil {
				return nil, err
			}
			if out.LatestVersion != nil && out.RawLineCount == nil && local.apiServices[documentID].DocumentType == DocumentTypeMarkdown {
				version := out.LatestVersion
				if err := local.hydrateVersionContentLocked(s.requestContext(), version, "raw"); err != nil {
					return nil, err
				}
				lines := int64(strings.Count(version.RawSchema, "\n") + 1)
				out.RawLineCount, out.RawSizeBytes = &lines, int64(len(version.RawSchema))
				if err := overviewRepo.SaveVersionContentStats(s.requestContext(), version.ID, version.RawSchemaHash, out.RawSizeBytes, lines); err != nil {
					return nil, err
				}
				version.RawSchema = ""
			}
			return out, nil
		}
	}
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
	out := &domain.DocumentOverview{PublishedBranchIDs: []string{}}
	branches := map[string]bool{}
	for _, version := range s.versions {
		if version.ServiceID != documentID {
			continue
		}
		out.VersionCount++
		if out.LatestVersion == nil || version.PublishedAt.After(out.LatestVersion.PublishedAt) || (version.PublishedAt.Equal(out.LatestVersion.PublishedAt) && version.ID < out.LatestVersion.ID) {
			out.LatestVersion = cloneVersion(version)
		}
		if branch := s.branches[version.BranchID]; version.Status == 1 && branch != nil && branch.Status == 1 {
			branches[branch.ID] = true
		}
	}
	for branch := range branches {
		out.PublishedBranchIDs = append(out.PublishedBranchIDs, branch)
	}
	sort.Strings(out.PublishedBranchIDs)
	for _, draft := range s.drafts {
		if branch := s.branches[draft.BranchID]; draft.ServiceID == documentID && (draft.Status == DraftStatusSubmitted || draft.Status == DraftStatusPublished) && branch != nil && branch.Status == 1 {
			out.HasReviewedDraft = true
		}
	}
	if version := out.LatestVersion; version != nil {
		for _, endpoint := range s.endpoints {
			if endpoint.ContractVersionID == version.ID {
				out.EndpointCount++
			}
		}
		if s.apiServices[documentID].DocumentType == DocumentTypeMarkdown {
			if err := s.hydrateVersionContentLocked(s.requestContext(), version, "raw"); err != nil {
				return nil, err
			}
			lines := int64(strings.Count(version.RawSchema, "\n") + 1)
			out.RawLineCount, out.RawSizeBytes = &lines, int64(len(version.RawSchema))
			version.RawSchema = ""
		}
	}
	return out, nil
}

func (s *Store) DocumentMCPReadiness(actorID, projectID, documentID string) (*time.Time, error) {
	local, repo, err := s.readScope(domain.ReadScope{ActorID: actorID, ProjectID: projectID, DocumentID: documentID, Tokens: true})
	if err != nil {
		return nil, err
	}
	if local != nil {
		tokens, err := local.readinessTokens(actorID, projectID, documentID)
		if err != nil {
			return nil, err
		}
		if reader, ok := repo.(domain.OverviewRepository); ok {
			return reader.ReadLatestMCPRead(s.requestContext(), projectID, documentID, tokens)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	tokens, err := s.readinessTokens(actorID, projectID, documentID)
	if err != nil {
		return nil, err
	}
	var latest *time.Time
	for _, audit := range s.audits {
		if audit.ProjectID == projectID && audit.ServiceID == documentID && audit.Action == "mcp.tool_call" && audit.Metadata["evidence_kind"] == "published_content_read" && audit.Metadata["result"] == "success" && slices.Contains(tokens, audit.ActorTokenID) && (latest == nil || audit.CreatedAt.After(*latest)) {
			timestamp := audit.CreatedAt
			latest = &timestamp
		}
	}
	return latest, nil
}

func (s *Store) readinessTokens(actorID, projectID, documentID string) ([]string, error) {
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(documentID, projectID) {
		return nil, ErrNotFound
	}
	tokens := []string{}
	if s.projects[projectID].Status != ProjectStatusActive || s.apiServices[documentID].Status != ServiceStatusActive {
		return tokens, nil
	}
	scope := ScopeAPIRead
	if s.apiServices[documentID].DocumentType == DocumentTypeMarkdown {
		scope = ScopeDocRead
	}
	for _, token := range s.tokens {
		if token.UserID == actorID && token.Status == MCPTokenStatusActive && (token.ExpiresAt == nil || token.ExpiresAt.After(time.Now())) && slices.Contains(token.Scopes, scope) {
			tokens = append(tokens, token.ID)
		}
	}
	return tokens, nil
}
