package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
)

type repositoryMemory struct {
	records  map[string]repo.Record
	blobs    map[string][]byte
	sequence int64
}

func newRepositoryMemory() *repositoryMemory {
	return &repositoryMemory{records: map[string]repo.Record{}, blobs: map[string][]byte{}}
}
func (m *repositoryMemory) clone() *repositoryMemory {
	n := newRepositoryMemory()
	n.sequence = m.sequence
	for k, v := range m.records {
		v.Payload = slices.Clone(v.Payload)
		n.records[k] = v
	}
	for k, v := range m.blobs {
		n.blobs[k] = slices.Clone(v)
	}
	return n
}
func (*repositoryMemory) LockRepository(context.Context, string) error { return nil }
func recordKey(p, k, id string) string                                 { return p + "\x00" + k + "\x00" + id }
func (m *repositoryMemory) RepositoryRecord(_ context.Context, p, k, id string) (*repo.Record, error) {
	v, ok := m.records[recordKey(p, k, id)]
	if !ok {
		return nil, ErrNotFound
	}
	v.Payload = slices.Clone(v.Payload)
	return &v, nil
}
func (m *repositoryMemory) RepositoryRecords(_ context.Context, p, k string, after int64, limit int) ([]repo.Record, error) {
	out := []repo.Record{}
	for _, v := range m.records {
		if v.ProjectID == p && v.Kind == k && v.Sequence > after {
			v.Payload = slices.Clone(v.Payload)
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out[:min(len(out), limit)], nil
}
func (m *repositoryMemory) PutRepositoryRecord(_ context.Context, v repo.Record, expected *int64) error {
	if expected != nil && (v.Kind == "commit" || v.Kind == "event" || v.Kind == "release" || v.Kind == "imported_release" || v.Kind == "legacy_map" || v.Kind == "idempotency") {
		return fmt.Errorf("%w: immutable repository record", ErrFailedPrecondition)
	}
	if !json.Valid(v.Payload) {
		return ErrInvalidArgument
	}
	key := recordKey(v.ProjectID, v.Kind, v.ID)
	old, ok := m.records[key]
	if expected == nil && ok {
		return ErrAlreadyExists
	}
	if expected != nil && (!ok || old.Generation != *expected) {
		return fmt.Errorf("%w: REF_CONFLICT: repository reference changed", ErrFailedPrecondition)
	}
	if ok {
		v.Sequence = old.Sequence
	} else {
		m.sequence++
		v.Sequence = m.sequence
	}
	v.Payload = slices.Clone(v.Payload)
	m.records[key] = v
	return nil
}

func (s *Store) repositoryStorageLocked() repo.Storage {
	if s.persistence != nil {
		storage, _ := s.persistence.repo.(repo.Storage)
		return storage
	}
	if s.repoMemory == nil {
		s.repoMemory = newRepositoryMemory()
	}
	return s.repoMemory
}

// WithRepositoryWrite 与旧协作权限使用同一事务锁；内容图不进入全局 State。
func (s *Store) WithRepositoryWrite(actorID, projectID string, permission int, fn func(repo.Storage) error) (resultErr error) {
	return s.withRepositoryWrite(actorID, projectID, permission, false, fn)
}

// 目录等可变元数据与文档仓库记录需要在同一事务中保存。
func (s *Store) withRepositoryStateWrite(actorID, projectID string, permission int, fn func(repo.Storage) error) error {
	return s.WithRepositoryDocument("").withRepositoryWrite(actorID, projectID, permission, true, fn)
}

func (s *Store) withRepositoryWrite(actorID, projectID string, permission int, persistState bool, fn func(repo.Storage) error) (resultErr error) {
	uploads := []string{}
	defer func() {
		if resultErr != nil {
			resultErr = s.repositoryCleanupUploads(resultErr, uploads)
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return err
	}
	var before, previousPersisted *domain.State
	if persistState {
		before, previousPersisted = s.cloneStateLocked(), s.persisted
		defer func() {
			if resultErr != nil {
				s.applyStateLocked(before)
				s.persisted = previousPersisted
			}
		}()
	}
	guard := domain.MutationGuard{ActorID: actorID, ProjectID: projectID, Permission: permission}
	if permission == MemberRoleReader {
		guard.AllowArchived = true
	}
	if tokenID := mcpMutationTokenID(s.requestContext()); tokenID != "" {
		guard.TokenID = tokenID
		token := s.tokens[tokenID]
		if token == nil {
			return ErrUnauthenticated
		}
		// MCP 入参可同时修改多种文档；事务内复核预检时观察到的全部 scope，避免部分撤销后仍落库。
		guard.TokenScopes = mcpAuthenticatedScopes(s.requestContext())
		if guard.TokenScopes == nil {
			guard.TokenScopes = slices.Clone(token.Scopes)
		}
		if permission == MemberRoleReader {
			guard.TokenScope = ScopeAPIRead
			if !slices.Contains(guard.TokenScopes, guard.TokenScope) {
				guard.TokenScope = ScopeDocRead
			}
		} else {
			guard.TokenScope = ScopeAPIDraft
			if !slices.Contains(guard.TokenScopes, guard.TokenScope) {
				guard.TokenScope = ScopeDocDraft
			}
		}
	}
	if err := domain.ValidateMutationContext(guard, s.stateLocked(), time.Now()); err != nil {
		return err
	}
	run := func(storage repo.Storage) error {
		if documentID := s.repositoryDocumentID(); documentID != "" {
			if err := s.repositoryPrepareDocument(storage, actorID, projectID, documentID, &uploads); err != nil {
				return err
			}
			return fn(documentRepositoryStorage{Storage: storage, documentID: documentID})
		}
		return fn(storage)
	}
	if s.persistence == nil {
		if s.repoMemory == nil {
			s.repoMemory = newRepositoryMemory()
		}
		before := s.repoMemory
		s.repoMemory = before.clone()
		if err := run(s.repoMemory); err != nil {
			s.repoMemory = before
			return err
		}
		return nil
	}
	tx, ok := s.persistence.repo.(transactionalRepository)
	if !ok {
		return fmt.Errorf("%w: repository transaction support is required", ErrFailedPrecondition)
	}
	resultErr = tx.WithinTransaction(s.requestContext(), func(database domain.Repository) error {
		if err := validateRepositoryMutation(s.requestContext(), database, &guard); err != nil {
			return err
		}
		storage, ok := database.(repo.Storage)
		if !ok {
			return fmt.Errorf("%w: repository storage is unavailable", ErrFailedPrecondition)
		}
		if err := storage.LockRepository(s.requestContext(), projectID); err != nil {
			return err
		}
		if persistState {
			// 取得项目/仓库锁之后再读目录，避免另一实例的创建或改名被旧快照覆盖。
			latest, err := database.LoadState(s.requestContext())
			if err != nil {
				return err
			}
			s.applyStateLocked(latest)
			s.persisted = s.cloneStateLocked()
			before, previousPersisted = s.cloneStateLocked(), s.persisted
		}
		if err := run(storage); err != nil {
			return err
		}
		if persistState {
			return s.persistence.saveLockedWithRepository(s.requestContext(), s, database)
		}
		return nil
	})
	if resultErr == nil && persistState {
		s.persisted = s.cloneStateLocked()
	}
	return resultErr
}

func (s *Store) WithRepositoryRead(actorID, projectID string, fn func(repo.Storage) error) error {
	return s.WithRepositoryWrite(actorID, projectID, MemberRoleReader, fn)
}

func repositoryLoad[T any](ctx context.Context, storage repo.Storage, project, kind, id string) (*T, error) {
	r, err := storage.RepositoryRecord(ctx, project, kind, id)
	if err != nil {
		return nil, err
	}
	var value T
	if err = json.Unmarshal(r.Payload, &value); err != nil {
		return nil, err
	}
	return &value, nil
}
func repositoryPut(ctx context.Context, storage repo.Storage, project, kind, id string, value any, generation int64, expected *int64) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return storage.PutRepositoryRecord(ctx, repo.Record{ProjectID: project, Kind: kind, ID: id, Generation: generation, Payload: raw}, expected)
}
func repositoryList[T any](ctx context.Context, storage repo.Storage, project, kind string) ([]T, error) {
	out := []T{}
	var after int64
	for {
		rows, err := storage.RepositoryRecords(ctx, project, kind, after, 500)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			var v T
			if err = json.Unmarshal(r.Payload, &v); err != nil {
				return nil, err
			}
			out = append(out, v)
			after = r.Sequence
		}
		if len(rows) < 500 {
			return out, nil
		}
		if len(out) > 10000 {
			return nil, fmt.Errorf("%w: repository collection exceeds query limit", ErrFailedPrecondition)
		}
	}
}

func (s *Store) RepositoryConverted(actorID, projectID string) (bool, error) {
	converted := false
	err := s.WithRepositoryRead(actorID, projectID, func(storage repo.Storage) error {
		_, e := storage.RepositoryRecord(s.requestContext(), projectID, "metadata", "converted")
		if errors.Is(e, ErrNotFound) {
			return nil
		}
		converted = e == nil
		return e
	})
	return converted, err
}
