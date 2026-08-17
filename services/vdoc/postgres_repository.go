package vdoc

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	domainshare "vdoc/domain/documentshare"
	domainvdoc "vdoc/domain/vdoc"
)

type versionedStateRepository interface {
	StateRevision(ctx context.Context) (string, error)
	LoadStateWithRevision(ctx context.Context) (*domainvdoc.State, string, error)
}

func (p *postgresPersistence) load(ctx context.Context, store *Store) (bool, error) {
	if repo, ok := p.repo.(versionedStateRepository); ok {
		revision, err := repo.StateRevision(ctx)
		if err != nil {
			return false, err
		}
		if p.revision != "" && revision == p.revision {
			return false, nil
		}
		state, loadedRevision, err := repo.LoadStateWithRevision(ctx)
		if err != nil {
			return false, err
		}
		store.applyStateLocked(state)
		p.revision = loadedRevision
		return true, nil
	}
	state, err := p.repo.LoadState(ctx)
	if err != nil {
		return false, err
	}
	store.applyStateLocked(state)
	return true, nil
}

func (p *postgresPersistence) loadUser(ctx context.Context, userID string) (*domainvdoc.User, error) {
	return p.repo.LoadUser(ctx, userID)
}

type documentMutationRepository interface {
	UpsertDocument(ctx context.Context, document *domainvdoc.APIService) error
	UpsertDocumentBranch(ctx context.Context, branch *domainvdoc.ContractBranch) error
	UpsertDocumentDraft(ctx context.Context, draft *domainvdoc.ContractDraft, document *domainvdoc.APIService) error
}

type optimisticDocumentMutationRepository interface {
	UpsertDocumentIfUnchanged(ctx context.Context, document, previous *domainvdoc.APIService) error
	UpsertDocumentBranchIfUnchanged(ctx context.Context, branch, previous *domainvdoc.ContractBranch) error
	UpsertDocumentDraftIfUnchanged(ctx context.Context, draft, previous *domainvdoc.ContractDraft, document *domainvdoc.APIService) error
}

type collaborationMutationRepository interface {
	UpsertUser(ctx context.Context, user *domainvdoc.User) error
	UpsertTeam(ctx context.Context, team *domainvdoc.Team) error
	UpsertProject(ctx context.Context, project *domainvdoc.Project) error
	UpsertProjectMember(ctx context.Context, member *domainvdoc.ProjectMember) error
}

type optimisticCollaborationMutationRepository interface {
	UpsertUserIfUnchanged(ctx context.Context, user, previous *domainvdoc.User) error
	UpsertTeamIfUnchanged(ctx context.Context, team, previous *domainvdoc.Team) error
	UpsertProjectIfUnchanged(ctx context.Context, project, previous *domainvdoc.Project) error
	UpsertProjectMemberIfUnchanged(ctx context.Context, member, previous *domainvdoc.ProjectMember) error
}

// collaborationInvariantRepository is implemented by PostgreSQL repositories
// that can serialize cross-row administrator invariants inside the same
// transaction as the collaboration writes. The in-memory Store mutex only
// protects one process, so it cannot be the final authority in a multi-instance
// deployment.
type collaborationInvariantRepository interface {
	LockCollaborationInvariants(ctx context.Context, superAdmin, projectAdmin bool) error
	ValidateCollaborationInvariants(ctx context.Context, superAdmin bool, projectIDs, userIDs []string) error
}

type collaborationInvariantPlan struct {
	superAdmin bool
	projectIDs []string
	userIDs    []string
}

type diffMutationRepository interface {
	UpsertDocumentDiff(ctx context.Context, diff *domainvdoc.Diff, fromVersion, toVersion *domainvdoc.ContractVersion) error
}

type documentShareMutationRepository interface {
	UpsertDocumentShare(ctx context.Context, share *domainvdoc.DocumentShare) error
}

type optimisticDocumentShareMutationRepository interface {
	UpsertDocumentShareIfUnchanged(ctx context.Context, share, previous *domainvdoc.DocumentShare) error
}

type optimisticMCPTokenMutationRepository interface {
	UpsertMCPTokenIfUnchanged(ctx context.Context, token, previous *domainvdoc.MCPToken) error
}

type documentShareReadRepository interface {
	LoadPublicDocumentShareSnapshot(ctx context.Context, shareID string) (*domainvdoc.PublicDocumentShareSnapshot, error)
}

type publicDocumentShareAccessRepository interface {
	RecordPublicDocumentShareAccess(ctx context.Context, shareID string, audit *domainvdoc.AuditLog) error
}

type transactionalRepository interface {
	WithinTransaction(ctx context.Context, fn func(domainvdoc.Repository) error) error
}

func (p *postgresPersistence) loadPublicDocumentShareSnapshot(ctx context.Context, shareID string) (*domainvdoc.PublicDocumentShareSnapshot, bool, error) {
	repo, ok := p.repo.(documentShareReadRepository)
	if !ok {
		return nil, false, nil
	}
	snapshot, err := repo.LoadPublicDocumentShareSnapshot(ctx, shareID)
	return snapshot, true, err
}

func (p *postgresPersistence) recordAudit(ctx context.Context, audit *domainvdoc.AuditLog) error {
	if audit == nil {
		return nil
	}
	return p.repo.RecordAudit(ctx, audit)
}

func (p *postgresPersistence) recordPublicDocumentShareAccess(ctx context.Context, shareID string, audit *domainvdoc.AuditLog) error {
	if repo, ok := p.repo.(publicDocumentShareAccessRepository); ok {
		return repo.RecordPublicDocumentShareAccess(ctx, shareID, audit)
	}
	return p.recordAudit(ctx, audit)
}

func (p *postgresPersistence) archiveTeam(ctx context.Context, teamID string, audit *domainvdoc.AuditLog) error {
	return p.repo.ArchiveTeam(ctx, teamID, audit)
}

func (p *postgresPersistence) saveLocked(ctx context.Context, store *Store) error {
	return p.saveLockedWithObjectRefs(ctx, store, nil)
}

func (p *postgresPersistence) saveLockedWithObjectRefs(ctx context.Context, store *Store, refs []domainvdoc.ObjectRef) error {
	save := func(repository domainvdoc.Repository) error {
		for _, ref := range refs {
			if ref.Key == "" {
				continue
			}
			if err := repository.RecordObject(ctx, ref); err != nil {
				return err
			}
		}
		return p.saveLockedWithRepository(ctx, store, repository)
	}
	if repo, ok := p.repo.(transactionalRepository); ok {
		return repo.WithinTransaction(ctx, save)
	}
	return save(p.repo)
}

func (p *postgresPersistence) saveLockedWithRepository(ctx context.Context, store *Store, repository domainvdoc.Repository) error {
	if repo, ok := repository.(collaborationMutationRepository); ok {
		if err := p.saveCollaborationLocked(ctx, store, repo); err != nil {
			return err
		}
	}
	if repo, ok := repository.(documentMutationRepository); ok {
		if err := p.saveDocumentWorkflowLocked(ctx, store, repo); err != nil {
			return err
		}
	}
	if repo, ok := repository.(diffMutationRepository); ok {
		var persistedDiffs map[string]*domainvdoc.Diff
		if store.persisted != nil {
			persistedDiffs = store.persisted.Diffs
		}
		for _, diff := range changedStoreValues(store.diffs, persistedDiffs, func(value *domainvdoc.Diff) string { return value.ID }) {
			fromVersion := store.versions[diff.FromVersionID]
			toVersion := store.versions[diff.ToVersionID]
			if fromVersion == nil || toVersion == nil {
				return domainvdoc.ErrFailedPrecondition
			}
			if err := repo.UpsertDocumentDiff(ctx, diff, fromVersion, toVersion); err != nil {
				return err
			}
		}
	}
	var persistedTokens map[string]*domainvdoc.MCPToken
	if store.persisted != nil {
		persistedTokens = store.persisted.Tokens
	}
	optimisticTokens, supportsOptimisticTokens := repository.(optimisticMCPTokenMutationRepository)
	for _, token := range changedStoreValues(store.tokens, persistedTokens, func(value *domainvdoc.MCPToken) string { return value.ID }) {
		var err error
		if supportsOptimisticTokens {
			err = optimisticTokens.UpsertMCPTokenIfUnchanged(ctx, token, persistedTokens[token.ID])
		} else {
			err = repository.UpsertMCPToken(ctx, token)
		}
		if err != nil {
			return err
		}
	}
	if repo, ok := repository.(documentShareMutationRepository); ok {
		var persistedShares map[string]*domainvdoc.DocumentShare
		if store.persisted != nil {
			persistedShares = store.persisted.Shares
		}
		optimisticShares, supportsOptimisticShares := repository.(optimisticDocumentShareMutationRepository)
		for _, share := range changedStoreValues(store.shares, persistedShares, func(value *domainvdoc.DocumentShare) string { return value.ID }) {
			var err error
			if supportsOptimisticShares {
				err = optimisticShares.UpsertDocumentShareIfUnchanged(ctx, share, persistedShares[share.ID])
			} else {
				err = repo.UpsertDocumentShare(ctx, share)
			}
			if err != nil {
				return err
			}
		}
	}
	if repo, ok := repository.(aiMutationRepository); ok {
		if err := p.saveAIStateLocked(ctx, store, repo); err != nil {
			return err
		}
	}
	var persistedAudits map[string]*domainvdoc.AuditLog
	if store.persisted != nil {
		persistedAudits = store.persisted.AuditLogs
	}
	for _, audit := range changedStoreValues(store.audits, persistedAudits, func(value *domainvdoc.AuditLog) string {
		return value.CreatedAt.Format(sortableTimeLayout) + ":" + value.ID
	}) {
		if err := repository.RecordAudit(ctx, audit); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresPersistence) saveCollaborationLocked(ctx context.Context, store *Store, repo collaborationMutationRepository) error {
	var persistedUsers map[string]*domainvdoc.User
	var persistedTeams map[string]*domainvdoc.Team
	var persistedProjects map[string]*domainvdoc.Project
	var persistedMembers map[string]*domainvdoc.ProjectMember
	if store.persisted != nil {
		persistedUsers = store.persisted.Users
		persistedTeams = store.persisted.Teams
		persistedProjects = store.persisted.Projects
		persistedMembers = store.persisted.Members
	}
	changedUsers := changedStoreValues(store.users, persistedUsers, func(value *domainvdoc.User) string { return value.ID })
	changedTeams := changedStoreValues(store.teams, persistedTeams, func(value *domainvdoc.Team) string { return value.ID })
	changedProjects := changedStoreValues(store.projects, persistedProjects, func(value *domainvdoc.Project) string { return value.ID })
	changedMembers := changedStoreValues(store.members, persistedMembers, func(value *domainvdoc.ProjectMember) string {
		return value.ProjectID + ":" + value.UserID
	})
	plan := buildCollaborationInvariantPlan(changedUsers, changedProjects, changedMembers, persistedUsers, persistedProjects, persistedMembers)
	invariantRepo, supportsInvariants := repo.(collaborationInvariantRepository)
	if supportsInvariants {
		if err := invariantRepo.LockCollaborationInvariants(ctx, plan.superAdmin, len(plan.projectIDs) > 0 || len(plan.userIDs) > 0); err != nil {
			return err
		}
	}
	optimisticRepo, supportsOptimisticWrites := repo.(optimisticCollaborationMutationRepository)
	for _, user := range changedUsers {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertUserIfUnchanged(ctx, user, persistedUsers[user.ID])
		} else {
			err = repo.UpsertUser(ctx, user)
		}
		if err != nil {
			return err
		}
	}
	for _, team := range changedTeams {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertTeamIfUnchanged(ctx, team, persistedTeams[team.ID])
		} else {
			err = repo.UpsertTeam(ctx, team)
		}
		if err != nil {
			return err
		}
	}
	for _, project := range changedProjects {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertProjectIfUnchanged(ctx, project, persistedProjects[project.ID])
		} else {
			err = repo.UpsertProject(ctx, project)
		}
		if err != nil {
			return err
		}
	}
	for _, member := range changedMembers {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertProjectMemberIfUnchanged(ctx, member, persistedMembers[member.ProjectID+":"+member.UserID])
		} else {
			err = repo.UpsertProjectMember(ctx, member)
		}
		if err != nil {
			return err
		}
	}
	if supportsInvariants {
		if err := invariantRepo.ValidateCollaborationInvariants(ctx, plan.superAdmin, plan.projectIDs, plan.userIDs); err != nil {
			return err
		}
	}
	return nil
}

func buildCollaborationInvariantPlan(
	users []*domainvdoc.User,
	projects []*domainvdoc.Project,
	members []*domainvdoc.ProjectMember,
	persistedUsers map[string]*domainvdoc.User,
	persistedProjects map[string]*domainvdoc.Project,
	persistedMembers map[string]*domainvdoc.ProjectMember,
) collaborationInvariantPlan {
	projectIDs := map[string]struct{}{}
	userIDs := map[string]struct{}{}
	plan := collaborationInvariantPlan{}
	for _, user := range users {
		if user == nil {
			continue
		}
		previous := persistedUsers[user.ID]
		if previous != nil && previous.Status == domainvdoc.UserStatusActive && previous.IsSuperAdmin && (user.Status != domainvdoc.UserStatusActive || !user.IsSuperAdmin) {
			plan.superAdmin = true
		}
		if previous != nil && previous.Status != user.Status {
			userIDs[user.ID] = struct{}{}
		}
	}
	for _, project := range projects {
		if project == nil || project.Status != domainvdoc.ProjectStatusActive {
			continue
		}
		previous := persistedProjects[project.ID]
		if previous == nil || previous.Status != domainvdoc.ProjectStatusActive {
			projectIDs[project.ID] = struct{}{}
		}
	}
	for _, member := range members {
		if member == nil {
			continue
		}
		previous := persistedMembers[member.ProjectID+":"+member.UserID]
		wasAdmin := previous != nil && previous.Status == domainvdoc.MemberStatusActive && previous.Role == domainvdoc.MemberRoleAdmin
		isAdmin := member.Status == domainvdoc.MemberStatusActive && member.Role == domainvdoc.MemberRoleAdmin
		if wasAdmin != isAdmin {
			projectIDs[member.ProjectID] = struct{}{}
		}
	}
	plan.projectIDs = sortedSetValues(projectIDs)
	plan.userIDs = sortedSetValues(userIDs)
	return plan
}

func sortedSetValues(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (p *postgresPersistence) saveDocumentWorkflowLocked(ctx context.Context, store *Store, repo documentMutationRepository) error {
	var persistedDocuments map[string]*domainvdoc.APIService
	var persistedBranches map[string]*domainvdoc.ContractBranch
	var persistedDrafts map[string]*domainvdoc.ContractDraft
	if store.persisted != nil {
		persistedDocuments = store.persisted.APIServices
		persistedBranches = store.persisted.Branches
		persistedDrafts = store.persisted.Drafts
	}
	optimisticRepo, supportsOptimisticWrites := repo.(optimisticDocumentMutationRepository)
	for _, document := range changedStoreValues(store.apiServices, persistedDocuments, func(value *domainvdoc.APIService) string { return value.ID }) {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertDocumentIfUnchanged(ctx, document, persistedDocuments[document.ID])
		} else {
			err = repo.UpsertDocument(ctx, document)
		}
		if err != nil {
			return err
		}
	}
	for _, branch := range changedStoreValues(store.branches, persistedBranches, func(value *domainvdoc.ContractBranch) string { return value.ID }) {
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertDocumentBranchIfUnchanged(ctx, branch, persistedBranches[branch.ID])
		} else {
			err = repo.UpsertDocumentBranch(ctx, branch)
		}
		if err != nil {
			return err
		}
	}
	for _, draft := range changedStoreValues(store.drafts, persistedDrafts, func(value *domainvdoc.ContractDraft) string { return value.ID }) {
		document := store.apiServices[documentIdentity(draft.DocumentID, draft.ServiceID)]
		var err error
		if supportsOptimisticWrites {
			err = optimisticRepo.UpsertDocumentDraftIfUnchanged(ctx, draft, persistedDrafts[draft.ID], document)
		} else {
			err = repo.UpsertDocumentDraft(ctx, draft, document)
		}
		if err != nil {
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
	s.shares = loaded.Shares
	s.aiProviders = loaded.AIProviders
	s.aiPrompts = loaded.AIPrompts
	s.aiSummaries = loaded.AISummaries
	s.aiChats = loaded.AIChats
	s.aiMessages = loaded.AIMessages
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
		Shares:      s.shares,
		AIProviders: s.aiProviders,
		AIPrompts:   s.aiPrompts,
		AISummaries: s.aiSummaries,
		AIChats:     s.aiChats,
		AIMessages:  s.aiMessages,
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
		state.Drafts[key] = cloneDraft(value)
	}
	for key, value := range s.versions {
		copied := *value
		state.Versions[key] = &copied
	}
	for key, value := range s.endpoints {
		state.Endpoints[key] = cloneEndpoint(value)
	}
	for key, value := range s.diffs {
		state.Diffs[key] = cloneDiff(value)
	}
	for key, value := range s.tokens {
		state.Tokens[key] = cloneToken(value)
	}
	for key, value := range s.shares {
		state.Shares[key] = domainshare.Clone(value)
	}
	for key, value := range s.aiProviders {
		state.AIProviders[key] = cloneAIProvider(value)
	}
	for key, value := range s.aiPrompts {
		copied := *value
		state.AIPrompts[key] = &copied
	}
	for key, value := range s.aiSummaries {
		copied := *value
		state.AISummaries[key] = &copied
	}
	for key, value := range s.aiChats {
		copied := *value
		state.AIChats[key] = &copied
	}
	for key, value := range s.aiMessages {
		copied := *value
		state.AIMessages[key] = &copied
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

func changedStoreValues[T any](items, persisted map[string]*T, key func(*T) string) []*T {
	changed := make(map[string]*T)
	for itemKey, value := range items {
		previous, ok := persisted[itemKey]
		if !ok || !reflect.DeepEqual(previous, value) {
			changed[itemKey] = value
		}
	}
	return sortedStoreValues(changed, key)
}

func documentIdentity(documentID, serviceID string) string {
	if documentID != "" {
		return documentID
	}
	return serviceID
}

func (s *Store) hydrateDraftSchemaLocked(ctx context.Context, draft *ContractDraft) error {
	if err := s.hydrateDraftContentLocked(ctx, draft, "raw"); err != nil {
		return err
	}
	return s.hydrateDraftContentLocked(ctx, draft, "normalized")
}

func (s *Store) hydrateVersionSchemaLocked(ctx context.Context, version *ContractVersion) error {
	if err := s.hydrateVersionContentLocked(ctx, version, "raw"); err != nil {
		return err
	}
	return s.hydrateVersionContentLocked(ctx, version, "normalized")
}

func (s *Store) hydrateDraftContentLocked(ctx context.Context, draft *ContractDraft, kind string) error {
	if draft == nil {
		return nil
	}
	switch kind {
	case "raw":
		if err := s.hydrateStoredContentLocked(ctx, draft.RawSchemaObjectKey, draft.RawSchemaHash, &draft.RawSchema); err != nil {
			return err
		}
		if s.persisted != nil && s.persisted.Drafts[draft.ID] != nil {
			s.persisted.Drafts[draft.ID].RawSchema = draft.RawSchema
		}
	case "normalized", "stable":
		if err := s.hydrateStoredContentLocked(ctx, draft.NormalizedObjectKey, draft.NormalizedSchemaHash, &draft.NormalizedSchema); err != nil {
			return err
		}
		if s.persisted != nil && s.persisted.Drafts[draft.ID] != nil {
			s.persisted.Drafts[draft.ID].NormalizedSchema = draft.NormalizedSchema
		}
	default:
		return fmt.Errorf("%w: unsupported draft content kind %q", ErrInvalidArgument, kind)
	}
	return nil
}

func (s *Store) hydrateVersionContentLocked(ctx context.Context, version *ContractVersion, kind string) error {
	if version == nil {
		return nil
	}
	switch kind {
	case "raw":
		if err := s.hydrateStoredContentLocked(ctx, version.RawSchemaObjectKey, version.RawSchemaHash, &version.RawSchema); err != nil {
			return err
		}
		if s.persisted != nil && s.persisted.Versions[version.ID] != nil {
			s.persisted.Versions[version.ID].RawSchema = version.RawSchema
		}
	case "normalized", "stable":
		if err := s.hydrateStoredContentLocked(ctx, version.NormalizedObjectKey, version.NormalizedSchemaHash, &version.NormalizedSchema); err != nil {
			return err
		}
		if s.persisted != nil && s.persisted.Versions[version.ID] != nil {
			s.persisted.Versions[version.ID].NormalizedSchema = version.NormalizedSchema
		}
	default:
		return fmt.Errorf("%w: unsupported version content kind %q", ErrInvalidArgument, kind)
	}
	return nil
}

func (s *Store) hydrateStoredContentLocked(ctx context.Context, objectKey, expectedHash string, content *string) error {
	if content == nil {
		return nil
	}
	if *content != "" {
		return validateStoredObjectBody(objectKey, expectedHash, []byte(*content))
	}
	if objectKey == "" {
		return nil
	}
	body, err := s.readVerifiedObject(ctx, objectKey, expectedHash)
	if err != nil {
		return err
	}
	*content = string(body)
	return nil
}
