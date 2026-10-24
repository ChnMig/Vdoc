package vdoc

import (
	"context"
	"fmt"

	"vdoc/db/pgdb"
	domainvdoc "vdoc/domain/vdoc"
)

// DocumentFolder 的父节点在同一项目中，目录路径创建后不可变。
type DocumentFolder struct {
	pgdb.Base
	ProjectID string  `gorm:"column:project_id;type:uuid;not null"`
	ParentID  *string `gorm:"column:parent_id;type:uuid"`
	Name      string  `gorm:"column:name;type:text;not null"`
	Path      string  `gorm:"column:path;type:text;not null"`
	CreatedBy string  `gorm:"column:created_by;type:uuid;not null"`
}

func (DocumentFolder) TableName() string { return TableNameDocumentFolders }

func (r *Repository) loadDocumentFolders(ctx context.Context, loaded *domainvdoc.State) error {
	var models []DocumentFolder
	if err := r.database.WithContext(ctx).Find(&models).Error; err != nil {
		return err
	}
	for _, model := range models {
		folder := &domainvdoc.DocumentFolder{ID: domainID(model.ID), ProjectID: domainID(model.ProjectID), ParentID: stringValueID(model.ParentID), Name: model.Name, Path: model.Path, CreatedBy: domainID(model.CreatedBy), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
		loaded.DocumentFolders[folder.ID] = folder
	}
	return nil
}

func (r *Repository) UpsertDocumentFolder(ctx context.Context, folder *domainvdoc.DocumentFolder) error {
	if r == nil || r.database == nil {
		return fmt.Errorf("postgres repository is not initialized")
	}
	if folder == nil {
		return nil
	}
	model := &DocumentFolder{Base: pgdb.Base{ID: folder.ID, CreatedAt: nonZeroTime(folder.CreatedAt), UpdatedAt: nonZeroTime(folder.UpdatedAt)}, ProjectID: folder.ProjectID, ParentID: stringPtr(folder.ParentID), Name: folder.Name, Path: folder.Path, CreatedBy: folder.CreatedBy}
	return r.upsertByID(ctx, model)
}
