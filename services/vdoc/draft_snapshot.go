package vdoc

// ReadDraftContent 在同一次读取中返回草稿元数据和正文，避免编辑器拼接不同版本的数据。
func (s *Store) ReadDraftContent(actorID, projectID, documentID, draftID, kind string) (*ContractDraft, *SchemaDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, nil, ErrPermissionDenied
	}
	draft, ok := s.draftInProjectServiceLocked(projectID, documentID, draftID)
	if !ok {
		return nil, nil, ErrNotFound
	}
	if err := s.ensureDraftPreviewFactsLocked(draft); err != nil {
		return nil, nil, err
	}
	if err := s.hydrateDraftContentLocked(s.requestContext(), draft, kind); err != nil {
		return nil, nil, err
	}
	build := schemaDocument
	if draft.SchemaFormat == DocumentFormatMarkdown {
		build = markdownContentDocument
	}
	content, err := build("draft", draft.ID, kind, draft.RawSchema, draft.NormalizedSchema, draft.RawSchemaObjectKey, draft.NormalizedObjectKey, draft.RawSchemaHash, draft.NormalizedSchemaHash)
	if err != nil {
		return nil, nil, err
	}
	return cloneDraft(draft), content, nil
}
