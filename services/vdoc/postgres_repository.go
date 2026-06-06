package vdoc

import (
	"context"
	"sort"

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

type documentMutationRepository interface {
	UpsertDocument(ctx context.Context, document *domainvdoc.APIService) error
	UpsertDocumentBranch(ctx context.Context, branch *domainvdoc.ContractBranch) error
	UpsertDocumentDraft(ctx context.Context, draft *domainvdoc.ContractDraft, document *domainvdoc.APIService) error
}

type collaborationMutationRepository interface {
	UpsertUser(ctx context.Context, user *domainvdoc.User) error
	UpsertTeam(ctx context.Context, team *domainvdoc.Team) error
	UpsertProject(ctx context.Context, project *domainvdoc.Project) error
	UpsertProjectMember(ctx context.Context, member *domainvdoc.ProjectMember) error
}

type diffMutationRepository interface {
	UpsertDocumentDiff(ctx context.Context, diff *domainvdoc.Diff, fromVersion, toVersion *domainvdoc.ContractVersion) error
}

func (p *postgresPersistence) saveLocked(ctx context.Context, store *Store) error {
	if repo, ok := p.repo.(collaborationMutationRepository); ok {
		if err := p.saveCollaborationLocked(ctx, store, repo); err != nil {
			return err
		}
	}
	if repo, ok := p.repo.(documentMutationRepository); ok {
		if err := p.saveDocumentWorkflowLocked(ctx, store, repo); err != nil {
			return err
		}
	}
	for _, token := range sortedStoreValues(store.tokens, func(value *domainvdoc.MCPToken) string { return value.ID }) {
		if err := p.repo.UpsertMCPToken(ctx, token); err != nil {
			return err
		}
	}
	if repo, ok := p.repo.(diffMutationRepository); ok {
		if err := p.saveDiffsLocked(ctx, store, repo); err != nil {
			return err
		}
	}
	for _, audit := range sortedStoreValues(store.audits, func(value *domainvdoc.AuditLog) string {
		return value.CreatedAt.Format(sortableTimeLayout) + ":" + value.ID
	}) {
		if err := p.repo.RecordAudit(ctx, audit); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresPersistence) saveDiffsLocked(ctx context.Context, store *Store, repo diffMutationRepository) error {
	for _, diff := range sortedStoreValues(store.diffs, func(value *domainvdoc.Diff) string { return value.ID }) {
		fromVersion := store.versions[diff.FromVersionID]
		toVersion := store.versions[diff.ToVersionID]
		if fromVersion == nil || toVersion == nil {
			continue
		}
		if err := repo.UpsertDocumentDiff(ctx, diff, fromVersion, toVersion); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresPersistence) saveCollaborationLocked(ctx context.Context, store *Store, repo collaborationMutationRepository) error {
	for _, user := range sortedStoreValues(store.users, func(value *domainvdoc.User) string { return value.ID }) {
		if err := repo.UpsertUser(ctx, user); err != nil {
			return err
		}
	}
	for _, team := range sortedStoreValues(store.teams, func(value *domainvdoc.Team) string { return value.ID }) {
		if err := repo.UpsertTeam(ctx, team); err != nil {
			return err
		}
	}
	for _, project := range sortedStoreValues(store.projects, func(value *domainvdoc.Project) string { return value.ID }) {
		if err := repo.UpsertProject(ctx, project); err != nil {
			return err
		}
	}
	for _, member := range sortedStoreValues(store.members, func(value *domainvdoc.ProjectMember) string {
		return value.ProjectID + ":" + value.UserID
	}) {
		if err := repo.UpsertProjectMember(ctx, member); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresPersistence) saveDocumentWorkflowLocked(ctx context.Context, store *Store, repo documentMutationRepository) error {
	for _, document := range sortedStoreValues(store.apiServices, func(value *domainvdoc.APIService) string { return value.ID }) {
		if err := repo.UpsertDocument(ctx, document); err != nil {
			return err
		}
	}
	for _, branch := range sortedStoreValues(store.branches, func(value *domainvdoc.ContractBranch) string { return value.ID }) {
		if err := repo.UpsertDocumentBranch(ctx, branch); err != nil {
			return err
		}
	}
	for _, draft := range sortedStoreValues(store.drafts, func(value *domainvdoc.ContractDraft) string { return value.ID }) {
		document := store.apiServices[documentIdentity(draft.DocumentID, draft.ServiceID)]
		if err := repo.UpsertDocumentDraft(ctx, draft, document); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresPersistence) publishLocked(ctx context.Context, input domainvdoc.PublishStateInput) error {
	return p.repo.PublishState(ctx, input)
}

func (s *Store) applyStateLocked(loaded *domainvdoc.State) {
	if loaded == nil {
		loaded = domainvdoc.NewState()
	}
	s.users = loaded.Users
	s.teams = loaded.Teams
	s.projects = loaded.Projects
	s.members = loaded.Members
	s.apiServices = loaded.APIServices
	s.branches = loaded.Branches
	s.drafts = loaded.Drafts
	s.versions = loaded.Versions
	s.endpoints = loaded.Endpoints
	s.diffs = loaded.Diffs
	s.tokens = loaded.Tokens
	s.audits = loaded.AuditLogs
}

func (s *Store) stateLocked() *domainvdoc.State {
	return &domainvdoc.State{
		Users:       s.users,
		Teams:       s.teams,
		Projects:    s.projects,
		Members:     s.members,
		APIServices: s.apiServices,
		Branches:    s.branches,
		Drafts:      s.drafts,
		Versions:    s.versions,
		Endpoints:   s.endpoints,
		Diffs:       s.diffs,
		Tokens:      s.tokens,
		AuditLogs:   s.audits,
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
	for key, value := range s.apiServices {
		copied := *value
		state.APIServices[key] = &copied
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

const sortableTimeLayout = "2006-01-02T15:04:05.999999999Z07:00"

func sortedStoreValues[T any](items map[string]*T, key func(*T) string) []*T {
	values := make([]*T, 0, len(items))
	for _, value := range items {
		values = append(values, value)
	}
	sort.Slice(values, func(first, second int) bool { return key(values[first]) < key(values[second]) })
	return values
}

func documentIdentity(documentID, serviceID string) string {
	if documentID != "" {
		return documentID
	}
	return serviceID
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
