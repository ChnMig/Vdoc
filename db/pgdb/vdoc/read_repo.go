package vdoc

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"strings"

	"gorm.io/gorm"
	domain "vdoc/domain/vdoc"
)

func (r *Repository) LoadReadState(ctx context.Context, scope domain.ReadScope) (*domain.State, error) {
	state := domain.NewState()
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reader := func(query *gorm.DB) *Repository { return &Repository{database: query} }
		if err := reader(tx.Where("id = ?", scope.ActorID)).loadUsers(ctx, state); err != nil {
			return err
		}
		if scope.ProjectID != "" {
			if err := reader(tx.Where("id = ?", scope.ProjectID)).loadProjects(ctx, state); err != nil {
				return err
			}
			if err := reader(tx.Where("project_id = ? AND user_id = ?", scope.ProjectID, scope.ActorID)).loadProjectMembers(ctx, state); err != nil {
				return err
			}
		}
		if scope.DocumentID != "" {
			if err := reader(tx.Where("id = ? AND project_id = ?", scope.DocumentID, scope.ProjectID)).loadDocuments(ctx, state); err != nil {
				return err
			}
			if scope.SummaryOwnerID == "" {
				if err := reader(tx.Where("document_id = ?", scope.DocumentID)).loadBranches(ctx, state); err != nil {
					return err
				}
			}
			if scope.Versions || scope.VersionID != "" {
				query := tx.Where("document_id = ? AND project_id = ?", scope.DocumentID, scope.ProjectID)
				if scope.VersionID != "" {
					query = query.Where("id = ?", scope.VersionID)
				}
				if err := reader(query).loadVersions(ctx, state); err != nil {
					return err
				}
			}
			if scope.Endpoints && scope.VersionID != "" {
				if err := reader(tx).loadEndpoints(ctx, state, scope.VersionID); err != nil {
					return err
				}
			}
		}
		if scope.Tokens {
			// 超级管理员指定 token 时也需要校验所有权；仅取当前 actor 或单个 token 的优化在调用端完成。
			query := tx.Where("user_id = ?", scope.ActorID)
			if scope.TokenID != "" {
				query = tx.Where("id = ?", scope.TokenID)
			}
			if err := reader(query).loadTokens(ctx, state); err != nil {
				return err
			}
		}
		if scope.SummaryOwnerID != "" {
			if err := (&Repository{database: tx}).loadSummaryReadState(ctx, scope, state); err != nil {
				return err
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return state, mapPostgresError(err)
}

func (r *Repository) loadSummaryReadState(ctx context.Context, scope domain.ReadScope, state *domain.State) error {
	query := r.database.WithContext(ctx).Where("id = ? AND document_id = ? AND project_id = ?", scope.SummaryOwnerID, scope.DocumentID, scope.ProjectID)
	switch scope.SummaryOwnerType {
	case "draft":
		if err := (&Repository{database: query}).loadDrafts(ctx, state); err != nil {
			return err
		}
		if draft := state.Drafts[scope.SummaryOwnerID]; draft != nil {
			if err := (&Repository{database: r.database.Where("id = ? AND document_id = ?", draft.BranchID, scope.DocumentID)}).loadBranches(ctx, state); err != nil {
				return err
			}
		}
	case "version":
		if err := (&Repository{database: query}).loadVersions(ctx, state); err != nil {
			return err
		}
	case "diff":
		var row DocumentVersionDiff
		result := r.database.WithContext(ctx).Where("id = ? AND document_id = ?", scope.SummaryOwnerID, scope.DocumentID).Find(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			diff := &domain.Diff{ID: domainID(row.ID), ServiceID: domainID(row.DocumentID), DocumentID: domainID(row.DocumentID), ToVersionID: domainID(row.ToVersionID)}
			state.Diffs[diff.ID] = diff
			if err := (&Repository{database: r.database.Where("id = ? AND document_id = ? AND project_id = ?", row.ToVersionID, scope.DocumentID, scope.ProjectID)}).loadVersions(ctx, state); err != nil {
				return err
			}
		}
	default:
		return domain.ErrInvalidArgument
	}
	return (&Repository{database: r.database.Where("project_id = ? AND document_id = ? AND owner_type = ? AND owner_id = ?", scope.ProjectID, scope.DocumentID, scope.SummaryOwnerType, scope.SummaryOwnerID)}).loadAISummaries(ctx, state)
}

func (r *Repository) ReadVersions(ctx context.Context, documentID, branchID string, page domain.PageQuery) ([]*domain.ContractVersion, int, error) {
	query := r.database.WithContext(ctx).Model(&DocumentVersion{}).Where("document_id = ?", documentID)
	if branchID != "" {
		query = query.Where("branch_id = ?", branchID)
	}
	if page.Search != "" {
		query = query.Where("version_name ILIKE ?", searchPattern(page.Search))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []DocumentVersion
	if err := query.Order("published_at DESC, id ASC").Offset(page.Offset).Limit(page.Limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]*domain.ContractVersion, 0, len(rows))
	for _, row := range rows {
		items = append(items, domainDocumentVersionFromModel(row))
	}
	return items, int(total), nil
}

func (r *Repository) ReadEndpoints(ctx context.Context, versionID string, page domain.PageQuery) ([]*domain.Endpoint, int, error) {
	query := r.database.WithContext(ctx).Model(&APIEndpoint{}).Where("document_version_id = ?", versionID)
	if page.Path != "" {
		query = query.Where("path LIKE ?", searchPattern(page.Path))
	}
	if page.Search != "" {
		query = query.Where(`concat_ws(' ', CASE method WHEN 1 THEN 'GET' WHEN 2 THEN 'POST' WHEN 3 THEN 'PUT' WHEN 4 THEN 'PATCH' WHEN 5 THEN 'DELETE' WHEN 6 THEN 'OPTIONS' WHEN 7 THEN 'HEAD' WHEN 8 THEN 'TRACE' END, path, summary, operation_id, array_to_string(tags, ' ')) ILIKE ?`, searchPattern(page.Search))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []APIEndpoint
	if err := query.Order("path ASC, method ASC, id ASC").Offset(page.Offset).Limit(page.Limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]*domain.Endpoint, 0, len(rows))
	for _, row := range rows {
		items = append(items, &domain.Endpoint{ID: domainID(row.ID), ContractVersionID: domainID(row.DocumentVersionID), Method: codeToMethod(row.Method), Path: row.Path, OperationID: stringValue(row.OperationID), Summary: stringValue(row.Summary), Tags: []string(row.Tags), Deprecated: row.Deprecated, Hash: row.EndpointHash, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
	}
	return items, int(total), nil
}

func searchPattern(value string) string {
	return "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value) + "%"
}

func (r *Repository) ReadAudits(ctx context.Context, input domain.AuditQuery) (*domain.AuditPage, error) {
	query := r.database.WithContext(ctx).Model(&AuditLog{})
	for _, filter := range []struct{ column, value string }{{"project_id", input.ProjectID}, {"action", input.Action}, {"resource_type", input.ResourceType}, {"resource_id", input.ResourceID}} {
		if filter.value != "" {
			query = query.Where(filter.column+" = ?", filter.value)
		}
	}
	if input.TokenIDs != nil {
		query = query.Where("actor_token_id IN ?", input.TokenIDs)
	}
	if input.From != nil {
		query = query.Where("created_at >= ?", *input.From)
	}
	if input.To != nil {
		query = query.Where("created_at < ?", *input.To)
	}
	cursor, err := domain.DecodeAuditCursor(input.Cursor)
	if err != nil {
		return nil, err
	}
	if input.Cursor != "" {
		cursorID, err := uuid.Parse(cursor.ID)
		if err != nil {
			return nil, domain.ErrInvalidArgument
		}
		query = query.Where("(created_at, id) < (?, ?::uuid)", cursor.Time, cursorID.String())
	}
	var rows []AuditLog
	if err := query.Order("created_at DESC, id DESC").Limit(input.Limit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	page := &domain.AuditPage{Items: make([]*domain.AuditLog, 0, min(len(rows), input.Limit))}
	for index, row := range rows {
		if index == input.Limit {
			page.NextCursor = domain.EncodeAuditCursor(page.Items[len(page.Items)-1])
			break
		}
		page.Items = append(page.Items, domainAuditLogFromModel(row))
	}
	return page, nil
}
