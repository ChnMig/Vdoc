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
	if err := r.loadDocuments(ctx, state); err != nil {
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
		document := input.State.APIServices[domainDocumentID(version.DocumentID, version.ServiceID)]
		if err := writer.insertPublishedVersion(ctx, version, document, versionNo, endpointCount(input.State.Endpoints, version.ID)); err != nil {
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

func (r *Repository) insertPublishedVersion(ctx context.Context, version *domainvdoc.ContractVersion, document *domainvdoc.APIService, versionNo, endpoints int) error {
	return r.database.WithContext(ctx).Create(documentVersionModelFromDomain(version, document, versionNo, endpoints)).Error
}

func documentVersionModelFromDomain(version *domainvdoc.ContractVersion, document *domainvdoc.APIService, versionNo, endpoints int) *DocumentVersion {
	if version == nil {
		return nil
	}
	projectID := version.ProjectID
	if projectID == "" && document != nil {
		projectID = document.ProjectID
	}
	return &DocumentVersion{Base: pgdb.Base{ID: version.ID, CreatedAt: nonZeroTime(version.CreatedAt), UpdatedAt: nonZeroTime(version.UpdatedAt)}, ProjectID: projectID, DocumentID: domainDocumentID(version.DocumentID, version.ServiceID), BranchID: version.BranchID, VersionName: version.VersionName, VersionNo: versionNo, RelativePath: documentRelativePath(document), Status: version.Status, SourceDraftID: version.DraftID, SourceType: version.SourceType, SourceBranchID: stringPtr(version.SourceBranchID), SourceVersionID: stringPtr(version.SourceVersionID), BaseVersionID: stringPtr(version.BaseVersionID), DocumentFormat: version.SchemaFormat, RawSchemaObjectKey: version.RawSchemaObjectKey, NormalizedSchemaObjectKey: version.NormalizedObjectKey, RawSchemaHash: version.RawSchemaHash, NormalizedSchemaHash: version.NormalizedSchemaHash, SchemaSizeBytes: int64(len(version.RawSchema)), SchemaMetadata: pgdb.JSONB(`{}`), Changelog: stringPtr(version.Changelog), SourceGitCommitID: stringPtr(version.SourceGitCommitID), EndpointCount: endpoints, PublishedBy: version.PublishedBy, PublishedAt: nonZeroTime(version.PublishedAt)}
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

func (r *Repository) markDraftPublished(ctx context.Context, input domainvdoc.PublishStateInput, updatedAt time.Time) error {
	result := r.database.WithContext(ctx).Model(&DocumentDraft{}).Where("id = ? AND status = ?", input.DraftID, domainvdoc.DraftStatusSubmitted).Updates(map[string]any{"status": domainvdoc.DraftStatusPublished, "reviewed_by": input.ActorID, "reviewed_at": nonZeroTime(updatedAt), "published_version_id": input.VersionID, "updated_at": nonZeroTime(updatedAt)})
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
		loaded.Users[model.ID] = &domainvdoc.User{ID: model.ID, Email: model.Email, Name: model.DisplayName, PasswordHash: model.PasswordHash, IsSuperAdmin: model.IsSuperAdmin, Status: model.Status, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadTeams(ctx context.Context, loaded *domainvdoc.State) error {
	var models []Team
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		loaded.Teams[model.ID] = &domainvdoc.Team{ID: model.ID, Name: model.Name, Description: stringValue(model.Description), CreatedBy: model.CreatedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadProjects(ctx context.Context, loaded *domainvdoc.State) error {
	var models []Project
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		loaded.Projects[model.ID] = &domainvdoc.Project{ID: model.ID, TeamID: model.TeamID, Name: model.Name, Description: stringValue(model.Description), Status: model.Status, CreatedBy: model.CreatedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadProjectMembers(ctx context.Context, loaded *domainvdoc.State) error {
	var models []ProjectMember
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		member := &domainvdoc.ProjectMember{ProjectID: model.ProjectID, UserID: model.UserID, Role: model.Role, Status: model.Status, AddedBy: model.AddedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
		loaded.Members[memberKey(model.ProjectID, model.UserID)] = member
	}
	return nil
}

func (r *Repository) loadDocuments(ctx context.Context, loaded *domainvdoc.State) error {
	var models []Document
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		loaded.APIServices[model.ID] = &domainvdoc.APIService{ID: model.ID, ProjectID: model.ProjectID, Name: model.Name, DocumentType: model.DocumentType, RelativePath: model.RelativePath, Description: stringValue(model.Description), BasePath: model.RelativePath, Status: model.Status, CreatedBy: model.CreatedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadBranches(ctx context.Context, loaded *domainvdoc.State) error {
	var models []DocumentBranch
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		loaded.Branches[model.ID] = &domainvdoc.ContractBranch{ID: model.ID, DocumentID: model.DocumentID, ServiceID: model.DocumentID, Name: model.Name, Kind: model.Kind, Description: stringValue(model.Description), IsDefault: model.IsDefault, IsProtected: model.IsProtected, Status: model.Status, CreatedBy: model.CreatedBy, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	return nil
}

func (r *Repository) loadTokens(ctx context.Context, loaded *domainvdoc.State) error {
	var models []MCPToken
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		loaded.Tokens[model.ID] = domainMCPTokenFromModel(model)
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

func (r *Repository) loadDrafts(ctx context.Context, loaded *domainvdoc.State) error {
	var models []DocumentDraft
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		draft := &domainvdoc.ContractDraft{ID: model.ID, DocumentID: model.DocumentID, ServiceID: model.DocumentID, BranchID: model.BranchID, VersionName: model.VersionName, Changelog: stringValue(model.Changelog), SourceGitCommitID: stringValue(model.SourceGitCommitID), SchemaFormat: model.DocumentFormat, SourceType: model.SourceType, SourceBranchID: stringValue(model.SourceBranchID), SourceVersionID: stringValue(model.SourceVersionID), BaseVersionID: stringValue(model.BaseVersionID), RawSchemaObjectKey: model.RawSchemaObjectKey, NormalizedObjectKey: model.NormalizedSchemaObjectKey, RawSchemaHash: model.RawSchemaHash, NormalizedSchemaHash: model.NormalizedSchemaHash, Status: model.Status, DiffPreview: diffPreviewFromJSON(model.DiffPreviewJSON), CreatedBy: model.CreatedByUserID, SubmittedAt: model.SubmittedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
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
		version := &domainvdoc.ContractVersion{ID: model.ID, DocumentID: model.DocumentID, ServiceID: model.DocumentID, BranchID: model.BranchID, DraftID: model.SourceDraftID, VersionName: model.VersionName, Changelog: stringValue(model.Changelog), SourceGitCommitID: stringValue(model.SourceGitCommitID), SchemaFormat: model.DocumentFormat, SourceType: model.SourceType, SourceBranchID: stringValue(model.SourceBranchID), SourceVersionID: stringValue(model.SourceVersionID), BaseVersionID: stringValue(model.BaseVersionID), RawSchemaObjectKey: model.RawSchemaObjectKey, NormalizedObjectKey: model.NormalizedSchemaObjectKey, RawSchemaHash: model.RawSchemaHash, NormalizedSchemaHash: model.NormalizedSchemaHash, Status: model.Status, PublishedBy: model.PublishedBy, PublishedAt: model.PublishedAt, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
		if service := loaded.APIServices[version.ServiceID]; service != nil {
			version.ProjectID = service.ProjectID
		}
		loaded.Versions[version.ID] = version
	}
	return nil
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
		loaded.Diffs[model.ID] = &domainvdoc.Diff{ID: model.ID, DocumentID: model.DocumentID, ServiceID: model.DocumentID, FromVersionID: model.FromVersionID, ToVersionID: model.ToVersionID, ObjectKey: stringValue(model.DiffObjectKey), Hash: stringValue(model.DiffHash), DiffStatus: model.DiffStatus, Summary: domainvdoc.DiffSummary{AddedEndpoints: model.AddedCount, RemovedEndpoints: model.RemovedCount, ModifiedEndpoints: model.ModifiedCount, BreakingChanges: model.BreakingCount}, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
	}
	var items []DocumentDiffItem
	if err := r.database.WithContext(ctx).Order("sort_order").Find(&items).Error; err != nil {
		return err
	}
	for _, model := range items {
		diff := loaded.Diffs[model.DiffID]
		if diff == nil {
			continue
		}
		diff.Items = append(diff.Items, domainvdoc.DiffItem{ID: model.ID, ChangeType: model.ChangeType, Severity: model.Severity, Method: codeToOptionalMethod(model.Method), Path: stringValue(model.Path), OperationID: stringValue(model.OperationID), Location: stringValue(model.Location), OldValue: model.OldValue.Interface(), NewValue: model.NewValue.Interface(), Message: model.Message, FrontendImpact: stringValue(model.FrontendImpact), IsBreaking: model.IsBreaking, MustHandle: model.IsBreaking, SortOrder: model.SortOrder})
	}
	return nil
}

func (r *Repository) loadAudits(ctx context.Context, loaded *domainvdoc.State) error {
	var models []AuditLog
	if err := r.database.WithContext(ctx).Order("created_at,id").Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		loaded.AuditLogs[model.ID] = domainAuditLogFromModel(model)
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
	return &domainvdoc.AuditLog{ID: model.ID, ActorType: model.ActorType, ActorUserID: stringValue(model.ActorUserID), ActorTokenID: stringValue(model.ActorTokenID), Action: model.Action, ResourceType: model.ResourceType, ResourceID: stringValue(model.ResourceID), ProjectID: stringValue(model.ProjectID), ServiceID: stringValue(model.DocumentID), Metadata: stringMapFromJSONB(model.Metadata), IPAddress: stringValue(model.IPAddress), UserAgent: stringValue(model.UserAgent), RequestID: stringValue(model.RequestID), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
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
	return &DocumentDraft{Base: pgdb.Base{ID: draft.ID, CreatedAt: nonZeroTime(draft.CreatedAt), UpdatedAt: nonZeroTime(draft.UpdatedAt)}, ProjectID: projectID, DocumentID: documentID, BranchID: draft.BranchID, VersionName: draft.VersionName, RelativePath: documentRelativePath(document), Status: draft.Status, DocumentFormat: draft.SchemaFormat, RawSchemaObjectKey: draft.RawSchemaObjectKey, NormalizedSchemaObjectKey: draft.NormalizedObjectKey, RawSchemaHash: draft.RawSchemaHash, NormalizedSchemaHash: draft.NormalizedSchemaHash, SchemaSizeBytes: int64(len(draft.RawSchema)), SchemaMetadata: pgdb.JSONB(`{}`), Changelog: stringPtr(draft.Changelog), SourceGitCommitID: stringPtr(draft.SourceGitCommitID), SourceType: draft.SourceType, SourceBranchID: stringPtr(draft.SourceBranchID), SourceVersionID: stringPtr(draft.SourceVersionID), BaseVersionID: stringPtr(draft.BaseVersionID), DiffPreviewJSON: diffPreviewJSON(draft.DiffPreview), CreatedByActorType: domainvdoc.AuditActorUser, CreatedByUserID: draft.CreatedBy, SubmittedAt: draft.SubmittedAt}
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
