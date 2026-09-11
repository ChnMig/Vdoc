package vdoc

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
	"vdoc/db/pgdb"
	domain "vdoc/domain/vdoc"
)

func versionContentMetadata(version *domain.ContractVersion) pgdb.JSONB {
	if version.RawSchema == "" {
		return pgdb.JSONB(`{}`)
	}
	body, _ := json.Marshal(map[string]int{"raw_line_count": strings.Count(version.RawSchema, "\n") + 1})
	return pgdb.JSONB(body)
}

func (r *Repository) ReadDocumentOverview(ctx context.Context, documentID string) (*domain.DocumentOverview, error) {
	out := &domain.DocumentOverview{PublishedBranchIDs: []string{}}
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&DocumentVersion{}).Where("document_id = ?", documentID).Count(&out.VersionCount).Error; err != nil {
			return err
		}
		var version DocumentVersion
		result := tx.Where("document_id = ?", documentID).Order("published_at DESC, id ASC").Limit(1).Find(&version)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			out.LatestVersion = domainDocumentVersionFromModel(version)
			out.RawSizeBytes = version.SchemaSizeBytes
			var metadata struct {
				RawLineCount *int64 `json:"raw_line_count"`
			}
			if err := json.Unmarshal(version.SchemaMetadata, &metadata); err != nil {
				return err
			}
			out.RawLineCount = metadata.RawLineCount
			if err := tx.Model(&APIEndpoint{}).Where("document_version_id = ?", version.ID).Count(&out.EndpointCount).Error; err != nil {
				return err
			}
		}
		if err := tx.Raw(`SELECT DISTINCT v.branch_id FROM document_versions v
		JOIN document_branches b ON b.id=v.branch_id
		WHERE v.document_id=? AND v.status=1 AND b.status=1 ORDER BY v.branch_id`, documentID).Scan(&out.PublishedBranchIDs).Error; err != nil {
			return err
		}
		for i, branch := range out.PublishedBranchIDs {
			out.PublishedBranchIDs[i] = domainID(branch)
		}
		return tx.Raw(`SELECT EXISTS(SELECT 1 FROM document_drafts d JOIN document_branches b ON b.id=d.branch_id
		WHERE d.document_id=? AND d.status IN (2,5) AND b.status=1)`, documentID).Scan(&out.HasReviewedDraft).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return out, mapPostgresError(err)
}

// 老版本首次读取时补齐正文统计；只更新不可变原文的派生元数据。
func (r *Repository) SaveVersionContentStats(ctx context.Context, versionID, hash string, size, lines int64) error {
	return r.database.WithContext(ctx).Exec(`UPDATE document_versions SET schema_size_bytes=?,
	schema_metadata=jsonb_set(schema_metadata, '{raw_line_count}', to_jsonb(?::bigint))
	WHERE id=? AND raw_schema_hash=? AND NOT jsonb_exists(schema_metadata, 'raw_line_count')`, size, lines, versionID, hash).Error
}

func (r *Repository) ReadLatestMCPRead(ctx context.Context, projectID, documentID string, tokens []string) (*time.Time, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	var rows []struct{ CreatedAt time.Time }
	err := r.database.WithContext(ctx).Table("audit_logs").Select("created_at").
		Where("project_id = ? AND document_id = ? AND actor_token_id IN ?", projectID, documentID, tokens).
		Where("action = 'mcp.tool_call' AND metadata->>'evidence_kind' = 'published_content_read' AND metadata->>'result' = 'success'").
		Order("created_at DESC").Limit(1).Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0].CreatedAt, nil
}
