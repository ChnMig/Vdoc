package vdoc

import (
	"time"

	"vdoc/db/pgdb"
)

type SchemaObject struct {
	ObjectKey   string     `gorm:"column:object_key;type:text;primaryKey"`
	Kind        string     `gorm:"column:kind;type:text;not null"`
	OwnerType   string     `gorm:"column:owner_type;type:text;not null"`
	OwnerID     *string    `gorm:"column:owner_id;type:uuid"`
	SHA256      string     `gorm:"column:sha256;type:text;not null"`
	ContentType string     `gorm:"column:content_type;type:text;not null;default:'application/json'"`
	SizeBytes   int64      `gorm:"column:size_bytes;type:bigint;not null"`
	ETag        *string    `gorm:"column:etag;type:text"`
	Metadata    pgdb.JSONB `gorm:"column:metadata;type:jsonb;not null;default:'{}'"`
	CreatedAt   time.Time  `gorm:"column:created_at;type:timestamptz;not null;autoCreateTime"`
}

func (SchemaObject) TableName() string { return TableNameSchemaObjects }
