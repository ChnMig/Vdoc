package vdoc

import "time"

// DocumentFolder 为项目内的持久化目录；空 ParentID 代表项目根目录。
type DocumentFolder struct {
	ID        string
	ProjectID string
	ParentID  string
	Name      string
	Path      string
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}
