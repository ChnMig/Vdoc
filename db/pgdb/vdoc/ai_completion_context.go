package vdoc

import (
	"context"

	"gorm.io/gorm"
	domain "vdoc/domain/vdoc"
)

// 完成事务持有配置表的共享锁直到提交，既防止更新，也覆盖原本不存在的覆盖配置被插入。
// 该锁只覆盖写入 AI 结果的短事务，外部模型请求期间不持有数据库锁。
func (r *Repository) LockAICompletionConfiguration(ctx context.Context, projectID, promptKey string) (*domain.State, error) {
	if err := r.database.WithContext(ctx).Exec("LOCK TABLE " + TableNameAIProviders + ", " + TableNameAIPromptOverrides + " IN SHARE MODE").Error; err != nil {
		return nil, err
	}
	state := domain.NewState()
	providerRepo := &Repository{database: r.database.Where("project_id IS NULL OR project_id = ?", projectID)}
	if err := providerRepo.loadAIProviders(ctx, state); err != nil {
		return nil, err
	}
	promptRepo := &Repository{database: r.database.Where("(project_id IS NULL OR project_id = ?) AND prompt_key = ?", projectID, promptKey)}
	if err := promptRepo.loadAIPrompts(ctx, state); err != nil {
		return nil, err
	}
	return state, nil
}

// 配置写必须先取得表锁，再取得乐观更新的行锁；否则可能与完成事务的消息外键锁反序。
func (r *Repository) withAIConfigurationMutation(ctx context.Context, write func(*Repository) error) error {
	return r.transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Exec("LOCK TABLE " + TableNameAIProviders + ", " + TableNameAIPromptOverrides + " IN ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		return write(&Repository{database: tx})
	})
}
