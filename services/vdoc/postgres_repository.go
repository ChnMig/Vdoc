package vdoc

import (
	"context"

	domainvdoc "vdoc/domain/vdoc"
)

func (p *postgresPersistence) load(ctx context.Context, store *Store) error {
	state, err := p.repo.LoadState(ctx)
	if err != nil {
		return err
	}
	store.applyStateLocked(state)
	return store.hydrateSchemaObjectsLocked(ctx)
}

func (p *postgresPersistence) saveLocked(ctx context.Context, store *Store) error {
	return p.repo.SaveState(ctx, store.stateLocked())
}

func (p *postgresPersistence) publishLocked(ctx context.Context, input domainvdoc.PublishStateInput) error {
	return p.repo.PublishState(ctx, input)
}

func (s *Store) applyStateLocked(state *domainvdoc.State) {
	if state == nil {
		state = domainvdoc.NewState()
	}
	s.users = state.Users
	s.teams = state.Teams
	s.projects = state.Projects
	s.members = state.Members
	s.services = state.Services
	s.branches = state.Branches
	s.drafts = state.Drafts
	s.versions = state.Versions
	s.endpoints = state.Endpoints
	s.diffs = state.Diffs
	s.tokens = state.Tokens
	s.audits = state.AuditLogs
}

func (s *Store) stateLocked() *domainvdoc.State {
	return &domainvdoc.State{
		Users:     s.users,
		Teams:     s.teams,
		Projects:  s.projects,
		Members:   s.members,
		Services:  s.services,
		Branches:  s.branches,
		Drafts:    s.drafts,
		Versions:  s.versions,
		Endpoints: s.endpoints,
		Diffs:     s.diffs,
		Tokens:    s.tokens,
		AuditLogs: s.audits,
	}
}

func (s *Store) cloneStateLocked() *domainvdoc.State {
	state := domainvdoc.NewState()
	for key, value := range s.users {
		copied := *value
		state.Users[key] = &copied
	}
	for key, value := range s.teams {
		copied := *value
		state.Teams[key] = &copied
	}
	for key, value := range s.projects {
		copied := *value
		state.Projects[key] = &copied
	}
	for key, value := range s.members {
		copied := *value
		state.Members[key] = &copied
	}
	for key, value := range s.services {
		copied := *value
		state.Services[key] = &copied
	}
	for key, value := range s.branches {
		copied := *value
		state.Branches[key] = &copied
	}
	for key, value := range s.drafts {
		copied := *value
		state.Drafts[key] = &copied
	}
	for key, value := range s.versions {
		copied := *value
		state.Versions[key] = &copied
	}
	for key, value := range s.endpoints {
		copied := *value
		state.Endpoints[key] = &copied
	}
	for key, value := range s.diffs {
		copied := *value
		copied.Items = append([]domainvdoc.DiffItem(nil), value.Items...)
		state.Diffs[key] = &copied
	}
	for key, value := range s.tokens {
		copied := *value
		copied.Scopes = append([]int(nil), value.Scopes...)
		copied.TokenCiphertext = append([]byte(nil), value.TokenCiphertext...)
		if value.RevokedBy != nil {
			revokedBy := *value.RevokedBy
			copied.RevokedBy = &revokedBy
		}
		state.Tokens[key] = &copied
	}
	for key, value := range s.audits {
		state.AuditLogs[key] = cloneAuditLog(value)
	}
	return state
}

func (s *Store) hydrateSchemaObjectsLocked(ctx context.Context) error {
	if s.objects == nil {
		return nil
	}
	for _, draft := range s.drafts {
		if err := s.hydrateDraftSchemaLocked(ctx, draft); err != nil {
			return err
		}
	}
	for _, version := range s.versions {
		if err := s.hydrateVersionSchemaLocked(ctx, version); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) hydrateDraftSchemaLocked(ctx context.Context, draft *ContractDraft) error {
	if draft == nil {
		return nil
	}
	if draft.RawSchema == "" && draft.RawSchemaObjectKey != "" {
		body, err := s.objects.GetObject(ctx, draft.RawSchemaObjectKey)
		if err != nil {
			return err
		}
		draft.RawSchema = string(body)
	}
	if draft.NormalizedSchema == "" && draft.NormalizedObjectKey != "" {
		body, err := s.objects.GetObject(ctx, draft.NormalizedObjectKey)
		if err != nil {
			return err
		}
		draft.NormalizedSchema = string(body)
	}
	return nil
}

func (s *Store) hydrateVersionSchemaLocked(ctx context.Context, version *ContractVersion) error {
	if version == nil {
		return nil
	}
	if version.RawSchema == "" && version.RawSchemaObjectKey != "" {
		body, err := s.objects.GetObject(ctx, version.RawSchemaObjectKey)
		if err != nil {
			return err
		}
		version.RawSchema = string(body)
	}
	if version.NormalizedSchema == "" && version.NormalizedObjectKey != "" {
		body, err := s.objects.GetObject(ctx, version.NormalizedObjectKey)
		if err != nil {
			return err
		}
		version.NormalizedSchema = string(body)
	}
	return nil
}
