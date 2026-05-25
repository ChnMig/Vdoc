package vdoc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func NewRepository(database *gorm.DB) *Repository { return &Repository{database: database} }

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
	if err := r.loadServices(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadBranches(ctx, state); err != nil {
		return nil, err
	}
	if err := r.loadTokens(ctx, state); err != nil {
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
	if err := r.loadAudits(ctx, state); err != nil {
		return nil, err
	}
	return state, nil
}

func (r *Repository) SaveState(ctx context.Context, state *domainvdoc.State) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	if state == nil {
		return nil
	}
	return mapPostgresError(r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		writer := &Repository{database: tx}
		return writer.saveState(ctx, state)
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
	if version.ServiceID != input.ServiceID || version.BranchID != input.BranchID || version.DraftID != input.DraftID || version.VersionName != input.VersionName || draft.Status != domainvdoc.DraftStatusPublished {
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
		if err := writer.insertPublishedEndpoints(ctx, input.State, version.ID); err != nil {
			return err
		}
		if err := writer.insertPublishedDiffs(ctx, input.State, version.ID); err != nil {
			return err
		}
		if err := writer.markDraftPublished(ctx, input, draft.UpdatedAt); err != nil {
			return err
		}
		return writer.insertPublishAudit(ctx, input)
	}))
}

func (r *Repository) saveState(ctx context.Context, state *domainvdoc.State) error {
	for _, user := range sortedValues(state.Users, func(value *domainvdoc.User) string { return value.ID }) {
		if err := r.upsertByID(ctx, &User{Base: pgdb.Base{ID: user.ID, CreatedAt: nonZeroTime(user.CreatedAt), UpdatedAt: nonZeroTime(user.UpdatedAt)}, Email: user.Email, PasswordHash: user.PasswordHash, DisplayName: user.Name, IsSuperAdmin: user.IsSuperAdmin, Status: user.Status}); err != nil {
			return err
		}
	}
	for _, team := range sortedValues(state.Teams, func(value *domainvdoc.Team) string { return value.ID }) {
		if err := r.upsertByID(ctx, &Team{Base: pgdb.Base{ID: team.ID, CreatedAt: nonZeroTime(team.CreatedAt), UpdatedAt: nonZeroTime(team.UpdatedAt)}, Name: team.Name, Slug: slugFor(team.Name, team.ID), Description: stringPtr(team.Description), CreatedBy: team.CreatedBy}); err != nil {
			return err
		}
	}
	if err := r.softDeleteMissingTeams(ctx, state); err != nil {
		return err
	}
	for _, project := range sortedValues(state.Projects, func(value *domainvdoc.Project) string { return value.ID }) {
		if err := r.upsertByID(ctx, &Project{Base: pgdb.Base{ID: project.ID, CreatedAt: nonZeroTime(project.CreatedAt), UpdatedAt: nonZeroTime(project.UpdatedAt)}, TeamID: project.TeamID, Name: project.Name, Slug: slugFor(project.Name, project.ID), Description: stringPtr(project.Description), Status: project.Status, CreatedBy: project.CreatedBy}); err != nil {
			return err
		}
	}
	for _, member := range sortedValues(state.Members, func(value *domainvdoc.ProjectMember) string { return memberKey(value.ProjectID, value.UserID) }) {
		if err := r.saveProjectMember(ctx, member); err != nil {
			return err
		}
	}
	for _, service := range sortedValues(state.Services, func(value *domainvdoc.APIService) string { return value.ID }) {
		if err := r.upsertByID(ctx, &APIService{Base: pgdb.Base{ID: service.ID, CreatedAt: nonZeroTime(service.CreatedAt), UpdatedAt: nonZeroTime(service.UpdatedAt)}, ProjectID: service.ProjectID, Name: service.Name, DisplayName: stringPtr(service.DisplayName), Description: stringPtr(service.Description), BasePath: stringPtr(service.BasePath), Status: service.Status, CreatedBy: service.CreatedBy}); err != nil {
			return err
		}
	}
	for _, branch := range sortedValues(state.Branches, func(value *domainvdoc.ContractBranch) string { return value.ID }) {
		if err := r.upsertByID(ctx, &APIContractBranch{Base: pgdb.Base{ID: branch.ID, CreatedAt: nonZeroTime(branch.CreatedAt), UpdatedAt: nonZeroTime(branch.UpdatedAt)}, ServiceID: branch.ServiceID, Name: branch.Name, Kind: branch.Kind, Description: stringPtr(branch.Description), IsDefault: branch.IsDefault, IsProtected: branch.IsProtected, Status: branch.Status, CreatedBy: branch.CreatedBy}); err != nil {
			return err
		}
	}
	for _, token := range sortedValues(state.Tokens, func(value *domainvdoc.MCPToken) string { return value.ID }) {
		if err := r.upsertByID(ctx, mcpTokenModelFromDomain(token)); err != nil {
			return err
		}
	}
	for _, draft := range sortedValues(state.Drafts, func(value *domainvdoc.ContractDraft) string { return value.ID }) {
		if err := r.upsertByID(ctx, &APIContractDraft{Base: pgdb.Base{ID: draft.ID, CreatedAt: nonZeroTime(draft.CreatedAt), UpdatedAt: nonZeroTime(draft.UpdatedAt)}, ServiceID: draft.ServiceID, BranchID: draft.BranchID, VersionName: draft.VersionName, Status: draft.Status, SchemaFormat: draft.SchemaFormat, RawSchemaObjectKey: draft.RawSchemaObjectKey, NormalizedSchemaObjectKey: draft.NormalizedObjectKey, RawSchemaHash: draft.RawSchemaHash, NormalizedSchemaHash: draft.NormalizedSchemaHash, SchemaSizeBytes: int64(len(draft.RawSchema)), SchemaMetadata: pgdb.JSONB(`{}`), Changelog: stringPtr(draft.Changelog), SourceGitCommitID: stringPtr(draft.SourceGitCommitID), SourceType: draft.SourceType, SourceBranchID: stringPtr(draft.SourceBranchID), SourceVersionID: stringPtr(draft.SourceVersionID), BaseVersionID: stringPtr(draft.BaseVersionID), DiffPreviewJSON: diffPreviewJSON(draft.DiffPreview), CreatedByActorType: 1, CreatedByUserID: draft.CreatedBy, SubmittedAt: draft.SubmittedAt}); err != nil {
			return err
		}
	}
	versionNumbers := versionNumberByID(state.Versions)
	for _, version := range sortedValues(state.Versions, func(value *domainvdoc.ContractVersion) string { return value.ID }) {
		if err := r.insertByIDIgnoreConflict(ctx, &APIContractVersion{Base: pgdb.Base{ID: version.ID, CreatedAt: nonZeroTime(version.CreatedAt), UpdatedAt: nonZeroTime(version.UpdatedAt)}, ServiceID: version.ServiceID, BranchID: version.BranchID, VersionName: version.VersionName, VersionNo: versionNumbers[version.ID], Status: version.Status, SourceDraftID: version.DraftID, SourceType: version.SourceType, SourceBranchID: stringPtr(version.SourceBranchID), SourceVersionID: stringPtr(version.SourceVersionID), BaseVersionID: stringPtr(version.BaseVersionID), SchemaFormat: version.SchemaFormat, RawSchemaObjectKey: version.RawSchemaObjectKey, NormalizedSchemaObjectKey: version.NormalizedObjectKey, RawSchemaHash: version.RawSchemaHash, NormalizedSchemaHash: version.NormalizedSchemaHash, SchemaSizeBytes: int64(len(version.RawSchema)), SchemaMetadata: pgdb.JSONB(`{}`), Changelog: stringPtr(version.Changelog), SourceGitCommitID: stringPtr(version.SourceGitCommitID), EndpointCount: endpointCount(state.Endpoints, version.ID), PublishedBy: version.PublishedBy, PublishedAt: nonZeroTime(version.PublishedAt)}); err != nil {
			return err
		}
	}
	for _, endpoint := range sortedValues(state.Endpoints, func(value *domainvdoc.Endpoint) string { return value.ID }) {
		version := state.Versions[endpoint.ContractVersionID]
		if version == nil {
			continue
		}
		method, ok := methodToCode(endpoint.Method)
		if !ok {
			return fmt.Errorf("%w: unsupported endpoint method %q", domainvdoc.ErrInvalidArgument, endpoint.Method)
		}
		if err := r.insertByIDIgnoreConflict(ctx, &APIEndpoint{Base: pgdb.Base{ID: endpoint.ID, CreatedAt: nonZeroTime(endpoint.CreatedAt), UpdatedAt: nonZeroTime(endpoint.UpdatedAt)}, ContractVersionID: endpoint.ContractVersionID, ServiceID: version.ServiceID, BranchID: version.BranchID, Method: method, Path: endpoint.Path, OperationID: stringPtr(endpoint.OperationID), Summary: stringPtr(endpoint.Summary), Tags: pgdb.StringArray(endpoint.Tags), Deprecated: endpoint.Deprecated, RequestHash: endpoint.Hash, ResponseHash: endpoint.Hash, EndpointHash: endpoint.Hash}); err != nil {
			return err
		}
		if err := r.saveEndpointDetail(ctx, endpoint); err != nil {
			return err
		}
	}
	for _, diff := range sortedValues(state.Diffs, func(value *domainvdoc.Diff) string { return value.ID }) {
		fromVersion := state.Versions[diff.FromVersionID]
		toVersion := state.Versions[diff.ToVersionID]
		if fromVersion == nil || toVersion == nil || diff.FromVersionID == diff.ToVersionID {
			continue
		}
		if err := r.insertByIDIgnoreConflict(ctx, &APIVersionDiff{Base: pgdb.Base{ID: diff.ID, CreatedAt: nonZeroTime(diff.CreatedAt), UpdatedAt: nonZeroTime(diff.UpdatedAt)}, ServiceID: diff.ServiceID, FromBranchID: fromVersion.BranchID, ToBranchID: toVersion.BranchID, FromVersionID: diff.FromVersionID, ToVersionID: diff.ToVersionID, DiffStatus: diff.DiffStatus, DiffObjectKey: stringPtr(diff.ObjectKey), DiffHash: stringPtr(diff.Hash), DiffSummaryJSON: pgdb.NewJSONB(diff.Summary, "{}"), BreakingChangesJSON: pgdb.JSONB(`{}`), AddedCount: diff.Summary.AddedEndpoints, ModifiedCount: diff.Summary.ModifiedEndpoints, RemovedCount: diff.Summary.RemovedEndpoints, BreakingCount: diff.Summary.BreakingChanges}); err != nil {
			return err
		}
		for _, item := range diff.Items {
			method, ok := optionalMethodToCode(item.Method)
			if !ok {
				return fmt.Errorf("%w: unsupported diff method %q", domainvdoc.ErrInvalidArgument, item.Method)
			}
			if err := r.insertByIDIgnoreConflict(ctx, &APIDiffItem{Base: pgdb.Base{ID: item.ID, CreatedAt: nonZeroTime(diff.CreatedAt), UpdatedAt: nonZeroTime(diff.UpdatedAt)}, DiffID: diff.ID, ChangeType: item.ChangeType, Severity: item.Severity, Method: method, Path: stringPtr(item.Path), OperationID: stringPtr(item.OperationID), Location: stringPtr(item.Location), OldValue: pgdb.NewJSONB(item.OldValue, "null"), NewValue: pgdb.NewJSONB(item.NewValue, "null"), Message: item.Message, FrontendImpact: stringPtr(item.FrontendImpact), IsBreaking: item.IsBreaking, SortOrder: item.SortOrder}); err != nil {
				return err
			}
		}
	}
	if err := r.insertStateAudits(ctx, state); err != nil {
		return err
	}
	return nil
}

func (r *Repository) upsertByID(ctx context.Context, value any) error {
	return mapPostgresError(r.database.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(value).Error)
}

func (r *Repository) insertByIDIgnoreConflict(ctx context.Context, value any) error {
	return mapPostgresError(r.database.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(value).Error)
}

func (r *Repository) saveProjectMember(ctx context.Context, member *domainvdoc.ProjectMember) error {
	return mapPostgresError(r.database.WithContext(ctx).Exec(`
INSERT INTO project_members(project_id,user_id,role,status,added_by,created_at,updated_at)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT (project_id, user_id) WHERE deleted_at IS NULL DO UPDATE SET role=EXCLUDED.role, status=EXCLUDED.status, added_by=EXCLUDED.added_by, updated_at=EXCLUDED.updated_at`, member.ProjectID, member.UserID, member.Role, member.Status, member.AddedBy, nonZeroTime(member.CreatedAt), nonZeroTime(member.UpdatedAt)).Error)
}

func (r *Repository) softDeleteMissingTeams(ctx context.Context, state *domainvdoc.State) error {
	ids := make([]string, 0, len(state.Teams))
	for id := range state.Teams {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return mapPostgresError(r.database.WithContext(ctx).Exec("UPDATE teams SET deleted_at=now(), updated_at=now() WHERE deleted_at IS NULL").Error)
	}
	return mapPostgresError(r.database.WithContext(ctx).Exec("UPDATE teams SET deleted_at=now(), updated_at=now() WHERE deleted_at IS NULL AND id NOT IN ?", ids).Error)
}

func (r *Repository) saveEndpointDetail(ctx context.Context, endpoint *domainvdoc.Endpoint) error {
	return mapPostgresError(r.database.WithContext(ctx).Exec(`
	INSERT INTO api_endpoint_details(endpoint_id,parameters_json,request_body_json,responses_json,security_json,servers_json,normalized_operation_json,schema_refs_json)
	VALUES(?,?,?,?,?,?,?,?)
	ON CONFLICT (endpoint_id) DO NOTHING`, endpoint.ID, pgdb.NewJSONB(endpoint.Parameters, "[]"), nullableJSON(endpoint.RequestBody), pgdb.NewJSONB(endpoint.Responses, "{}"), nullableJSON(endpoint.Security), nullableJSON(endpoint.Servers), pgdb.NewJSONB(endpoint.NormalizedOperation, "{}"), nullableJSON(endpoint.SchemaRefs)).Error)
}

func (r *Repository) lockPublishScope(ctx context.Context, input domainvdoc.PublishStateInput) (*APIContractDraft, error) {
	var service APIService
	if err := r.database.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND project_id = ?", input.ServiceID, input.ProjectID).First(&service).Error; err != nil {
		return nil, mapRecordLookupError(err)
	}
	var branch APIContractBranch
	if err := r.database.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND service_id = ?", input.BranchID, input.ServiceID).First(&branch).Error; err != nil {
		return nil, mapRecordLookupError(err)
	}
	var draft APIContractDraft
	if err := r.database.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND service_id = ? AND branch_id = ?", input.DraftID, input.ServiceID, input.BranchID).First(&draft).Error; err != nil {
		return nil, mapRecordLookupError(err)
	}
	return &draft, nil
}

func (r *Repository) ensureVersionAvailable(ctx context.Context, serviceID, branchID, versionName string) error {
	var count int64
	if err := r.database.WithContext(ctx).Model(&APIContractVersion{}).Where("service_id = ? AND branch_id = ? AND version_name = ?", serviceID, branchID, versionName).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return domainvdoc.ErrAlreadyExists
	}
	return nil
}

func (r *Repository) nextVersionNo(ctx context.Context, serviceID, branchID string) (int, error) {
	var count int64
	if err := r.database.WithContext(ctx).Model(&APIContractVersion{}).Where("service_id = ? AND branch_id = ?", serviceID, branchID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count) + 1, nil
}

func (r *Repository) insertPublishedVersion(ctx context.Context, version *domainvdoc.ContractVersion, versionNo, endpoints int) error {
	return r.database.WithContext(ctx).Create(&APIContractVersion{Base: pgdb.Base{ID: version.ID, CreatedAt: nonZeroTime(version.CreatedAt), UpdatedAt: nonZeroTime(version.UpdatedAt)}, ServiceID: version.ServiceID, BranchID: version.BranchID, VersionName: version.VersionName, VersionNo: versionNo, Status: version.Status, SourceDraftID: version.DraftID, SourceType: version.SourceType, SourceBranchID: stringPtr(version.SourceBranchID), SourceVersionID: stringPtr(version.SourceVersionID), BaseVersionID: stringPtr(version.BaseVersionID), SchemaFormat: version.SchemaFormat, RawSchemaObjectKey: version.RawSchemaObjectKey, NormalizedSchemaObjectKey: version.NormalizedObjectKey, RawSchemaHash: version.RawSchemaHash, NormalizedSchemaHash: version.NormalizedSchemaHash, SchemaSizeBytes: int64(len(version.RawSchema)), SchemaMetadata: pgdb.JSONB(`{}`), Changelog: stringPtr(version.Changelog), SourceGitCommitID: stringPtr(version.SourceGitCommitID), EndpointCount: endpoints, PublishedBy: version.PublishedBy, PublishedAt: nonZeroTime(version.PublishedAt)}).Error
}

func (r *Repository) insertPublishedEndpoints(ctx context.Context, state *domainvdoc.State, versionID string) error {
	for _, endpoint := range sortedValues(state.Endpoints, func(value *domainvdoc.Endpoint) string { return value.ID }) {
		if endpoint.ContractVersionID != versionID {
			continue
		}
		version := state.Versions[endpoint.ContractVersionID]
		if version == nil {
			return fmt.Errorf("%w: endpoint version missing", domainvdoc.ErrFailedPrecondition)
		}
		method, ok := methodToCode(endpoint.Method)
		if !ok {
			return fmt.Errorf("%w: unsupported endpoint method %q", domainvdoc.ErrInvalidArgument, endpoint.Method)
		}
		if err := r.database.WithContext(ctx).Create(&APIEndpoint{Base: pgdb.Base{ID: endpoint.ID, CreatedAt: nonZeroTime(endpoint.CreatedAt), UpdatedAt: nonZeroTime(endpoint.UpdatedAt)}, ContractVersionID: endpoint.ContractVersionID, ServiceID: version.ServiceID, BranchID: version.BranchID, Method: method, Path: endpoint.Path, OperationID: stringPtr(endpoint.OperationID), Summary: stringPtr(endpoint.Summary), Tags: pgdb.StringArray(endpoint.Tags), Deprecated: endpoint.Deprecated, RequestHash: endpoint.Hash, ResponseHash: endpoint.Hash, EndpointHash: endpoint.Hash}).Error; err != nil {
			return err
		}
		if err := r.database.WithContext(ctx).Create(&APIEndpointDetail{EndpointID: endpoint.ID, ParametersJSON: pgdb.NewJSONB(endpoint.Parameters, "[]"), RequestBodyJSON: nullableJSON(endpoint.RequestBody), ResponsesJSON: pgdb.NewJSONB(endpoint.Responses, "{}"), SecurityJSON: nullableJSON(endpoint.Security), ServersJSON: nullableJSON(endpoint.Servers), NormalizedOperationJSON: pgdb.NewJSONB(endpoint.NormalizedOperation, "{}"), SchemaRefsJSON: nullableJSON(endpoint.SchemaRefs)}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) insertPublishedDiffs(ctx context.Context, state *domainvdoc.State, versionID string) error {
	for _, diff := range sortedValues(state.Diffs, func(value *domainvdoc.Diff) string { return value.ID }) {
		if diff.ToVersionID != versionID {
			continue
		}
		fromVersion := state.Versions[diff.FromVersionID]
		toVersion := state.Versions[diff.ToVersionID]
		if fromVersion == nil || toVersion == nil || diff.FromVersionID == diff.ToVersionID {
			return fmt.Errorf("%w: diff versions missing", domainvdoc.ErrFailedPrecondition)
		}
		generatedAt := nonZeroTime(diff.CreatedAt)
		if err := r.database.WithContext(ctx).Create(&APIVersionDiff{Base: pgdb.Base{ID: diff.ID, CreatedAt: nonZeroTime(diff.CreatedAt), UpdatedAt: nonZeroTime(diff.UpdatedAt)}, ServiceID: diff.ServiceID, FromBranchID: fromVersion.BranchID, ToBranchID: toVersion.BranchID, FromVersionID: diff.FromVersionID, ToVersionID: diff.ToVersionID, DiffStatus: diff.DiffStatus, DiffObjectKey: stringPtr(diff.ObjectKey), DiffHash: stringPtr(diff.Hash), DiffSummaryJSON: pgdb.NewJSONB(diff.Summary, "{}"), BreakingChangesJSON: pgdb.JSONB(`{}`), AddedCount: diff.Summary.AddedEndpoints, ModifiedCount: diff.Summary.ModifiedEndpoints, RemovedCount: diff.Summary.RemovedEndpoints, BreakingCount: diff.Summary.BreakingChanges, GeneratedAt: &generatedAt}).Error; err != nil {
			return err
		}
		for _, item := range sortedDiffItems(diff.Items) {
			method, ok := optionalMethodToCode(item.Method)
			if !ok {
				return fmt.Errorf("%w: unsupported diff method %q", domainvdoc.ErrInvalidArgument, item.Method)
			}
			if err := r.database.WithContext(ctx).Create(&APIDiffItem{Base: pgdb.Base{ID: item.ID, CreatedAt: nonZeroTime(diff.CreatedAt), UpdatedAt: nonZeroTime(diff.UpdatedAt)}, DiffID: diff.ID, ChangeType: item.ChangeType, Severity: item.Severity, Method: method, Path: stringPtr(item.Path), OperationID: stringPtr(item.OperationID), Location: stringPtr(item.Location), OldValue: pgdb.NewJSONB(item.OldValue, "null"), NewValue: pgdb.NewJSONB(item.NewValue, "null"), Message: item.Message, FrontendImpact: stringPtr(item.FrontendImpact), IsBreaking: item.IsBreaking, SortOrder: item.SortOrder}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repository) markDraftPublished(ctx context.Context, input domainvdoc.PublishStateInput, updatedAt time.Time) error {
	result := r.database.WithContext(ctx).Model(&APIContractDraft{}).Where("id = ? AND status = ?", input.DraftID, domainvdoc.DraftStatusSubmitted).Updates(map[string]any{"status": domainvdoc.DraftStatusPublished, "reviewed_by": input.ActorID, "reviewed_at": nonZeroTime(updatedAt), "published_version_id": input.VersionID, "updated_at": nonZeroTime(updatedAt)})
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
			matchesReview := audit.Action == "contract_draft.review" && audit.ResourceID == input.DraftID
			matchesPublish := audit.Action == "api_contract_version.publish" && audit.ResourceID == input.VersionID
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
	return r.RecordAudit(ctx, &domainvdoc.AuditLog{ActorType: domainvdoc.AuditActorUser, ActorUserID: input.ActorID, Action: "api_contract_version.publish", ResourceType: "api_contract_version", ResourceID: input.VersionID, ProjectID: input.ProjectID, ServiceID: input.ServiceID, Metadata: metadata})
}

func (r *Repository) loadUsers(ctx context.Context, state *domainvdoc.State) error {
	var models []User
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		state.Users[model.ID] = &domainvdoc.User{ID: model.ID, Email: model.Email, Name: model.DisplayName, PasswordHash: model.PasswordHash, IsSuperAdmin: model.IsSuperAdmin, Status: model.Status, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadTeams(ctx context.Context, state *domainvdoc.State) error {
	var models []Team
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		state.Teams[model.ID] = &domainvdoc.Team{ID: model.ID, Name: model.Name, Description: stringValue(model.Description), CreatedBy: model.CreatedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadProjects(ctx context.Context, state *domainvdoc.State) error {
	var models []Project
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		state.Projects[model.ID] = &domainvdoc.Project{ID: model.ID, TeamID: model.TeamID, Name: model.Name, Description: stringValue(model.Description), Status: model.Status, CreatedBy: model.CreatedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadProjectMembers(ctx context.Context, state *domainvdoc.State) error {
	var models []ProjectMember
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		member := &domainvdoc.ProjectMember{ProjectID: model.ProjectID, UserID: model.UserID, Role: model.Role, Status: model.Status, AddedBy: model.AddedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
		state.Members[memberKey(model.ProjectID, model.UserID)] = member
	}
	return nil
}

func (r *Repository) loadServices(ctx context.Context, state *domainvdoc.State) error {
	var models []APIService
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		state.Services[model.ID] = &domainvdoc.APIService{ID: model.ID, ProjectID: model.ProjectID, Name: model.Name, DisplayName: stringValue(model.DisplayName), Description: stringValue(model.Description), BasePath: stringValue(model.BasePath), Status: model.Status, CreatedBy: model.CreatedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadBranches(ctx context.Context, state *domainvdoc.State) error {
	var models []APIContractBranch
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		state.Branches[model.ID] = &domainvdoc.ContractBranch{ID: model.ID, ServiceID: model.ServiceID, Name: model.Name, Kind: model.Kind, Description: stringValue(model.Description), IsDefault: model.IsDefault, IsProtected: model.IsProtected, Status: model.Status, CreatedBy: model.CreatedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadTokens(ctx context.Context, state *domainvdoc.State) error {
	var models []MCPToken
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		state.Tokens[model.ID] = domainMCPTokenFromModel(model)
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
	return &domainvdoc.MCPToken{ID: model.ID, UserID: model.UserID, Name: model.Name, TokenHash: model.TokenHash, TokenCiphertext: append([]byte(nil), model.TokenCiphertext...), CipherKID: model.CipherKID, Scopes: []int(model.Scopes), Status: model.Status, ExpiresAt: model.ExpiresAt, RevokedAt: model.RevokedAt, RevokedBy: stringPtr(model.RevokedBy), LastUsedAt: model.LastUsedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
}

func (r *Repository) loadDrafts(ctx context.Context, state *domainvdoc.State) error {
	var models []APIContractDraft
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		draft := &domainvdoc.ContractDraft{ID: model.ID, ServiceID: model.ServiceID, BranchID: model.BranchID, VersionName: model.VersionName, Changelog: stringValue(model.Changelog), SourceGitCommitID: stringValue(model.SourceGitCommitID), SchemaFormat: model.SchemaFormat, SourceType: model.SourceType, SourceBranchID: stringValue(model.SourceBranchID), SourceVersionID: stringValue(model.SourceVersionID), BaseVersionID: stringValue(model.BaseVersionID), RawSchemaObjectKey: model.RawSchemaObjectKey, NormalizedObjectKey: model.NormalizedSchemaObjectKey, RawSchemaHash: model.RawSchemaHash, NormalizedSchemaHash: model.NormalizedSchemaHash, Status: model.Status, DiffPreview: diffPreviewFromJSON(model.DiffPreviewJSON), CreatedBy: model.CreatedByUserID, SubmittedAt: model.SubmittedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
		if draft.DiffPreview != nil {
			draft.DiffPreview.ObjectKey = stringValue(model.DiffPreviewObjectKey)
		}
		if service := state.Services[draft.ServiceID]; service != nil {
			draft.ProjectID = service.ProjectID
		}
		state.Drafts[draft.ID] = draft
	}
	return nil
}

func (r *Repository) loadVersions(ctx context.Context, state *domainvdoc.State) error {
	var models []APIContractVersion
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		version := &domainvdoc.ContractVersion{ID: model.ID, ServiceID: model.ServiceID, BranchID: model.BranchID, DraftID: model.SourceDraftID, VersionName: model.VersionName, Changelog: stringValue(model.Changelog), SourceGitCommitID: stringValue(model.SourceGitCommitID), SchemaFormat: model.SchemaFormat, SourceType: model.SourceType, SourceBranchID: stringValue(model.SourceBranchID), SourceVersionID: stringValue(model.SourceVersionID), BaseVersionID: stringValue(model.BaseVersionID), RawSchemaObjectKey: model.RawSchemaObjectKey, NormalizedObjectKey: model.NormalizedSchemaObjectKey, RawSchemaHash: model.RawSchemaHash, NormalizedSchemaHash: model.NormalizedSchemaHash, Status: model.Status, PublishedBy: model.PublishedBy, PublishedAt: model.PublishedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
		if service := state.Services[version.ServiceID]; service != nil {
			version.ProjectID = service.ProjectID
		}
		state.Versions[version.ID] = version
	}
	return nil
}

func (r *Repository) loadEndpoints(ctx context.Context, state *domainvdoc.State) error {
	rows, err := r.database.WithContext(ctx).Raw(`SELECT e.id::text,e.contract_version_id::text,e.method,e.path,COALESCE(e.operation_id,''),COALESCE(e.summary,''),array_to_json(e.tags)::text,e.deprecated,e.endpoint_hash,e.created_at,e.updated_at,d.parameters_json,d.request_body_json,d.responses_json,d.security_json,d.servers_json,d.normalized_operation_json,d.schema_refs_json FROM api_endpoints e LEFT JOIN api_endpoint_details d ON d.endpoint_id=e.id`).Rows()
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
		endpoint.Method = codeToMethod(method)
		endpoint.Tags = stringArrayFromJSON(tags)
		endpoint.Parameters = jsonToInterface(nullStringBytes(parameters))
		endpoint.RequestBody = jsonToInterface(nullStringBytes(requestBody))
		endpoint.Responses = jsonToInterface(nullStringBytes(responses))
		endpoint.Security = jsonToInterface(nullStringBytes(security))
		endpoint.Servers = jsonToInterface(nullStringBytes(servers))
		endpoint.NormalizedOperation = jsonToInterface(nullStringBytes(normalizedOperation))
		endpoint.SchemaRefs = jsonToInterface(nullStringBytes(schemaRefs))
		state.Endpoints[endpoint.ID] = endpoint
	}
	return rows.Err()
}

func (r *Repository) loadDiffs(ctx context.Context, state *domainvdoc.State) error {
	var models []APIVersionDiff
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		state.Diffs[model.ID] = &domainvdoc.Diff{ID: model.ID, ServiceID: model.ServiceID, FromVersionID: model.FromVersionID, ToVersionID: model.ToVersionID, ObjectKey: stringValue(model.DiffObjectKey), Hash: stringValue(model.DiffHash), DiffStatus: model.DiffStatus, Summary: domainvdoc.DiffSummary{AddedEndpoints: model.AddedCount, RemovedEndpoints: model.RemovedCount, ModifiedEndpoints: model.ModifiedCount, BreakingChanges: model.BreakingCount}, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	var items []APIDiffItem
	if err := r.database.WithContext(ctx).Order("sort_order").Find(&items).Error; err != nil {
		return err
	}
	for _, model := range items {
		diff := state.Diffs[model.DiffID]
		if diff == nil {
			continue
		}
		diff.Items = append(diff.Items, domainvdoc.DiffItem{ID: model.ID, ChangeType: model.ChangeType, Severity: model.Severity, Method: codeToOptionalMethod(model.Method), Path: stringValue(model.Path), OperationID: stringValue(model.OperationID), Location: stringValue(model.Location), OldValue: model.OldValue.Interface(), NewValue: model.NewValue.Interface(), Message: model.Message, FrontendImpact: stringValue(model.FrontendImpact), IsBreaking: model.IsBreaking, MustHandle: model.IsBreaking, SortOrder: model.SortOrder})
	}
	return nil
}

func (r *Repository) loadAudits(ctx context.Context, state *domainvdoc.State) error {
	var models []AuditLog
	if err := r.database.WithContext(ctx).Order("created_at,id").Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		state.AuditLogs[model.ID] = domainAuditLogFromModel(model)
	}
	return nil
}

func (r *Repository) insertStateAudits(ctx context.Context, state *domainvdoc.State) error {
	for _, audit := range sortedValues(state.AuditLogs, func(value *domainvdoc.AuditLog) string {
		return value.CreatedAt.Format(time.RFC3339Nano) + ":" + value.ID
	}) {
		if err := r.RecordAudit(ctx, audit); err != nil {
			return err
		}
	}
	return nil
}

func auditLogModelFromDomain(audit *domainvdoc.AuditLog) *AuditLog {
	if audit == nil {
		return nil
	}
	return &AuditLog{Base: pgdb.Base{ID: audit.ID, CreatedAt: nonZeroTime(audit.CreatedAt), UpdatedAt: nonZeroTime(audit.UpdatedAt)}, ActorType: audit.ActorType, ActorUserID: stringPtr(audit.ActorUserID), ActorTokenID: stringPtr(audit.ActorTokenID), Action: audit.Action, ResourceType: audit.ResourceType, ResourceID: stringPtr(audit.ResourceID), ProjectID: stringPtr(audit.ProjectID), ServiceID: stringPtr(audit.ServiceID), Metadata: pgdb.NewJSONB(audit.Metadata, "{}"), IPAddress: stringPtr(audit.IPAddress), UserAgent: stringPtr(audit.UserAgent), RequestID: stringPtr(audit.RequestID)}
}

func domainAuditLogFromModel(model AuditLog) *domainvdoc.AuditLog {
	return &domainvdoc.AuditLog{ID: model.ID, ActorType: model.ActorType, ActorUserID: stringValue(model.ActorUserID), ActorTokenID: stringValue(model.ActorTokenID), Action: model.Action, ResourceType: model.ResourceType, ResourceID: stringValue(model.ResourceID), ProjectID: stringValue(model.ProjectID), ServiceID: stringValue(model.ServiceID), Metadata: stringMapFromJSONB(model.Metadata), IPAddress: stringValue(model.IPAddress), UserAgent: stringValue(model.UserAgent), RequestID: stringValue(model.RequestID), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
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

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
		key := version.ServiceID + ":" + version.BranchID
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
