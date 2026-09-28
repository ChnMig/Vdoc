package vdoc

// 解析只读取请求快照；进入写锁后仍执行原有权限、revision 与发布基线校验。
type preparedOpenAPI struct {
	hash   string
	parsed ParsedOpenAPI
	err    error
}

func (s *Store) prepareOpenAPI(content string) *Store {
	view := *s
	parsed, err := ParseOpenAPIContext(s.requestContext(), content)
	view.prepared = &preparedOpenAPI{hash: sha(content), parsed: parsed, err: err}
	return &view
}

func (s *Store) parseOpenAPI(content string) (ParsedOpenAPI, error) {
	if s.prepared != nil {
		if s.prepared.hash != sha(content) {
			return ParsedOpenAPI{}, ErrFailedPrecondition
		}
		return s.prepared.parsed, s.prepared.err
	}
	return ParseOpenAPIContext(s.requestContext(), content)
}

func (s *Store) prepareDraftOpenAPI(actorID, projectID, documentID, draftID string) (*Store, error) {
	content, err := func() (string, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.refreshLocked(); err != nil {
			return "", err
		}
		if !s.canPublishLocked(actorID, projectID) {
			return "", ErrPermissionDenied
		}
		draft, ok := s.draftInProjectServiceLocked(projectID, documentID, draftID)
		if !ok {
			return "", ErrNotFound
		}
		if err := s.hydrateDraftContentLocked(s.requestContext(), draft, "raw"); err != nil {
			return "", err
		}
		return draft.RawSchema, nil
	}()
	if err != nil {
		return nil, err
	}
	return s.prepareOpenAPI(content), nil
}
