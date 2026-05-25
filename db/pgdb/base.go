package pgdb

import (
	"time"

	"gorm.io/gorm"
)

type Base struct {
	ID        string    `gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null;autoUpdateTime"`
}

type SoftDelete struct {
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;type:timestamptz"`
}
