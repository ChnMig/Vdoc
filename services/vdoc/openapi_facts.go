package vdoc

import (
	"fmt"
	domain "vdoc/domain/vdoc"
)

const openAPIParserVersion = 9

// 待发布草稿始终对比当前分支 latest；已发布草稿保留当时审核基线。
func (s *Store) ensureDraftPreviewFactsLocked(draft *ContractDraft) error {
	if draft == nil {
		return nil
	}
	markdown := draft.SchemaFormat == DocumentFormatMarkdown
	if !markdown && draft.SchemaFormat != SchemaFormatOpenAPI30 && draft.SchemaFormat != SchemaFormatOpenAPI31 {
		return nil
	}
	previous := draft.DiffPreview
	baseline := s.latestVersionLocked(draft.ServiceID, draft.BranchID)
	if draft.Status == DraftStatusPublished {
		if previous == nil {
			return nil
		}
		baseline = s.versions[previous.FromVersionID]
		if previous.FromVersionID == "" {
			// 历史数据库仅保留统计；使用该次发布的前一版本恢复差异。
			for _, version := range s.versions {
				if version.DraftID == draft.ID {
					baseline = s.previousVersionLocked(version)
					break
				}
			}
		}
	}
	if baseline == nil {
		s.cacheDraftPreviewLocked(draft, nil)
		return nil
	}
	if previous != nil && previous.FromVersionID == baseline.ID && previous.Summary.DocumentFormat == draft.SchemaFormat && draftPreviewHasDetails(previous) && (markdown || previous.Summary.ParserVersion >= openAPIParserVersion) {
		return nil
	}
	if err := s.hydrateDraftContentLocked(s.requestContext(), draft, "raw"); err != nil {
		return err
	}
	var updated *Diff
	if markdown {
		if err := s.hydrateVersionContentLocked(s.requestContext(), baseline, "stable"); err != nil {
			return err
		}
		updated = markdownDiff(draft.ServiceID, baseline.ID, "draft", baseline.NormalizedSchema, draft.RawSchema)
	} else {
		if err := s.ensureVersionEndpointFactsLocked(baseline.ID); err != nil {
			return err
		}
		parsed, err := s.parseOpenAPI(draft.RawSchema)
		if err != nil {
			return err
		}
		updated = s.diffEndpointSetsLocked(draft.ServiceID, baseline.ID, "draft", parsed.SchemaFormat, s.endpointsForVersionLocked(baseline.ID), parsed.Endpoints)
	}
	if previous != nil {
		updated.ID, updated.CreatedAt, updated.UpdatedAt = previous.ID, previous.CreatedAt, previous.UpdatedAt
	}
	s.cacheDraftPreviewLocked(draft, updated)
	return nil
}

func draftPreviewHasDetails(preview *Diff) bool {
	summary := preview.Summary
	return len(preview.Items) > 0 || (summary.AddedEndpoints == 0 && summary.RemovedEndpoints == 0 && summary.ModifiedEndpoints == 0 && summary.BreakingChanges == 0 && summary.AddedLines == 0 && summary.RemovedLines == 0 && summary.ModifiedLines == 0 && summary.ModifiedBlocks == 0)
}

func (s *Store) cacheDraftPreviewLocked(draft *ContractDraft, preview *Diff) {
	draft.DiffPreview = preview
	if current := s.drafts[draft.ID]; current != nil && current.Revision() == draft.Revision() {
		current.DiffPreview = cloneDiff(preview)
	}
	if s.persisted != nil {
		if persisted := s.persisted.Drafts[draft.ID]; persisted != nil && persisted.Revision() == draft.Revision() {
			persisted.DiffPreview = cloneDiff(preview)
		}
	}
}

// 旧索引只缓存解析结果；升级时从不可变且校验哈希的原文恢复事实，保留接口 ID。
func (s *Store) ensureVersionEndpointFactsLocked(versionID string) error {
	version := s.versions[versionID]
	if version == nil || (version.SchemaFormat != SchemaFormatOpenAPI30 && version.SchemaFormat != SchemaFormatOpenAPI31) {
		return nil
	}
	endpoints := s.endpointsForVersionLocked(versionID)
	legacy := version.ParserVersion < openAPIParserVersion
	for _, endpoint := range endpoints {
		operation, _ := asMap(endpoint.NormalizedOperation)
		if _, current := operation["securitySchemes"]; !current {
			legacy = true
		}
	}
	if !legacy {
		return nil
	}
	if err := s.hydrateVersionContentLocked(s.requestContext(), version, "raw"); err != nil {
		return err
	}
	parsed, err := ParseOpenAPIContext(s.requestContext(), version.RawSchema)
	if err != nil {
		return err
	}
	byOperation := map[string]Endpoint{}
	for _, endpoint := range parsed.Endpoints {
		byOperation[endpoint.Method+" "+endpoint.Path] = endpoint
	}
	oldByOperation := map[string]Endpoint{}
	for _, old := range endpoints {
		if _, ok := byOperation[old.Method+" "+old.Path]; !ok {
			return fmt.Errorf("%w: stored endpoint is absent from version source", ErrFailedPrecondition)
		}
		oldByOperation[old.Method+" "+old.Path] = old
	}
	updated := make([]Endpoint, 0, len(parsed.Endpoints))
	for _, current := range parsed.Endpoints {
		if old, ok := oldByOperation[current.Method+" "+current.Path]; ok {
			current.ID, current.CreatedAt, current.UpdatedAt = old.ID, old.CreatedAt, old.UpdatedAt
		} else {
			current.ID = sha("endpoint-facts:" + versionID + ":" + current.Method + ":" + current.Path)[:32]
			current.CreatedAt, current.UpdatedAt = version.CreatedAt, version.CreatedAt
		}
		current.ContractVersionID = versionID
		updated = append(updated, current)
	}
	repo := s.endpointFactsRepo
	if repo == nil && s.persistence != nil {
		repo, _ = s.persistence.repo.(domain.EndpointFactsRepository)
	}
	if repo != nil {
		if err := repo.RefreshEndpointFacts(s.requestContext(), versionID, version.RawSchemaHash, openAPIParserVersion, sha(parsed.Normalized), updated); err != nil {
			return err
		}
	}
	version.ParserVersion, version.ParsedSchemaHash = openAPIParserVersion, sha(parsed.Normalized)
	if s.persisted != nil && s.persisted.Versions[versionID] != nil {
		s.persisted.Versions[versionID].ParserVersion, s.persisted.Versions[versionID].ParsedSchemaHash = version.ParserVersion, version.ParsedSchemaHash
	}
	for _, endpoint := range updated {
		s.endpoints[endpoint.ID] = cloneEndpoint(&endpoint)
		// 派生索引已单独持久化，同步快照以免后续保存误判为发布内容变更。
		if s.persisted != nil {
			s.persisted.Endpoints[endpoint.ID] = cloneEndpoint(&endpoint)
		}
	}
	return nil
}

func (s *Store) ensureDiffFactsLocked(diff *Diff) error {
	if diff == nil {
		return nil
	}
	from, to := s.versions[diff.FromVersionID], s.versions[diff.ToVersionID]
	if from == nil || to == nil || from.SchemaFormat == DocumentFormatMarkdown || to.SchemaFormat == DocumentFormatMarkdown {
		return nil
	}
	if from.SchemaFormat == 0 || to.SchemaFormat == 0 {
		return nil
	}
	// 旧解析结果或目标格式不匹配时，按需修复对比快照。
	if diff.Summary.ParserVersion >= openAPIParserVersion && diff.Summary.DocumentFormat == to.SchemaFormat {
		return nil
	}
	for _, version := range []*ContractVersion{from, to} {
		if err := s.ensureVersionEndpointFactsLocked(version.ID); err != nil {
			return err
		}
	}
	updated := s.diffVersionsLocked(diff.ServiceID, from, to)
	updated.ID, updated.CreatedAt = diff.ID, diff.CreatedAt
	for index := range updated.Items {
		for _, old := range diff.Items {
			current := updated.Items[index]
			current.ID, current.SortOrder = old.ID, old.SortOrder
			if valuesEqual(current, old) {
				updated.Items[index].ID = old.ID
				break
			}
		}
	}
	ref, err := s.persistDiffSnapshotLocked(to.ProjectID, to.ServiceID, to.BranchID, updated)
	if err != nil {
		return err
	}
	s.diffs[updated.ID] = updated
	if err := s.persistWithObjectRefsLocked(ref); err != nil {
		return s.cleanupNewObjectRefs(err, ref)
	}
	*diff = *updated
	return nil
}

func effectiveVersionHash(version *ContractVersion) string {
	if version.ParsedSchemaHash != "" {
		return version.ParsedSchemaHash
	}
	return version.NormalizedSchemaHash
}
