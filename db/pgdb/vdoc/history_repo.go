package vdoc

import (
	"context"
	domain "vdoc/domain/vdoc"
)

func (r *Repository) ReadDraftPage(ctx context.Context, documentID, branchID string, page domain.PageQuery) ([]*domain.ContractDraft, int, error) {
	if err := page.Validate(); err != nil {
		return nil, 0, err
	}
	query := r.database.WithContext(ctx).Model(&DocumentDraft{}).Where("document_id = ?", documentID)
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
	var rows []DocumentDraft
	if err := query.Omit("diff_preview_json").Order("created_at DESC, id DESC").Offset(page.Offset).Limit(page.Limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*domain.ContractDraft, 0, len(rows))
	for _, row := range rows {
		out = append(out, &domain.ContractDraft{ID: domainID(row.ID), DocumentID: documentID, ServiceID: documentID, BranchID: domainID(row.BranchID), VersionName: row.VersionName, Changelog: stringValue(row.Changelog), SchemaFormat: row.DocumentFormat, SourceType: row.SourceType, Status: row.Status, CreatedBy: domainID(row.CreatedByUserID), SubmittedAt: row.SubmittedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
	}
	return out, int(total), nil
}

func (r *Repository) ReadDiffPage(ctx context.Context, documentID, fromID, toID string, page domain.PageQuery) ([]*domain.Diff, int, error) {
	if err := page.Validate(); err != nil {
		return nil, 0, err
	}
	query := r.database.WithContext(ctx).Model(&DocumentVersionDiff{}).Where("document_id = ?", documentID)
	if fromID != "" {
		query = query.Where("from_version_id = ?", fromID)
	}
	if toID != "" {
		query = query.Where("to_version_id = ?", toID)
	}
	if page.Search != "" {
		query = query.Where(`from_version_id IN (SELECT id FROM document_versions WHERE document_id = ? AND version_name ILIKE ?) OR to_version_id IN (SELECT id FROM document_versions WHERE document_id = ? AND version_name ILIKE ?)`, documentID, searchPattern(page.Search), documentID, searchPattern(page.Search))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []DocumentVersionDiff
	if err := query.Select("id", "document_id", "from_version_id", "to_version_id", "diff_status", "created_at", "updated_at").Order("created_at DESC, id DESC").Offset(page.Offset).Limit(page.Limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*domain.Diff, 0, len(rows))
	for _, row := range rows {
		out = append(out, &domain.Diff{ID: domainID(row.ID), DocumentID: documentID, ServiceID: documentID, FromVersionID: domainID(row.FromVersionID), ToVersionID: domainID(row.ToVersionID), DiffStatus: row.DiffStatus, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
	}
	return out, int(total), nil
}
