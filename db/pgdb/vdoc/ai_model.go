package vdoc

import (
	"time"

	"vdoc/db/pgdb"
)

type AIProvider struct {
	pgdb.Base
	Scope            string  `gorm:"column:scope;type:text;not null"`
	ProjectID        *string `gorm:"column:project_id;type:uuid"`
	Name             string  `gorm:"column:name;type:text;not null"`
	BaseURL          string  `gorm:"column:base_url;type:text;not null"`
	Model            string  `gorm:"column:model;type:text;not null"`
	APIMode          string  `gorm:"column:api_mode;type:text;not null"`
	APIKeyCiphertext []byte  `gorm:"column:api_key_ciphertext;type:bytea;not null"`
	CipherKID        string  `gorm:"column:cipher_kid;type:text;not null"`
	APIKeyLast4      string  `gorm:"column:api_key_last4;type:text"`
	Enabled          bool    `gorm:"column:enabled;type:boolean;not null;default:true"`
	Temperature      float64 `gorm:"column:temperature;type:double precision;not null;default:0.2"`
	TimeoutMS        int     `gorm:"column:timeout_ms;type:integer;not null;default:30000"`
	MaxOutputTokens  int     `gorm:"column:max_output_tokens;type:integer;not null;default:1000"`
	CreatedBy        string  `gorm:"column:created_by;type:uuid;not null"`
	UpdatedBy        string  `gorm:"column:updated_by;type:uuid;not null"`
}

func (AIProvider) TableName() string { return TableNameAIProviders }

type AIPromptOverride struct {
	pgdb.Base
	Scope              string  `gorm:"column:scope;type:text;not null"`
	ProjectID          *string `gorm:"column:project_id;type:uuid"`
	PromptKey          string  `gorm:"column:prompt_key;type:text;not null"`
	SystemPrompt       string  `gorm:"column:system_prompt;type:text;not null"`
	UserPromptTemplate string  `gorm:"column:user_prompt_template;type:text;not null"`
	Enabled            bool    `gorm:"column:enabled;type:boolean;not null;default:true"`
	CreatedBy          string  `gorm:"column:created_by;type:uuid;not null"`
	UpdatedBy          string  `gorm:"column:updated_by;type:uuid;not null"`
}

func (AIPromptOverride) TableName() string { return TableNameAIPromptOverrides }

type AISummary struct {
	pgdb.Base
	ProjectID           string     `gorm:"column:project_id;type:uuid;not null"`
	DocumentID          string     `gorm:"column:document_id;type:uuid;not null"`
	OwnerType           string     `gorm:"column:owner_type;type:text;not null"`
	OwnerID             string     `gorm:"column:owner_id;type:uuid;not null"`
	PromptKey           string     `gorm:"column:prompt_key;type:text;not null"`
	ProviderID          *string    `gorm:"column:provider_id;type:uuid"`
	Status              string     `gorm:"column:status;type:text;not null"`
	Content             *string    `gorm:"column:content;type:text"`
	ErrorMessage        *string    `gorm:"column:error_message;type:text"`
	GeneratedBy         string     `gorm:"column:generated_by;type:uuid;not null"`
	GeneratedAt         *time.Time `gorm:"column:generated_at;type:timestamptz"`
	GenerationToken     string     `gorm:"column:generation_token;type:text;not null;default:''"`
	GenerationStartedAt *time.Time `gorm:"column:generation_started_at;type:timestamptz"`
}

func (AISummary) TableName() string { return TableNameAISummaries }

type AIChatSession struct {
	pgdb.Base
	ProjectID           string     `gorm:"column:project_id;type:uuid;not null"`
	DocumentID          *string    `gorm:"column:document_id;type:uuid"`
	ContextType         string     `gorm:"column:context_type;type:text;not null"`
	ContextID           string     `gorm:"column:context_id;type:uuid;not null"`
	Title               string     `gorm:"column:title;type:text;not null"`
	CreatedBy           string     `gorm:"column:created_by;type:uuid;not null"`
	GenerationToken     string     `gorm:"column:generation_token;type:text;not null;default:''"`
	GenerationStartedAt *time.Time `gorm:"column:generation_started_at;type:timestamptz"`
}

func (AIChatSession) TableName() string { return TableNameAIChatSessions }

type AIChatMessage struct {
	pgdb.Base
	SessionID  string  `gorm:"column:session_id;type:uuid;not null"`
	Role       string  `gorm:"column:role;type:text;not null"`
	Content    string  `gorm:"column:content;type:text;not null"`
	ProviderID *string `gorm:"column:provider_id;type:uuid"`
}

func (AIChatMessage) TableName() string { return TableNameAIChatMessages }
