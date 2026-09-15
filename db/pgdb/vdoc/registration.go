package vdoc

import "context"

// HasRegisteredUsers 在调用方已持有协作不变量锁的事务内读取初始化状态。
func (r *Repository) HasRegisteredUsers(ctx context.Context) (bool, error) {
	var ids []string
	err := r.database.WithContext(ctx).Model(&User{}).Limit(1).Pluck("id", &ids).Error
	return len(ids) > 0, err
}
