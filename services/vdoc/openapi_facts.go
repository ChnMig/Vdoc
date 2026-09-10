package vdoc

import (
	"context"
	"fmt"
)

const openAPIParserVersion = 2

// 草稿预览也是缓存；保留创建预览时的对比基线，不更改草稿内容和更新时间。
func (s *Store) ensureDraftPreviewFactsLocked(draft *ContractDraft) error {
	if draft == nil || (draft.SchemaFormat != SchemaFormatOpenAPI30 && draft.SchemaFormat != SchemaFormatOpenAPI31) || draft.DiffPreview == nil || draft.DiffPreview.Summary.ParserVersion >= openAPIParserVersion {
		return nil
	}
	previous := draft.DiffPreview
	if s.versions[previous.FromVersionID] == nil {
		return nil
	}
	if err := s.ensureVersionEndpointFactsLocked(previous.FromVersionID); err != nil {
		return err
	}
	if err := s.hydrateDraftContentLocked(context.Background(), draft, "raw"); err != nil {
		return err
	}
	parsed, err := ParseOpenAPI(draft.RawSchema)
	if err != nil {
		return err
	}
	updated := s.diffEndpointSetsLocked(draft.ServiceID, previous.FromVersionID, previous.ToVersionID, s.endpointsForVersionLocked(previous.FromVersionID), parsed.Endpoints)
	updated.ID, updated.CreatedAt, updated.UpdatedAt = previous.ID, previous.CreatedAt, previous.UpdatedAt
	draft.DiffPreview = updated
	if s.persisted != nil && s.persisted.Drafts[draft.ID] != nil {
		s.persisted.Drafts[draft.ID].DiffPreview = cloneDiff(updated)
	}
	return nil
}

// 旧索引只缓存解析结果；升级时从不可变且校验哈希的原文恢复事实，保留接口 ID。
func (s *Store) ensureVersionEndpointFactsLocked(versionID string) error {
	version := s.versions[versionID]
	if version == nil || (version.SchemaFormat != SchemaFormatOpenAPI30 && version.SchemaFormat != SchemaFormatOpenAPI31) {
		return nil
	}
	endpoints := s.endpointsForVersionLocked(versionID)
	legacy := false
	for _, endpoint := range endpoints {
		operation, _ := asMap(endpoint.NormalizedOperation)
		if _, current := operation["securitySchemes"]; !current {
			legacy = true
		}
	}
	if !legacy {
		return nil
	}
	if err := s.hydrateVersionContentLocked(context.Background(), version, "raw"); err != nil {
		return err
	}
	parsed, err := ParseOpenAPI(version.RawSchema)
	if err != nil {
		return err
	}
	byOperation := map[string]Endpoint{}
	for _, endpoint := range parsed.Endpoints {
		byOperation[endpoint.Method+" "+endpoint.Path] = endpoint
	}
	if len(byOperation) != len(endpoints) {
		return fmt.Errorf("%w: stored endpoint index does not match version source", ErrFailedPrecondition)
	}
	updated := make([]Endpoint, 0, len(endpoints))
	for _, old := range endpoints {
		current, ok := byOperation[old.Method+" "+old.Path]
		if !ok {
			return fmt.Errorf("%w: stored endpoint is absent from version source", ErrFailedPrecondition)
		}
		current.ID, current.ContractVersionID = old.ID, old.ContractVersionID
		current.CreatedAt, current.UpdatedAt = old.CreatedAt, old.UpdatedAt
		updated = append(updated, current)
	}
	for _, endpoint := range updated {
		s.endpoints[endpoint.ID] = cloneEndpoint(&endpoint)
		// 与延迟加载原文一样，缓存升级不修改不可变的发布索引记录。
		if s.persisted != nil {
			s.persisted.Endpoints[endpoint.ID] = cloneEndpoint(&endpoint)
		}
	}
	return nil
}

func (s *Store) ensureDiffFactsLocked(diff *Diff) error {
	if diff == nil || diff.Summary.ParserVersion >= openAPIParserVersion {
		return nil
	}
	from, to := s.versions[diff.FromVersionID], s.versions[diff.ToVersionID]
	if from == nil || to == nil || from.SchemaFormat == DocumentFormatMarkdown || to.SchemaFormat == DocumentFormatMarkdown {
		return nil
	}
	if from.SchemaFormat == 0 || to.SchemaFormat == 0 {
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
