package ai

import "time"

const (
	ProviderScopeSystem  = "system"
	ProviderScopeProject = "project"

	ProviderModeChatCompletions = "chat_completions"
	ProviderModeResponses       = "responses"

	PromptDraftReviewSummary   = "draft_review_summary"
	PromptVersionChangeSummary = "version_change_summary"
	PromptDiffChangeSummary    = "diff_change_summary"
	PromptPageChat             = "page_chat"

	SummaryStatusSkipped   = "skipped"
	SummaryStatusSucceeded = "succeeded"
	SummaryStatusFailed    = "failed"

	SummaryOwnerDraft   = "draft"
	SummaryOwnerVersion = "version"
	SummaryOwnerDiff    = "diff"

	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
)

type ProviderConfig struct {
	ID               string    `json:"id"`
	Scope            string    `json:"scope"`
	ProjectID        string    `json:"project_id,omitempty"`
	Name             string    `json:"name"`
	BaseURL          string    `json:"base_url"`
	Model            string    `json:"model"`
	APIMode          string    `json:"api_mode"`
	APIKeyCiphertext []byte    `json:"-"`
	CipherKID        string    `json:"-"`
	APIKeyLast4      string    `json:"api_key_last4,omitempty"`
	Enabled          bool      `json:"enabled"`
	CreatedBy        string    `json:"created_by"`
	UpdatedBy        string    `json:"updated_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ProviderInput struct {
	Name    string
	BaseURL string
	Model   string
	APIMode string
	APIKey  string
	Enabled bool
}

type PromptOverride struct {
	ID                 string    `json:"id"`
	Scope              string    `json:"scope"`
	ProjectID          string    `json:"project_id,omitempty"`
	PromptKey          string    `json:"prompt_key"`
	SystemPrompt       string    `json:"system_prompt"`
	UserPromptTemplate string    `json:"user_prompt_template"`
	Enabled            bool      `json:"enabled"`
	CreatedBy          string    `json:"created_by"`
	UpdatedBy          string    `json:"updated_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type PromptTemplate struct {
	PromptKey          string `json:"prompt_key"`
	SystemPrompt       string `json:"system_prompt"`
	UserPromptTemplate string `json:"user_prompt_template"`
	Enabled            bool   `json:"enabled"`
}

type Summary struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	DocumentID   string    `json:"document_id"`
	OwnerType    string    `json:"owner_type"`
	OwnerID      string    `json:"owner_id"`
	PromptKey    string    `json:"prompt_key"`
	ProviderID   string    `json:"provider_id,omitempty"`
	Status       string    `json:"status"`
	Content      string    `json:"content,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	GeneratedBy  string    `json:"generated_by"`
	GeneratedAt  time.Time `json:"generated_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ChatSession struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	DocumentID  string    `json:"document_id,omitempty"`
	ContextType string    `json:"context_type"`
	ContextID   string    `json:"context_id"`
	Title       string    `json:"title"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ChatMessage struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	ProviderID string    `json:"provider_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
