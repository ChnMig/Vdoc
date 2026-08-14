package vdoc

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"vdoc/db/pgdb"
	domainvdoc "vdoc/domain/vdoc"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ database *gorm.DB }

const (
	vdocInvariantLockNamespace int32 = 0x56444f43 // "VDOC"
	superAdminInvariantLock    int32 = 1
	projectAdminInvariantLock  int32 = 2
)

func NewRepository(database *gorm.DB) *Repository { return &Repository{database: database} }

// WithinTransaction executes one service persistence unit against a single
// PostgreSQL transaction. The callback receives a repository bound to the
// transaction so business rows and their audit rows commit or roll back
// together.
func (r *Repository) WithinTransaction(ctx context.Context, fn func(domainvdoc.Repository) error) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	if fn == nil {
		return fmt.Errorf("%w: transaction callback is required", domainvdoc.ErrInvalidArgument)
	}
	return mapPostgresError(r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&Repository{database: tx})
	}))
}

func (r *Repository) LoadState(ctx context.Context) (*domainvdoc.State, error) {
	if r == nil || r.database == nil {
		return nil, fmt.Errorf("postgres repository is not initialized")
	}
	state := domainvdoc.NewState()
	if err := r.loadUsers(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadTeams(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadProjects(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadProjectMembers(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadDocuments(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadBranches(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadTokens(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadDocumentShares(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadDrafts(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadVersions(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadEndpoints(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadDiffs(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadAIProviders(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadAIPrompts(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadAISummaries(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadAIChats(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadAIMessages(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadAudits(ctx, state); err != nil {
		return nil, err
	}
	return state, nil
}

func (r *Repository) LoadUser(ctx context.Context, userID string) (*domainvdoc.User, error) {
	if r == nil || r.database == nil {
		return nil, fmt.Errorf("postgres repository is not initialized")
	}
	var model User
	if err := r.database.WithContext(ctx).First(&model, "id = ?", userID).Error; err != nil {
		return nil, mapRecordLookupError(err)
	}
	return domainUserFromModel(model), nil
}

// ArchiveTeam serializes against project creation by locking the parent team,
// re-checks the active-project invariant, and commits the soft delete together
// with its audit record.
func (r *Repository) ArchiveTeam(ctx context.Context, teamID string, audit *domainvdoc.AuditLog) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	if strings.TrimSpace(teamID) == "" {
		return fmt.Errorf("%w: team id is required", domainvdoc.ErrInvalidArgument)
	}
	return mapPostgresError(r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		writer := &Repository{database: tx}
		var team Team
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&team, "id = ?", teamID).Error; err != nil {
			return mapRecordLookupError(err)
		}
		var activeProjects int64
		if err := tx.WithContext(ctx).Model(&Project{}).Where("team_id = ? AND status <> ?", teamID, domainvdoc.ProjectStatusArchived).Count(&activeProjects).Error; err != nil {
			return err
		}
		if activeProjects != 0 {
			return fmt.Errorf("%w: team has active projects", domainvdoc.ErrFailedPrecondition)
		}
		result := tx.WithContext(ctx).Delete(&team)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return domainvdoc.ErrNotFound
		}
		return writer.RecordAudit(ctx, audit)
	}))
}

// LoadPublicDocumentShareSnapshot loads only the rows needed to authorize and
// serve an anonymous share request. Keeping this read model separate prevents
// public traffic from loading and later upserting the entire Vdoc state.
func (r *Repository) LoadPublicDocumentShareSnapshot(ctx context.Context, shareID string) (*domainvdoc.PublicDocumentShareSnapshot, error) {
	if r == nil || r.database == nil {
		return nil, fmt.Errorf("postgres repository is not initialized")
	}
	var shareModel DocumentShare
	if err := r.database.WithContext(ctx).First(&shareModel, "id = ?", shareID).Error; err != nil {
		return nil, err
	}
	var projectModel Project
	if err := r.database.WithContext(ctx).First(&projectModel, "id = ?", shareModel.ProjectID).Error; err != nil {
		return nil, err
	}
	var documentModel Document
	if err := r.database.WithContext(ctx).First(&documentModel, "id = ?", shareModel.DocumentID).Error; err != nil {
		return nil, err
	}
	var branchModel DocumentBranch
	if err := r.database.WithContext(ctx).First(&branchModel, "id = ?", shareModel.BranchID).Error; err != nil {
		return nil, err
	}
	var versionModels []DocumentVersion
	if err := r.database.WithContext(ctx).
		Where("document_id = ? AND branch_id = ? AND status = ?", shareModel.DocumentID, shareModel.BranchID, domainvdoc.VersionStatusPublished).
		Order("published_at DESC, id DESC").
		Find(&versionModels).Error; err != nil {
		return nil, err
	}
	versions := make([]*domainvdoc.ContractVersion, 0, len(versionModels))
	for _, model := range versionModels {
		versions = append(versions, domainDocumentVersionFromModel(model))
	}
	return &domainvdoc.PublicDocumentShareSnapshot{
		Share: domainDocumentShareFromModel(shareModel),
		Project: &domainvdoc.Project{
			ID: domainID(projectModel.ID), TeamID: domainID(projectModel.TeamID), Name: projectModel.Name,
			Description: stringValue(projectModel.Description), Status: projectModel.Status, CreatedBy: domainID(projectModel.CreatedBy),
			CreatedAt: projectModel.CreatedAt, UpdatedAt: projectModel.UpdatedAt,
		},
		Document: &domainvdoc.APIService{
			ID: domainID(documentModel.ID), ProjectID: domainID(documentModel.ProjectID), Name: documentModel.Name,
			DocumentType: documentModel.DocumentType, RelativePath: documentModel.RelativePath, BasePath: documentModel.RelativePath,
			Description: stringValue(documentModel.Description), Status: documentModel.Status, CreatedBy: domainID(documentModel.CreatedBy),
			CreatedAt: documentModel.CreatedAt, UpdatedAt: documentModel.UpdatedAt,
		},
		Branch: &domainvdoc.ContractBranch{
			ID: domainID(branchModel.ID), DocumentID: domainID(branchModel.DocumentID), ServiceID: domainID(branchModel.DocumentID),
			Name: branchModel.Name, Kind: branchModel.Kind, Description: stringValue(branchModel.Description),
			IsDefault: branchModel.IsDefault, IsProtected: branchModel.IsProtected, Status: branchModel.Status,
			CreatedBy: domainID(branchModel.CreatedBy), CreatedAt: branchModel.CreatedAt, UpdatedAt: branchModel.UpdatedAt,
		},
		Versions: versions,
	}, nil
}

// RecordPublicDocumentShareAccess linearizes a successful anonymous access
// against share revocation and parent-resource archival. The unlocked first
// read only discovers the lock keys; every row is then re-read under a shared
// lock in the same order used by normal persistence writes.
func (r *Repository) RecordPublicDocumentShareAccess(ctx context.Context, shareID string, audit *domainvdoc.AuditLog) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	if strings.TrimSpace(shareID) == "" || audit == nil {
		return fmt.Errorf("%w: share id and audit are required", domainvdoc.ErrInvalidArgument)
	}
	return mapPostgresError(r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var discovered DocumentShare
		if err := tx.WithContext(ctx).
			Select("id", "project_id", "document_id", "branch_id").
			First(&discovered, "id = ?", shareID).Error; err != nil {
			return mapRecordLookupError(err)
		}

		var project Project
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&project, "id = ?", discovered.ProjectID).Error; err != nil {
			return mapRecordLookupError(err)
		}
		var document Document
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&document, "id = ?", discovered.DocumentID).Error; err != nil {
			return mapRecordLookupError(err)
		}
		var branch DocumentBranch
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&branch, "id = ?", discovered.BranchID).Error; err != nil {
			return mapRecordLookupError(err)
		}
		var share DocumentShare
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&share, "id = ?", shareID).Error; err != nil {
			return mapRecordLookupError(err)
		}

		now := time.Now().UTC()
		shareActive := share.Status == domainvdoc.DocumentShareStatusActive && (share.ExpiresAt == nil || now.Before(*share.ExpiresAt))
		parentsActive := project.Status == domainvdoc.ProjectStatusActive &&
			document.ProjectID == project.ID && document.Status == domainvdoc.DocumentStatusActive &&
			branch.DocumentID == document.ID && branch.Status == domainvdoc.BranchStatusActive
		identitiesMatch := share.ProjectID == project.ID && share.DocumentID == document.ID && share.BranchID == branch.ID
		if !shareActive || !parentsActive || !identitiesMatch {
			return fmt.Errorf("%w: public document share is no longer available", domainvdoc.ErrFailedPrecondition)
		}
		return (&Repository{database: tx}).RecordAudit(ctx, audit)
	}))
}

func (r *Repository) RecordObject(ctx context.Context, ref domainvdoc.ObjectRef) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	contentType := ref.ContentType
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	return mapPostgresError(r.database.WithContext(ctx).Exec(`
INSERT INTO vdoc_schema_objects(object_key, kind, owner_type, owner_id, sha256, content_type, size_bytes, etag, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (object_key) DO UPDATE SET kind=EXCLUDED.kind, owner_type=EXCLUDED.owner_type, owner_id=EXCLUDED.owner_id, sha256=EXCLUDED.sha256, content_type=EXCLUDED.content_type, size_bytes=EXCLUDED.size_bytes, etag=EXCLUDED.etag, metadata=EXCLUDED.metadata`, ref.Key, ref.Kind, ref.OwnerType, nullIfEmpty(ref.OwnerID), ref.Hash, contentType, ref.SizeBytes, nullIfEmpty(ref.ETag), pgdb.NewJSONB(ref.Metadata, "{}")).Error)
}

func (r *Repository) RecordAudit(ctx context.Context, audit *domainvdoc.AuditLog) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	if audit == nil {
		return nil
	}
	return r.insertByIDIgnoreConflict(ctx, auditLogModelFromDomain(audit))
}

func (r *Repository) UpsertUser(ctx context.Context, user *domainvdoc.User) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := userModelFromDomain(user)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertUserIfUnchanged(ctx context.Context, user, previous *domainvdoc.User) error {
	model := userModelFromDomain(user)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) ResetSuperAdminPassword(ctx context.Context, email, passwordHash string) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || strings.TrimSpace(passwordHash) == "" {
		return fmt.Errorf("%w: admin email and password hash are required", domainvdoc.ErrInvalidArgument)
	}
	result := r.database.WithContext(ctx).Model(&User{}).
		Where("LOWER(email) = ? AND is_super_admin = ? AND status = ?", email, true, domainvdoc.UserStatusActive).
		Update("password_hash", passwordHash)
	if result.Error != nil {
		return mapPostgresError(result.Error)
	}
	if result.RowsAffected != 1 {
		return domainvdoc.ErrNotFound
	}
	return nil
}

func (r *Repository) UpsertTeam(ctx context.Context, team *domainvdoc.Team) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := teamModelFromDomain(team)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertTeamIfUnchanged(ctx context.Context, team, previous *domainvdoc.Team) error {
	model := teamModelFromDomain(team)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) UpsertProject(ctx context.Context, project *domainvdoc.Project) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := projectModelFromDomain(project)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertProjectIfUnchanged(ctx context.Context, project, previous *domainvdoc.Project) error {
	model := projectModelFromDomain(project)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) UpsertProjectMember(ctx context.Context, member *domainvdoc.ProjectMember) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := projectMemberModelFromDomain(member)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertProjectMemberIfUnchanged(ctx context.Context, member, previous *domainvdoc.ProjectMember) error {
	model := projectMemberModelFromDomain(member)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

// LockCollaborationInvariants serializes the cross-row administrator checks.
// These locks must be acquired from the transaction-bound repository passed to
// postgresPersistence.saveLockedWithRepository and in this fixed order.
func (r *Repository) LockCollaborationInvariants(ctx context.Context, superAdmin, projectAdmin bool) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	locks := make([]int32, 0, 2)
	if superAdmin {
		locks = append(locks, superAdminInvariantLock)
	}
	if projectAdmin {
		locks = append(locks, projectAdminInvariantLock)
	}
	for _, lockID := range locks {
		if err := r.database.WithContext(ctx).Exec(
			"SELECT pg_advisory_xact_lock(?, ?)",
			vdocInvariantLockNamespace,
			lockID,
		).Error; err != nil {
			return fmt.Errorf("lock collaboration invariant %d: %w", lockID, err)
		}
	}
	return nil
}

// ValidateCollaborationInvariants re-reads PostgreSQL after the pending writes
// have been applied. userIDs are resolved to their current admin memberships
// only after the advisory lock is held, which closes the race between creating
// a project for a user and disabling that user on another application instance.
func (r *Repository) ValidateCollaborationInvariants(ctx context.Context, superAdmin bool, projectIDs, userIDs []string) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	database := r.database.WithContext(ctx)
	if superAdmin {
		var activeSuperAdmins int64
		if err := database.Model(&User{}).
			Where("status = ? AND is_super_admin = ?", domainvdoc.UserStatusActive, true).
			Count(&activeSuperAdmins).Error; err != nil {
			return fmt.Errorf("count active SuperAdmins: %w", err)
		}
		if activeSuperAdmins == 0 {
			return fmt.Errorf("%w: cannot remove the last active SuperAdmin", domainvdoc.ErrFailedPrecondition)
		}
	}

	affectedProjects := make(map[string]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		if strings.TrimSpace(projectID) != "" {
			affectedProjects[projectID] = struct{}{}
		}
	}
	if len(userIDs) > 0 {
		var membershipProjectIDs []string
		if err := database.Model(&ProjectMember{}).
			Distinct("project_id").
			Where("user_id IN ?", userIDs).
			Where("role = ? AND status = ?", domainvdoc.MemberRoleAdmin, domainvdoc.MemberStatusActive).
			Pluck("project_id", &membershipProjectIDs).Error; err != nil {
			return fmt.Errorf("list affected project admin memberships: %w", err)
		}
		for _, projectID := range membershipProjectIDs {
			affectedProjects[projectID] = struct{}{}
		}
	}
	if len(affectedProjects) == 0 {
		return nil
	}
	candidates := make([]string, 0, len(affectedProjects))
	for projectID := range affectedProjects {
		candidates = append(candidates, projectID)
	}
	sort.Strings(candidates)

	var projectsWithoutAdmin []string
	if err := database.Table(TableNameProjects+" AS candidate_project").
		Where("candidate_project.id IN ?", candidates).
		Where("candidate_project.status = ? AND candidate_project.deleted_at IS NULL", domainvdoc.ProjectStatusActive).
		Where(`NOT EXISTS (
			SELECT 1
			FROM project_members AS active_member
			JOIN users AS active_user ON active_user.id = active_member.user_id
			WHERE active_member.project_id = candidate_project.id
			  AND active_member.role = ?
			  AND active_member.status = ?
			  AND active_member.deleted_at IS NULL
			  AND active_user.status = ?
			  AND active_user.deleted_at IS NULL
		)`, domainvdoc.MemberRoleAdmin, domainvdoc.MemberStatusActive, domainvdoc.UserStatusActive).
		Order("candidate_project.id").
		Limit(1).
		Pluck("candidate_project.id", &projectsWithoutAdmin).Error; err != nil {
		return fmt.Errorf("validate active project administrators: %w", err)
	}
	if len(projectsWithoutAdmin) > 0 {
		return fmt.Errorf("%w: cannot leave active project %s without an active admin", domainvdoc.ErrFailedPrecondition, projectsWithoutAdmin[0])
	}
	return nil
}

func (r *Repository) UpsertDocumentShare(ctx context.Context, share *domainvdoc.DocumentShare) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := documentShareModelFromDomain(share)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertDocumentShareIfUnchanged(ctx context.Context, share, previous *domainvdoc.DocumentShare) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := documentShareModelFromDomain(share)
	if model == nil {
		return nil
	}
	if previous == nil {
		return mapPostgresError(r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			writer := &Repository{database: tx}
			if err := writer.lockActiveDocumentShareParents(ctx, model); err != nil {
				return err
			}
			return writer.upsertByIDIfUnchanged(ctx, model, model.ID, nil)
		}))
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) lockActiveDocumentShareParents(ctx context.Context, share *DocumentShare) error {
	var project Project
	if err := r.database.WithContext(ctx).
		Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND status = ?", share.ProjectID, domainvdoc.ProjectStatusActive).
		First(&project).Error; err != nil {
		return mapRecordLookupError(err)
	}
	var document Document
	if err := r.database.WithContext(ctx).
		Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND project_id = ? AND status = ?", share.DocumentID, share.ProjectID, domainvdoc.DocumentStatusActive).
		First(&document).Error; err != nil {
		return mapRecordLookupError(err)
	}
	var branch DocumentBranch
	if err := r.database.WithContext(ctx).
		Clauses(clause.Locking{Strength: "SHARE"}).
		Where("id = ? AND document_id = ? AND status = ?", share.BranchID, share.DocumentID, domainvdoc.BranchStatusActive).
		First(&branch).Error; err != nil {
		return mapRecordLookupError(err)
	}
	return nil
}

func (r *Repository) UpsertDocumentDiff(ctx context.Context, diff *domainvdoc.Diff, fromVersion, toVersion *domainvdoc.ContractVersion) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	if diff == nil || fromVersion == nil || toVersion == nil {
		return nil
	}
	return mapPostgresError(r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		writer := &Repository{database: tx}
		if err := writer.upsertDocumentDiff(ctx, diff, fromVersion, toVersion); err != nil {
			return err
		}
		for _, item := range sortedDiffItems(diff.Items) {
			if err := writer.upsertDocumentDiffItem(ctx, diff, item); err != nil {
				return err
			}
		}
		return nil
	}))
}

func (r *Repository) UpsertDocument(ctx context.Context, document *domainvdoc.APIService) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := documentModelFromDomain(document)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertDocumentIfUnchanged(ctx context.Context, document, previous *domainvdoc.APIService) error {
	model := documentModelFromDomain(document)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) UpsertDocumentBranch(ctx context.Context, branch *domainvdoc.ContractBranch) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := documentBranchModelFromDomain(branch)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertDocumentBranchIfUnchanged(ctx context.Context, branch, previous *domainvdoc.ContractBranch) error {
	model := documentBranchModelFromDomain(branch)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) UpsertDocumentDraft(ctx context.Context, draft *domainvdoc.ContractDraft, document *domainvdoc.APIService) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := documentDraftModelFromDomain(draft, document)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertDocumentDraftIfUnchanged(ctx context.Context, draft, previous *domainvdoc.ContractDraft, document *domainvdoc.APIService) error {
	model := documentDraftModelFromDomain(draft, document)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) UpsertMCPToken(ctx context.Context, token *domainvdoc.MCPToken) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	model := mcpTokenModelFromDomain(token)
	if model == nil {
		return nil
	}
	return r.upsertByID(ctx, model)
}

func (r *Repository) UpsertMCPTokenIfUnchanged(ctx context.Context, token, previous *domainvdoc.MCPToken) error {
	model := mcpTokenModelFromDomain(token)
	if model == nil {
		return nil
	}
	return r.upsertByIDIfUnchanged(ctx, model, model.ID, domainUpdatedAt(previous))
}

func (r *Repository) PublishState(ctx context.Context, input domainvdoc.PublishStateInput) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	if input.State == nil {
		return fmt.Errorf("%w: publish state is required", domainvdoc.ErrInvalidArgument)
	}
	version := input.State.Versions[input.VersionID]
	draft := input.State.Drafts[input.DraftID]
	if version == nil || draft == nil {
		return fmt.Errorf("%w: publish version and draft are required", domainvdoc.ErrFailedPrecondition)
	}
	if domainDocumentID(version.DocumentID, version.ServiceID) != input.ServiceID || version.BranchID != input.BranchID || version.DraftID != input.DraftID || version.VersionName != input.VersionName || draft.Status != domainvdoc.DraftStatusPublished {
		return fmt.Errorf("%w: inconsistent publish state", domainvdoc.ErrFailedPrecondition)
	}
	return mapPostgresError(r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		writer := &Repository{database: tx}
		currentDraft, err := writer.lockPublishScope(ctx, input)
		if err != nil {
			return err
		}
		if currentDraft.Status != domainvdoc.DraftStatusSubmitted {
			return domainvdoc.ErrFailedPrecondition
		}
		if err := writer.ensureChangedFromLatest(ctx, input.ServiceID, input.BranchID, currentDraft.NormalizedSchemaHash); err != nil {
			return err
		}
		if err := writer.ensureVersionAvailable(ctx, input.ServiceID, input.BranchID, input.VersionName); err != nil {
			return err
		}
		for _, ref := range input.ObjectRefs {
			if ref.Key == "" {
				continue
			}
			if err := writer.RecordObject(ctx, ref); err != nil {
				return err
			}
		}
		versionNo, err := writer.nextVersionNo(ctx, input.ServiceID, input.BranchID)
		if err != nil {
			return err
		}
		if err := writer.insertPublishedVersion(ctx, version, versionNo, endpointCount(input.State.Endpoints, version.ID)); err != nil {
			return err
		}
		if err := writer.insertPublishedEndpoints(ctx, input.State.Endpoints, input.State.Versions, version.ID); err != nil {
			return err
		}
		if err := writer.insertPublishedDiffs(ctx, input.State.Diffs, input.State.Versions, version.ID); err != nil {
			return err
		}
		if err := writer.markDraftPublished(ctx, input, draft.UpdatedAt); err != nil {
			return err
		}
		return writer.insertPublishAudit(ctx, input)
	}))
}

func (r *Repository) upsertByID(ctx context.Context, value any) error {
	return mapPostgresError(r.database.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(value).Error)
}

func (r *Repository) upsertByIDIfUnchanged(ctx context.Context, value any, id string, expectedUpdatedAt *time.Time) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	if value == nil || strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: optimistic upsert value and id are required", domainvdoc.ErrInvalidArgument)
	}
	return mapPostgresError(r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if expectedUpdatedAt == nil {
			touchModelUpdatedAt(value, time.Now().UTC())
			result := tx.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(value)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%w: row %s already exists", domainvdoc.ErrAlreadyExists, id)
			}
			return nil
		}

		var current struct {
			UpdatedAt time.Time `gorm:"column:updated_at"`
		}
		if err := tx.WithContext(ctx).
			Model(value).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("updated_at").
			Where("id = ?", id).
			Take(&current).Error; err != nil {
			return mapRecordLookupError(err)
		}
		if !current.UpdatedAt.Equal(*expectedUpdatedAt) {
			return fmt.Errorf("%w: row %s changed since it was loaded", domainvdoc.ErrFailedPrecondition, id)
		}
		touchModelUpdatedAt(value, time.Now().UTC())
		return tx.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(value).Error
	}))
}

func touchModelUpdatedAt(value any, updatedAt time.Time) {
	model := reflect.ValueOf(value)
	if model.Kind() != reflect.Pointer || model.IsNil() {
		return
	}
	field := model.Elem().FieldByName("UpdatedAt")
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(updatedAt))
	}
}

func domainUpdatedAt(value any) *time.Time {
	var updatedAt time.Time
	switch model := value.(type) {
	case *domainvdoc.User:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.Team:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.Project:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.ProjectMember:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.APIService:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.ContractBranch:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.ContractDraft:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.MCPToken:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.DocumentShare:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.AIProviderConfig:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.AIPromptOverride:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.AISummary:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	case *domainvdoc.AIChatSession:
		if model == nil {
			return nil
		}
		updatedAt = model.UpdatedAt
	default:
		return nil
	}
	return &updatedAt
}

func (r *Repository) insertByIDIgnoreConflict(ctx context.Context, value any) error {
	return mapPostgresError(r.database.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(value).Error)
}

func (r *Repository) saveEndpointDetail(ctx context.Context, endpoint *domainvdoc.Endpoint) error {
	return mapPostgresError(r.database.WithContext(ctx).Exec(`
	INSERT INTO api_endpoint_details(endpoint_id,parameters_json,request_body_json,responses_json,security_json,servers_json,normalized_operation_json,schema_refs_json)
	VALUES(?,?,?,?,?,?,?,?)
	ON CONFLICT (endpoint_id) DO NOTHING`, endpoint.ID, pgdb.NewJSONB(endpoint.Parameters, "[]"), nullableJSON(endpoint.RequestBody), pgdb.NewJSONB(endpoint.Responses, "{}"), nullableJSON(endpoint.Security), nullableJSON(endpoint.Servers), pgdb.NewJSONB(endpoint.NormalizedOperation, "{}"), nullableJSON(endpoint.SchemaRefs)).Error)
}

func (r *Repository) lockPublishScope(ctx context.Context, input domainvdoc.PublishStateInput) (*DocumentDraft, error) {
	var service Document
	if err := r.database.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND project_id = ?", input.ServiceID, input.ProjectID).First(&service).Error; err != nil {
		return nil, mapRecordLookupError(err)
	}
	var branch DocumentBranch
	if err := r.database.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND document_id = ?", input.BranchID, input.ServiceID).First(&branch).Error; err != nil {
		return nil, mapRecordLookupError(err)
	}
	var draft DocumentDraft
	if err := r.database.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND document_id = ? AND branch_id = ?", input.DraftID, input.ServiceID, input.BranchID).First(&draft).Error; err != nil {
		return nil, mapRecordLookupError(err)
	}
	return &draft, nil
}

func (r *Repository) ensureVersionAvailable(ctx context.Context, serviceID, branchID, versionName string) error {
	var count int64
	if err := r.database.WithContext(ctx).Model(&DocumentVersion{}).Where("document_id = ? AND branch_id = ? AND version_name = ?", serviceID, branchID, versionName).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return domainvdoc.ErrAlreadyExists
	}
	return nil
}

func (r *Repository) ensureChangedFromLatest(ctx context.Context, serviceID, branchID, candidateHash string) error {
	var latest DocumentVersion
	err := r.database.WithContext(ctx).Where("document_id = ? AND branch_id = ?", serviceID, branchID).Order("published_at DESC, id DESC").First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if latest.NormalizedSchemaHash == candidateHash {
		return fmt.Errorf("%w: schema has no changes from latest version", domainvdoc.ErrFailedPrecondition)
	}
	return nil
}

func (r *Repository) nextVersionNo(ctx context.Context, serviceID, branchID string) (int, error) {
	var count int64
	if err := r.database.WithContext(ctx).Model(&DocumentVersion{}).Where("document_id = ? AND branch_id = ?", serviceID, branchID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count) + 1, nil
}

func (r *Repository) insertPublishedVersion(ctx context.Context, version *domainvdoc.ContractVersion, versionNo, endpoints int) error {
	return r.database.WithContext(ctx).Create(documentVersionModelFromDomain(version, versionNo, endpoints)).Error
}

func documentVersionModelFromDomain(version *domainvdoc.ContractVersion, versionNo, endpoints int) *DocumentVersion {
	if version == nil {
		return nil
	}
	return &DocumentVersion{Base: pgdb.Base{ID: version.ID, CreatedAt: nonZeroTime(version.CreatedAt), UpdatedAt: nonZeroTime(version.UpdatedAt)}, ProjectID: version.ProjectID, DocumentID: domainDocumentID(version.DocumentID, version.ServiceID), BranchID: version.BranchID, VersionName: version.VersionName, VersionNo: versionNo, RelativePath: version.RelativePath, Status: version.Status, SourceDraftID: version.DraftID, SourceType: version.SourceType, SourceBranchID: stringPtr(version.SourceBranchID), SourceVersionID: stringPtr(version.SourceVersionID), BaseVersionID: stringPtr(version.BaseVersionID), DocumentFormat: version.SchemaFormat, RawSchemaObjectKey: version.RawSchemaObjectKey, NormalizedSchemaObjectKey: version.NormalizedObjectKey, RawSchemaHash: version.RawSchemaHash, NormalizedSchemaHash: version.NormalizedSchemaHash, SchemaSizeBytes: int64(len(version.RawSchema)), SchemaMetadata: pgdb.JSONB(`{}`), Changelog: stringPtr(version.Changelog), SourceGitCommitID: stringPtr(version.SourceGitCommitID), EndpointCount: endpoints, PublishedBy: version.PublishedBy, PublishedAt: nonZeroTime(version.PublishedAt)}
}

func (r *Repository) insertPublishedEndpoints(ctx context.Context, endpoints map[string]*domainvdoc.Endpoint, versions map[string]*domainvdoc.ContractVersion, versionID string) error {
	for _, endpoint := range sortedValues(endpoints, func(value *domainvdoc.Endpoint) string { return value.ID }) {
		if endpoint.ContractVersionID != versionID {
			continue
		}
		version := versions[endpoint.ContractVersionID]
		if version == nil {
			return fmt.Errorf("%w: endpoint version missing", domainvdoc.ErrFailedPrecondition)
		}
		method, ok := methodToCode(endpoint.Method)
		if !ok {
			return fmt.Errorf("%w: unsupported endpoint method %q", domainvdoc.ErrInvalidArgument, endpoint.Method)
		}
		if err := r.database.WithContext(ctx).Create(&APIEndpoint{Base: pgdb.Base{ID: endpoint.ID, CreatedAt: nonZeroTime(endpoint.CreatedAt), UpdatedAt: nonZeroTime(endpoint.UpdatedAt)}, DocumentVersionID: endpoint.ContractVersionID, DocumentID: domainDocumentID(version.DocumentID, version.ServiceID), BranchID: version.BranchID, Method: method, Path: endpoint.Path, OperationID: stringPtr(endpoint.OperationID), Summary: stringPtr(endpoint.Summary), Tags: pgdb.StringArray(endpoint.Tags), Deprecated: endpoint.Deprecated, RequestHash: endpoint.Hash, ResponseHash: endpoint.Hash, EndpointHash: endpoint.Hash}).Error; err != nil {
			return err
		}
		if err := r.database.WithContext(ctx).Create(&APIEndpointDetail{EndpointID: endpoint.ID, ParametersJSON: pgdb.NewJSONB(endpoint.Parameters, "[]"), RequestBodyJSON: nullableJSON(endpoint.RequestBody), ResponsesJSON: pgdb.NewJSONB(endpoint.Responses, "{}"), SecurityJSON: nullableJSON(endpoint.Security), ServersJSON: nullableJSON(endpoint.Servers), NormalizedOperationJSON: pgdb.NewJSONB(endpoint.NormalizedOperation, "{}"), SchemaRefsJSON: nullableJSON(endpoint.SchemaRefs)}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) insertPublishedDiffs(ctx context.Context, diffs map[string]*domainvdoc.Diff, versions map[string]*domainvdoc.ContractVersion, versionID string) error {
	for _, diff := range sortedValues(diffs, func(value *domainvdoc.Diff) string { return value.ID }) {
		if diff.ToVersionID != versionID {
			continue
		}
		fromVersion := versions[diff.FromVersionID]
		toVersion := versions[diff.ToVersionID]
		if fromVersion == nil || toVersion == nil || diff.FromVersionID == diff.ToVersionID {
			return fmt.Errorf("%w: diff versions missing", domainvdoc.ErrFailedPrecondition)
		}
		generatedAt := nonZeroTime(diff.CreatedAt)
		if err := r.database.WithContext(ctx).Create(&DocumentVersionDiff{Base: pgdb.Base{ID: diff.ID, CreatedAt: nonZeroTime(diff.CreatedAt), UpdatedAt: nonZeroTime(diff.UpdatedAt)}, DocumentID: domainDocumentID(diff.DocumentID, diff.ServiceID), FromBranchID: fromVersion.BranchID, ToBranchID: toVersion.BranchID, FromVersionID: diff.FromVersionID, ToVersionID: diff.ToVersionID, DiffStatus: diff.DiffStatus, DiffObjectKey: stringPtr(diff.ObjectKey), DiffHash: stringPtr(diff.Hash), DiffSummaryJSON: pgdb.NewJSONB(diff.Summary, "{}"), BreakingChangesJSON: pgdb.JSONB(`{}`), AddedCount: diff.Summary.AddedEndpoints, ModifiedCount: diff.Summary.ModifiedEndpoints, RemovedCount: diff.Summary.RemovedEndpoints, BreakingCount: diff.Summary.BreakingChanges, GeneratedAt: &generatedAt}).Error; err != nil {
			return err
		}
		for _, item := range sortedDiffItems(diff.Items) {
			method, ok := optionalMethodToCode(item.Method)
			if !ok {
				return fmt.Errorf("%w: unsupported diff method %q", domainvdoc.ErrInvalidArgument, item.Method)
			}
			if err := r.database.WithContext(ctx).Create(&DocumentDiffItem{Base: pgdb.Base{ID: item.ID, CreatedAt: nonZeroTime(diff.CreatedAt), UpdatedAt: nonZeroTime(diff.UpdatedAt)}, DiffID: diff.ID, ChangeType: item.ChangeType, Severity: item.Severity, Method: method, Path: stringPtr(item.Path), OperationID: stringPtr(item.OperationID), Location: stringPtr(item.Location), OldValue: pgdb.NewJSONB(item.OldValue, "null"), NewValue: pgdb.NewJSONB(item.NewValue, "null"), Message: item.Message, FrontendImpact: stringPtr(item.FrontendImpact), IsBreaking: item.IsBreaking, SortOrder: item.SortOrder}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repository) upsertDocumentDiff(ctx context.Context, diff *domainvdoc.Diff, fromVersion, toVersion *domainvdoc.ContractVersion) error {
	generatedAt := nonZeroTime(diff.CreatedAt)
	model := &DocumentVersionDiff{Base: pgdb.Base{ID: diff.ID, CreatedAt: nonZeroTime(diff.CreatedAt), UpdatedAt: nonZeroTime(diff.UpdatedAt)}, DocumentID: domainDocumentID(diff.DocumentID, diff.ServiceID), FromBranchID: fromVersion.BranchID, ToBranchID: toVersion.BranchID, FromVersionID: diff.FromVersionID, ToVersionID: diff.ToVersionID, DiffStatus: diff.DiffStatus, DiffObjectKey: stringPtr(diff.ObjectKey), DiffHash: stringPtr(diff.Hash), DiffSummaryJSON: pgdb.NewJSONB(diff.Summary, "{}"), BreakingChangesJSON: pgdb.JSONB(`{}`), AddedCount: diff.Summary.AddedEndpoints, ModifiedCount: diff.Summary.ModifiedEndpoints, RemovedCount: diff.Summary.RemovedEndpoints, BreakingCount: diff.Summary.BreakingChanges, GeneratedAt: &generatedAt}
	return r.upsertByID(ctx, model)
}

func (r *Repository) upsertDocumentDiffItem(ctx context.Context, diff *domainvdoc.Diff, item domainvdoc.DiffItem) error {
	method, ok := optionalMethodToCode(item.Method)
	if !ok {
		return fmt.Errorf("%w: unsupported diff method %q", domainvdoc.ErrInvalidArgument, item.Method)
	}
	model := &DocumentDiffItem{Base: pgdb.Base{ID: item.ID, CreatedAt: nonZeroTime(diff.CreatedAt), UpdatedAt: nonZeroTime(diff.UpdatedAt)}, DiffID: diff.ID, ChangeType: item.ChangeType, Severity: item.Severity, Method: method, Path: stringPtr(item.Path), OperationID: stringPtr(item.OperationID), Location: stringPtr(item.Location), OldValue: pgdb.NewJSONB(item.OldValue, "null"), NewValue: pgdb.NewJSONB(item.NewValue, "null"), Message: item.Message, FrontendImpact: stringPtr(item.FrontendImpact), IsBreaking: item.IsBreaking, SortOrder: item.SortOrder}
	return r.upsertByID(ctx, model)
}

func (r *Repository) markDraftPublished(ctx context.Context, input domainvdoc.PublishStateInput, updatedAt time.Time) error {
	reviewComment := ""
	if input.State != nil && input.State.Drafts[input.DraftID] != nil {
		reviewComment = input.State.Drafts[input.DraftID].ReviewComment
	}
	result := r.database.WithContext(ctx).Model(&DocumentDraft{}).Where("id = ? AND status = ?", input.DraftID, domainvdoc.DraftStatusSubmitted).Updates(map[string]any{"status": domainvdoc.DraftStatusPublished, "review_comment": nullIfEmpty(reviewComment), "reviewed_by": input.ActorID, "reviewed_at": nonZeroTime(updatedAt), "published_version_id": input.VersionID, "updated_at": nonZeroTime(updatedAt)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domainvdoc.ErrFailedPrecondition
	}
	return nil
}

func (r *Repository) insertPublishAudit(ctx context.Context, input domainvdoc.PublishStateInput) error {
	foundPublishAudit := false
	if input.State != nil {
		for _, audit := range sortedValues(input.State.AuditLogs, func(value *domainvdoc.AuditLog) string {
			return value.CreatedAt.Format(time.RFC3339Nano) + ":" + value.ID
		}) {
			matchesReview := (audit.Action == "contract_draft.review" || audit.Action == "markdown_draft.review") && audit.ResourceID == input.DraftID
			matchesPublish := audit.Action == "document_version.publish" && audit.ResourceID == input.VersionID
			if !matchesReview && !matchesPublish {
				continue
			}
			if matchesPublish {
				foundPublishAudit = true
			}
			if err := r.RecordAudit(ctx, audit); err != nil {
				return err
			}
		}
	}
	if foundPublishAudit {
		return nil
	}
	metadata := map[string]string{"branch_id": input.BranchID, "draft_id": input.DraftID, "version_name": input.VersionName, "result": "success"}
	return r.RecordAudit(ctx, &domainvdoc.AuditLog{ActorType: domainvdoc.AuditActorUser, ActorUserID: input.ActorID, Action: "document_version.publish", ResourceType: "document_version", ResourceID: input.VersionID, ProjectID: input.ProjectID, ServiceID: input.ServiceID, Metadata: metadata})
}

func (r *Repository) loadUsers(ctx context.Context, loaded *domainvdoc.State) error {
	var models []User
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		userID := domainID(model.ID)
		loaded.Users[userID] = domainUserFromModel(model)
	}
	return nil
}

func domainUserFromModel(model User) *domainvdoc.User {
	return &domainvdoc.User{ID: domainID(model.ID), Email: model.Email, Name: model.DisplayName, PasswordHash: model.PasswordHash, IsSuperAdmin: model.IsSuperAdmin, Status: model.Status, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
}

func (r *Repository) loadTeams(ctx context.Context, loaded *domainvdoc.State) error {
	var models []Team
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		teamID := domainID(model.ID)
		loaded.Teams[teamID] = &domainvdoc.Team{ID: teamID, Name: model.Name, Description: stringValue(model.Description), CreatedBy: domainID(model.CreatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadProjects(ctx context.Context, loaded *domainvdoc.State) error {
	var models []Project
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		projectID := domainID(model.ID)
		loaded.Projects[projectID] = &domainvdoc.Project{ID: projectID, TeamID: domainID(model.TeamID), Name: model.Name, Description: stringValue(model.Description), Status: model.Status, CreatedBy: domainID(model.CreatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadProjectMembers(ctx context.Context, loaded *domainvdoc.State) error {
	var models []ProjectMember
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		member := &domainvdoc.ProjectMember{ProjectID: domainID(model.ProjectID), UserID: domainID(model.UserID), Role: model.Role, Status: model.Status, AddedBy: domainID(model.AddedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
		loaded.Members[memberKey(member.ProjectID, member.UserID)] = member
	}
	return nil
}

func (r *Repository) loadDocuments(ctx context.Context, loaded *domainvdoc.State) error {
	var models []Document
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		documentID := domainID(model.ID)
		loaded.APIServices[documentID] = &domainvdoc.APIService{ID: documentID, ProjectID: domainID(model.ProjectID), Name: model.Name, DocumentType: model.DocumentType, RelativePath: model.RelativePath, Description: stringValue(model.Description), BasePath: model.RelativePath, Status: model.Status, CreatedBy: domainID(model.CreatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadBranches(ctx context.Context, loaded *domainvdoc.State) error {
	var models []DocumentBranch
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		branchID := domainID(model.ID)
		documentID := domainID(model.DocumentID)
		loaded.Branches[branchID] = &domainvdoc.ContractBranch{ID: branchID, DocumentID: documentID, ServiceID: documentID, Name: model.Name, Kind: model.Kind, Description: stringValue(model.Description), IsDefault: model.IsDefault, IsProtected: model.IsProtected, Status: model.Status, CreatedBy: domainID(model.CreatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadTokens(ctx context.Context, loaded *domainvdoc.State) error {
	var models []MCPToken
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		token := domainMCPTokenFromModel(model)
		loaded.Tokens[token.ID] = token
	}
	return nil
}

func mcpTokenModelFromDomain(token *domainvdoc.MCPToken) *MCPToken {
	if token == nil {
		return nil
	}
	return &MCPToken{Base: pgdb.Base{ID: token.ID, CreatedAt: nonZeroTime(token.CreatedAt), UpdatedAt: nonZeroTime(token.UpdatedAt)}, UserID: token.UserID, Name: token.Name, TokenHash: token.TokenHash, TokenCiphertext: append([]byte(nil), token.TokenCiphertext...), CipherKID: token.CipherKID, Scopes: pgdb.SmallintArray(token.Scopes), Status: token.Status, ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt, RevokedAt: token.RevokedAt, RevokedBy: stringPtr(token.RevokedBy)}
}

func domainMCPTokenFromModel(model MCPToken) *domainvdoc.MCPToken {
	return &domainvdoc.MCPToken{ID: domainID(model.ID), UserID: domainID(model.UserID), Name: model.Name, TokenHash: model.TokenHash, TokenCiphertext: append([]byte(nil), model.TokenCiphertext...), CipherKID: model.CipherKID, Scopes: []int(model.Scopes), Status: model.Status, ExpiresAt: model.ExpiresAt, RevokedAt: model.RevokedAt, RevokedBy: stringPtrID(model.RevokedBy), LastUsedAt: model.LastUsedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
}

func (r *Repository) loadDocumentShares(ctx context.Context, loaded *domainvdoc.State) error {
	var models []DocumentShare
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		share := domainDocumentShareFromModel(model)
		loaded.Shares[share.ID] = share
	}
	return nil
}

func documentShareModelFromDomain(share *domainvdoc.DocumentShare) *DocumentShare {
	if share == nil {
		return nil
	}
	return &DocumentShare{
		Base:      pgdb.Base{ID: share.ID, CreatedAt: nonZeroTime(share.CreatedAt), UpdatedAt: nonZeroTime(share.UpdatedAt)},
		ProjectID: share.ProjectID, DocumentID: share.DocumentID, BranchID: share.BranchID,
		TokenHash: share.TokenHash, TokenCiphertext: append([]byte(nil), share.TokenCiphertext...), CipherKID: share.CipherKID,
		PasswordVerifier: stringPtr(share.PasswordVerifier), VersionScope: share.VersionScope, Status: share.Status,
		ExpiresAt: share.ExpiresAt, CreatedBy: share.CreatedBy, RevokedBy: stringPtr(share.RevokedBy), RevokedAt: share.RevokedAt,
	}
}

func domainDocumentShareFromModel(model DocumentShare) *domainvdoc.DocumentShare {
	return &domainvdoc.DocumentShare{
		ID: domainID(model.ID), ProjectID: domainID(model.ProjectID), DocumentID: domainID(model.DocumentID), BranchID: domainID(model.BranchID),
		TokenHash: model.TokenHash, TokenCiphertext: append([]byte(nil), model.TokenCiphertext...), CipherKID: model.CipherKID,
		PasswordVerifier: cloneOptionalString(model.PasswordVerifier), VersionScope: model.VersionScope, Status: model.Status,
		ExpiresAt: cloneOptionalTime(model.ExpiresAt), CreatedBy: domainID(model.CreatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
		RevokedBy: stringPtrID(model.RevokedBy), RevokedAt: cloneOptionalTime(model.RevokedAt),
	}
}

func (r *Repository) loadDrafts(ctx context.Context, loaded *domainvdoc.State) error {
	var models []DocumentDraft
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		documentID := domainID(model.DocumentID)
		draft := &domainvdoc.ContractDraft{ID: domainID(model.ID), DocumentID: documentID, ServiceID: documentID, BranchID: domainID(model.BranchID), VersionName: model.VersionName, Changelog: stringValue(model.Changelog), SourceGitCommitID: stringValue(model.SourceGitCommitID), SchemaFormat: model.DocumentFormat, SourceType: model.SourceType, SourceBranchID: stringValueID(model.SourceBranchID), SourceVersionID: stringValueID(model.SourceVersionID), BaseVersionID: stringValueID(model.BaseVersionID), RawSchemaObjectKey: model.RawSchemaObjectKey, NormalizedObjectKey: model.NormalizedSchemaObjectKey, RawSchemaHash: model.RawSchemaHash, NormalizedSchemaHash: model.NormalizedSchemaHash, Status: model.Status, DiffPreview: diffPreviewFromJSON(model.DiffPreviewJSON), ReviewComment: stringValue(model.ReviewComment), CreatedBy: domainID(model.CreatedByUserID), SubmittedAt: model.SubmittedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
		if draft.DiffPreview != nil {
			draft.DiffPreview.ObjectKey = stringValue(model.DiffPreviewObjectKey)
		}
		if document := loaded.APIServices[draft.ServiceID]; document != nil {
			draft.ProjectID = document.ProjectID
		}
		loaded.Drafts[draft.ID] = draft
	}
	return nil
}

func (r *Repository) loadVersions(ctx context.Context, loaded *domainvdoc.State) error {
	var models []DocumentVersion
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		version := domainDocumentVersionFromModel(model)
		loaded.Versions[version.ID] = version
	}
	return nil
}

func domainDocumentVersionFromModel(model DocumentVersion) *domainvdoc.ContractVersion {
	documentID := domainID(model.DocumentID)
	return &domainvdoc.ContractVersion{ID: domainID(model.ID), ProjectID: domainID(model.ProjectID), DocumentID: documentID, ServiceID: documentID, BranchID: domainID(model.BranchID), DraftID: domainID(model.SourceDraftID), VersionName: model.VersionName, RelativePath: model.RelativePath, Changelog: stringValue(model.Changelog), SourceGitCommitID: stringValue(model.SourceGitCommitID), SchemaFormat: model.DocumentFormat, SourceType: model.SourceType, SourceBranchID: stringValueID(model.SourceBranchID), SourceVersionID: stringValueID(model.SourceVersionID), BaseVersionID: stringValueID(model.BaseVersionID), RawSchemaObjectKey: model.RawSchemaObjectKey, NormalizedObjectKey: model.NormalizedSchemaObjectKey, RawSchemaHash: model.RawSchemaHash, NormalizedSchemaHash: model.NormalizedSchemaHash, Status: model.Status, PublishedBy: domainID(model.PublishedBy), PublishedAt: model.PublishedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
}

func (r *Repository) loadEndpoints(ctx context.Context, loaded *domainvdoc.State) error {
	rows, err := r.database.WithContext(ctx).Raw(`SELECT e.id::text,e.document_version_id::text,e.method,e.path,COALESCE(e.operation_id,''),COALESCE(e.summary,''),array_to_json(e.tags)::text,e.deprecated,e.endpoint_hash,e.created_at,e.updated_at,d.parameters_json,d.request_body_json,d.responses_json,d.security_json,d.servers_json,d.normalized_operation_json,d.schema_refs_json FROM api_endpoints e LEFT JOIN api_endpoint_details d ON d.endpoint_id=e.id`).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		endpoint := &domainvdoc.Endpoint{}
		var method int
		var tags string
		var parameters, requestBody, responses, security, servers, normalizedOperation, schemaRefs sql.NullString
		if err := rows.Scan(&endpoint.ID, &endpoint.ContractVersionID, &method, &endpoint.Path, &endpoint.OperationID, &endpoint.Summary, &tags, &endpoint.Deprecated, &endpoint.Hash, &endpoint.CreatedAt, &endpoint.UpdatedAt, &parameters, &requestBody, &responses, &security, &servers, &normalizedOperation, &schemaRefs); err != nil {
			return err
		}
		endpoint.ID = domainID(endpoint.ID)
		endpoint.ContractVersionID = domainID(endpoint.ContractVersionID)
		endpoint.Method = codeToMethod(method)
		endpoint.Tags = stringArrayFromJSON(tags)
		endpoint.Parameters = jsonToInterface(nullStringBytes(parameters))
		endpoint.RequestBody = jsonToInterface(nullStringBytes(requestBody))
		endpoint.Responses = jsonToInterface(nullStringBytes(responses))
		endpoint.Security = jsonToInterface(nullStringBytes(security))
		endpoint.Servers = jsonToInterface(nullStringBytes(servers))
		endpoint.NormalizedOperation = jsonToInterface(nullStringBytes(normalizedOperation))
		endpoint.SchemaRefs = jsonToInterface(nullStringBytes(schemaRefs))
		loaded.Endpoints[endpoint.ID] = endpoint
	}
	return rows.Err()
}

func (r *Repository) loadDiffs(ctx context.Context, loaded *domainvdoc.State) error {
	var models []DocumentVersionDiff
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		// Persisted diff identity is sourced from DocumentID: model.DocumentID and ServiceID: model.DocumentID.
		documentID := domainID(model.DocumentID)
		diffID := domainID(model.ID)
		summary := domainvdoc.DiffSummary{AddedEndpoints: model.AddedCount, RemovedEndpoints: model.RemovedCount, ModifiedEndpoints: model.ModifiedCount, BreakingChanges: model.BreakingCount}
		if len(model.DiffSummaryJSON) > 0 {
			if err := json.Unmarshal(model.DiffSummaryJSON, &summary); err != nil {
				return fmt.Errorf("decode diff summary %s: %w", diffID, err)
			}
		}
		loaded.Diffs[diffID] = &domainvdoc.Diff{ID: diffID, DocumentID: documentID, ServiceID: documentID, FromVersionID: domainID(model.FromVersionID), ToVersionID: domainID(model.ToVersionID), ObjectKey: stringValue(model.DiffObjectKey), Hash: stringValue(model.DiffHash), DiffStatus: model.DiffStatus, Summary: summary, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	var items []DocumentDiffItem
	if err := r.database.WithContext(ctx).Order("sort_order").Find(&items).Error; err != nil {
		return err
	}
	for _, model := range items {
		diff := loaded.Diffs[domainID(model.DiffID)]
		if diff == nil {
			continue
		}
		diff.Items = append(diff.Items, domainvdoc.DiffItem{ID: domainID(model.ID), ChangeType: model.ChangeType, Severity: model.Severity, Method: codeToOptionalMethod(model.Method), Path: stringValue(model.Path), OperationID: stringValue(model.OperationID), Location: stringValue(model.Location), OldValue: model.OldValue.Interface(), NewValue: model.NewValue.Interface(), Message: model.Message, FrontendImpact: stringValue(model.FrontendImpact), IsBreaking: model.IsBreaking, MustHandle: model.IsBreaking, SortOrder: model.SortOrder})
	}
	return nil
}

func (r *Repository) loadAudits(ctx context.Context, loaded *domainvdoc.State) error {
	var models []AuditLog
	if err := r.database.WithContext(ctx).Order("created_at,id").Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		audit := domainAuditLogFromModel(model)
		loaded.AuditLogs[audit.ID] = audit
	}
	return nil
}

func auditLogModelFromDomain(audit *domainvdoc.AuditLog) *AuditLog {
	if audit == nil {
		return nil
	}
	return &AuditLog{Base: pgdb.Base{ID: audit.ID, CreatedAt: nonZeroTime(audit.CreatedAt), UpdatedAt: nonZeroTime(audit.UpdatedAt)}, ActorType: audit.ActorType, ActorUserID: stringPtr(audit.ActorUserID), ActorTokenID: stringPtr(audit.ActorTokenID), Action: audit.Action, ResourceType: audit.ResourceType, ResourceID: stringPtr(audit.ResourceID), ProjectID: stringPtr(audit.ProjectID), DocumentID: stringPtr(audit.ServiceID), Metadata: pgdb.NewJSONB(audit.Metadata, "{}"), IPAddress: stringPtr(audit.IPAddress), UserAgent: stringPtr(audit.UserAgent), RequestID: stringPtr(audit.RequestID)}
}

func domainAuditLogFromModel(model AuditLog) *domainvdoc.AuditLog {
	return &domainvdoc.AuditLog{ID: domainID(model.ID), ActorType: model.ActorType, ActorUserID: stringValueID(model.ActorUserID), ActorTokenID: stringValueID(model.ActorTokenID), Action: model.Action, ResourceType: model.ResourceType, ResourceID: stringValueID(model.ResourceID), ProjectID: stringValueID(model.ProjectID), ServiceID: stringValueID(model.DocumentID), Metadata: stringMapFromJSONB(model.Metadata), IPAddress: stringValue(model.IPAddress), UserAgent: stringValue(model.UserAgent), RequestID: stringValue(model.RequestID), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
}

func userModelFromDomain(user *domainvdoc.User) *User {
	if user == nil {
		return nil
	}
	return &User{Base: pgdb.Base{ID: user.ID, CreatedAt: nonZeroTime(user.CreatedAt), UpdatedAt: nonZeroTime(user.UpdatedAt)}, Email: user.Email, PasswordHash: user.PasswordHash, DisplayName: user.Name, IsSuperAdmin: user.IsSuperAdmin, Status: user.Status}
}

func teamModelFromDomain(team *domainvdoc.Team) *Team {
	if team == nil {
		return nil
	}
	return &Team{Base: pgdb.Base{ID: team.ID, CreatedAt: nonZeroTime(team.CreatedAt), UpdatedAt: nonZeroTime(team.UpdatedAt)}, Name: team.Name, Slug: team.ID, Description: stringPtr(team.Description), CreatedBy: team.CreatedBy}
}

func projectModelFromDomain(project *domainvdoc.Project) *Project {
	if project == nil {
		return nil
	}
	return &Project{Base: pgdb.Base{ID: project.ID, CreatedAt: nonZeroTime(project.CreatedAt), UpdatedAt: nonZeroTime(project.UpdatedAt)}, TeamID: project.TeamID, Name: project.Name, Slug: project.ID, Description: stringPtr(project.Description), Status: project.Status, CreatedBy: project.CreatedBy}
}

func projectMemberModelFromDomain(member *domainvdoc.ProjectMember) *ProjectMember {
	if member == nil {
		return nil
	}
	return &ProjectMember{Base: pgdb.Base{ID: projectMemberID(member.ProjectID, member.UserID), CreatedAt: nonZeroTime(member.CreatedAt), UpdatedAt: nonZeroTime(member.UpdatedAt)}, ProjectID: member.ProjectID, UserID: member.UserID, Role: member.Role, Status: member.Status, AddedBy: member.AddedBy, AddedAt: nonZeroTime(member.CreatedAt)}
}

func documentModelFromDomain(document *domainvdoc.APIService) *Document {
	if document == nil {
		return nil
	}
	documentType := document.DocumentType
	if documentType == 0 {
		documentType = domainvdoc.DocumentTypeOpenAPI
	}
	return &Document{Base: pgdb.Base{ID: document.ID, CreatedAt: nonZeroTime(document.CreatedAt), UpdatedAt: nonZeroTime(document.UpdatedAt)}, ProjectID: document.ProjectID, Name: document.Name, DocumentType: documentType, RelativePath: documentRelativePath(document), Description: stringPtr(document.Description), Status: document.Status, CreatedBy: document.CreatedBy}
}

func documentBranchModelFromDomain(branch *domainvdoc.ContractBranch) *DocumentBranch {
	if branch == nil {
		return nil
	}
	return &DocumentBranch{Base: pgdb.Base{ID: branch.ID, CreatedAt: nonZeroTime(branch.CreatedAt), UpdatedAt: nonZeroTime(branch.UpdatedAt)}, DocumentID: domainDocumentID(branch.DocumentID, branch.ServiceID), Name: branch.Name, Kind: branch.Kind, Description: stringPtr(branch.Description), IsDefault: branch.IsDefault, IsProtected: branch.IsProtected, Status: branch.Status, CreatedBy: branch.CreatedBy}
}

func documentDraftModelFromDomain(draft *domainvdoc.ContractDraft, document *domainvdoc.APIService) *DocumentDraft {
	if draft == nil {
		return nil
	}
	documentID := domainDocumentID(draft.DocumentID, draft.ServiceID)
	projectID := draft.ProjectID
	if projectID == "" && document != nil {
		projectID = document.ProjectID
	}
	return &DocumentDraft{Base: pgdb.Base{ID: draft.ID, CreatedAt: nonZeroTime(draft.CreatedAt), UpdatedAt: nonZeroTime(draft.UpdatedAt)}, ProjectID: projectID, DocumentID: documentID, BranchID: draft.BranchID, VersionName: draft.VersionName, RelativePath: documentRelativePath(document), Status: draft.Status, DocumentFormat: draft.SchemaFormat, RawSchemaObjectKey: draft.RawSchemaObjectKey, NormalizedSchemaObjectKey: draft.NormalizedObjectKey, RawSchemaHash: draft.RawSchemaHash, NormalizedSchemaHash: draft.NormalizedSchemaHash, SchemaSizeBytes: int64(len(draft.RawSchema)), SchemaMetadata: pgdb.JSONB(`{}`), Changelog: stringPtr(draft.Changelog), SourceGitCommitID: stringPtr(draft.SourceGitCommitID), SourceType: draft.SourceType, SourceBranchID: stringPtr(draft.SourceBranchID), SourceVersionID: stringPtr(draft.SourceVersionID), BaseVersionID: stringPtr(draft.BaseVersionID), DiffPreviewJSON: diffPreviewJSON(draft.DiffPreview), ReviewComment: stringPtr(draft.ReviewComment), CreatedByActorType: domainvdoc.AuditActorUser, CreatedByUserID: draft.CreatedBy, SubmittedAt: draft.SubmittedAt}
}

func documentRelativePath(document *domainvdoc.APIService) string {
	if document == nil {
		return ""
	}
	if document.RelativePath != "" {
		return document.RelativePath
	}
	return document.BasePath
}

func domainDocumentID(documentID, legacyID string) string {
	if documentID != "" {
		return documentID
	}
	return legacyID
}

func mapPostgresError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23505":
		return fmt.Errorf("%w: %s", domainvdoc.ErrAlreadyExists, pgErr.ConstraintName)
	case "23503":
		return fmt.Errorf("%w: %s", domainvdoc.ErrFailedPrecondition, pgErr.ConstraintName)
	case "23514":
		return fmt.Errorf("%w: %s", domainvdoc.ErrInvalidArgument, pgErr.ConstraintName)
	default:
		return err
	}
}

func mapRecordLookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainvdoc.ErrNotFound
	}
	return err
}

func stringPtr(value any) *string {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case *string:
		if v == nil {
			return nil
		}
		text = *v
	default:
		return nil
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &text
}

func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtrID(value *string) *string {
	if value == nil {
		return nil
	}
	id := domainID(*value)
	if id == "" {
		return nil
	}
	return &id
}

func stringValueID(value *string) string {
	if value == nil {
		return ""
	}
	return domainID(*value)
}

func domainID(value string) string {
	if !isCanonicalUUID(value) {
		return value
	}
	return strings.ReplaceAll(strings.ToLower(value), "-", "")
}

func isCanonicalUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		switch index {
		case 8, 13, 18, 23:
			if character != '-' {
				return false
			}
		default:
			if character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F' {
				continue
			}
			return false
		}
	}
	return true
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nonZeroTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now()
	}
	return value
}

func slugFor(name, fallback string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			return character
		}
		if character == '-' || character == '_' {
			return '-'
		}
		return '-'
	}, base)
	base = strings.Trim(base, "-")
	if base == "" {
		return fallback
	}
	return base
}

func sortedValues[T any](items map[string]*T, key func(*T) string) []*T {
	values := make([]*T, 0, len(items))
	for _, value := range items {
		values = append(values, value)
	}
	sort.Slice(values, func(first, second int) bool { return key(values[first]) < key(values[second]) })
	return values
}

func sortedDiffItems(items []domainvdoc.DiffItem) []domainvdoc.DiffItem {
	values := append([]domainvdoc.DiffItem(nil), items...)
	sort.Slice(values, func(first, second int) bool {
		if values[first].SortOrder == values[second].SortOrder {
			return values[first].ID < values[second].ID
		}
		return values[first].SortOrder < values[second].SortOrder
	})
	return values
}

func memberKey(projectID, userID string) string { return projectID + ":" + userID }

func projectMemberID(projectID, userID string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte("project_member:"+projectID+":"+userID)))
}

func endpointCount(endpoints map[string]*domainvdoc.Endpoint, versionID string) int {
	count := 0
	for _, endpoint := range endpoints {
		if endpoint.ContractVersionID == versionID {
			count++
		}
	}
	return count
}

func versionNumberByID(versions map[string]*domainvdoc.ContractVersion) map[string]int {
	groups := map[string][]*domainvdoc.ContractVersion{}
	for _, version := range versions {
		key := domainDocumentID(version.DocumentID, version.ServiceID) + ":" + version.BranchID
		groups[key] = append(groups[key], version)
	}
	out := map[string]int{}
	for _, group := range groups {
		sort.Slice(group, func(first, second int) bool {
			if group[first].PublishedAt.Equal(group[second].PublishedAt) {
				return group[first].ID < group[second].ID
			}
			return group[first].PublishedAt.Before(group[second].PublishedAt)
		})
		for index, version := range group {
			out[version.ID] = index + 1
		}
	}
	return out
}

func diffPreviewJSON(diff *domainvdoc.Diff) pgdb.JSONB {
	if diff == nil {
		return nil
	}
	return pgdb.NewJSONB(diff.Summary, "{}")
}

func diffPreviewFromJSON(raw pgdb.JSONB) *domainvdoc.Diff {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var summary domainvdoc.DiffSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return nil
	}
	return &domainvdoc.Diff{DiffStatus: domainvdoc.DiffStatusSucceeded, Summary: summary}
}

func nullableJSON(value any) pgdb.JSONB {
	if value == nil {
		return nil
	}
	return pgdb.NewJSONB(value, "null")
}

func stringMapFromJSONB(raw pgdb.JSONB) map[string]string {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]string{}
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if text, ok := value.(string); ok {
			out[key] = text
			continue
		}
		out[key] = fmt.Sprint(value)
	}
	return out
}

func jsonToInterface(raw []byte) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func nullStringBytes(value sql.NullString) []byte {
	if !value.Valid {
		return nil
	}
	return []byte(value.String)
}

func stringArrayFromJSON(raw string) []string {
	var values []string
	_ = json.Unmarshal([]byte(raw), &values)
	return values
}

func methodToCode(method string) (int, bool) {
	switch strings.ToUpper(method) {
	case "GET":
		return 1, true
	case "POST":
		return 2, true
	case "PUT":
		return 3, true
	case "PATCH":
		return 4, true
	case "DELETE":
		return 5, true
	case "OPTIONS":
		return 6, true
	case "HEAD":
		return 7, true
	case "TRACE":
		return 8, true
	default:
		return 0, false
	}
}

func optionalMethodToCode(method string) (*int, bool) {
	if strings.TrimSpace(method) == "" {
		return nil, true
	}
	code, ok := methodToCode(method)
	if !ok {
		return nil, false
	}
	return &code, true
}

func codeToMethod(code int) string {
	switch code {
	case 1:
		return "GET"
	case 2:
		return "POST"
	case 3:
		return "PUT"
	case 4:
		return "PATCH"
	case 5:
		return "DELETE"
	case 6:
		return "OPTIONS"
	case 7:
		return "HEAD"
	case 8:
		return "TRACE"
	default:
		return ""
	}
}

func codeToOptionalMethod(code *int) string {
	if code == nil {
		return ""
	}
	return codeToMethod(*code)
}
