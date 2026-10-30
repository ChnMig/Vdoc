package vdoc

import (
	"context"

	repo "vdoc/domain/documentrepo"

	"gorm.io/gorm/clause"
)

var _ repo.ShareIndex = (*Repository)(nil)

type repositoryShareIndex struct {
	ID        string `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID string `gorm:"column:project_id;type:uuid"`
}

func (repositoryShareIndex) TableName() string { return "repository_share_index" }

func (r *Repository) PutRepositoryShareIndex(ctx context.Context, id, projectID string) error {
	return mapPostgresError(r.database.WithContext(ctx).Create(&repositoryShareIndex{ID: id, ProjectID: projectID}).Error)
}

func (r *Repository) RepositoryShareProject(ctx context.Context, id string) (string, bool, error) {
	var row struct {
		ProjectID string
		Status    int
	}
	// 匿名正文读取在事务内持有父项目共享锁，归档不能在鉴权与读取之间穿过。
	err := r.database.WithContext(ctx).Table("repository_share_index AS s").Select("replace(s.project_id::text, '-', '') AS project_id, p.status").Joins("JOIN projects AS p ON p.id = s.project_id").Where("s.id = ?", id).Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "p"}}).Take(&row).Error
	if err != nil {
		return "", false, mapRecordLookupError(err)
	}
	return row.ProjectID, row.Status == 1, nil
}
