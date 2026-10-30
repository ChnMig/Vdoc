package vdoc

import (
	"context"
	"encoding/json"
	"fmt"

	"vdoc/db/pgdb"
	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
)

type repositoryRecord struct {
	ProjectID  string     `gorm:"column:project_id;type:uuid;primaryKey"`
	Kind       string     `gorm:"column:kind;primaryKey"`
	ID         string     `gorm:"column:id;primaryKey"`
	Generation int64      `gorm:"column:generation"`
	Sequence   int64      `gorm:"column:sequence;autoIncrement"`
	Payload    pgdb.JSONB `gorm:"column:payload;type:jsonb"`
}

func (repositoryRecord) TableName() string { return "repository_records" }

func (r *Repository) LockRepository(ctx context.Context, projectID string) error {
	return r.database.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "vdoc-repository:"+projectID).Error
}

func (r *Repository) RepositoryHasLegacyContent(ctx context.Context, projectID string) (bool, error) {
	var found bool
	err := r.database.WithContext(ctx).Raw("SELECT EXISTS(SELECT 1 FROM document_drafts WHERE project_id = ?) OR EXISTS(SELECT 1 FROM document_versions WHERE project_id = ?)", projectID, projectID).Scan(&found).Error
	return found, mapPostgresError(err)
}

func (r *Repository) RepositoryLatestSequence(ctx context.Context, projectID, kind string) (int64, error) {
	var latest int64
	err := r.database.WithContext(ctx).Model(&repositoryRecord{}).Where("project_id = ? AND kind = ?", projectID, kind).Select("COALESCE(MAX(sequence), 0)").Scan(&latest).Error
	return latest, mapPostgresError(err)
}

func (r *Repository) RepositoryRecord(ctx context.Context, projectID, kind, id string) (*repo.Record, error) {
	var row repositoryRecord
	err := r.database.WithContext(ctx).Where("project_id = ? AND kind = ? AND id = ?", projectID, kind, id).First(&row).Error
	if err != nil {
		return nil, mapRecordLookupError(err)
	}
	return &repo.Record{ProjectID: row.ProjectID, Kind: row.Kind, ID: row.ID, Generation: row.Generation, Sequence: row.Sequence, Payload: json.RawMessage(row.Payload)}, nil
}

func (r *Repository) RepositoryRecords(ctx context.Context, projectID, kind string, after int64, limit int) ([]repo.Record, error) {
	if limit <= 0 || limit > 500 {
		return nil, domain.ErrInvalidArgument
	}
	var rows []repositoryRecord
	err := r.database.WithContext(ctx).Where("project_id = ? AND kind = ? AND sequence > ?", projectID, kind, after).Order("sequence ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, mapPostgresError(err)
	}
	out := make([]repo.Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, repo.Record{ProjectID: row.ProjectID, Kind: row.Kind, ID: row.ID, Generation: row.Generation, Sequence: row.Sequence, Payload: json.RawMessage(row.Payload)})
	}
	return out, nil
}

func (r *Repository) PutRepositoryRecord(ctx context.Context, value repo.Record, expected *int64) error {
	if expected != nil && (value.Kind == "commit" || value.Kind == "event" || value.Kind == "release" || value.Kind == "legacy_map" || value.Kind == "idempotency") {
		return fmt.Errorf("%w: immutable repository record", domain.ErrFailedPrecondition)
	}
	if !json.Valid(value.Payload) {
		return domain.ErrInvalidArgument
	}
	row := repositoryRecord{ProjectID: value.ProjectID, Kind: value.Kind, ID: value.ID, Generation: value.Generation, Payload: pgdb.JSONB(value.Payload)}
	if expected == nil {
		return mapPostgresError(r.database.WithContext(ctx).Create(&row).Error)
	}
	result := r.database.WithContext(ctx).Model(&repositoryRecord{}).Where("project_id = ? AND kind = ? AND id = ? AND generation = ?", value.ProjectID, value.Kind, value.ID, *expected).Updates(map[string]any{"payload": row.Payload, "generation": row.Generation})
	if result.Error != nil {
		return mapPostgresError(result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: REF_CONFLICT: repository reference changed", domain.ErrFailedPrecondition)
	}
	return nil
}
