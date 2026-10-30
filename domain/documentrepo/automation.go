package documentrepo

import "time"

type AnalysisInput struct {
	FromCommitID string `json:"from_commit_id,omitempty"`
	ToCommitID   string `json:"to_commit_id"`
	DocumentID   string `json:"document_id,omitempty"`
	Message      string `json:"message,omitempty"`
	Language     string `json:"language,omitempty"`
}

// Analysis 的身份和输入都固定，后续分支推进不会改变旧分析的归属。
type Analysis struct {
	ID                string     `json:"id"`
	Sequence          int64      `json:"sequence"`
	ProjectID         string     `json:"project_id"`
	FromCommitID      string     `json:"from_commit_id,omitempty"`
	ToCommitID        string     `json:"to_commit_id"`
	DocumentID        string     `json:"document_id,omitempty"`
	Message           string     `json:"message,omitempty"`
	Language          string     `json:"language,omitempty"`
	InputFingerprint  string     `json:"input_fingerprint"`
	ProviderID        string     `json:"provider_id"`
	Model             string     `json:"model"`
	Status            string     `json:"status"`
	Content           string     `json:"content"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	EvidenceTruncated bool       `json:"evidence_truncated"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

type SubscriptionInput struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	EventTypes []string `json:"event_types"`
	BranchID   string   `json:"branch_id,omitempty"`
	DocumentID string   `json:"document_id,omitempty"`
	URL        string   `json:"url,omitempty"`
}

type Subscription struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	EventTypes   []string  `json:"event_types"`
	BranchID     string    `json:"branch_id,omitempty"`
	DocumentID   string    `json:"document_id,omitempty"`
	URL          string    `json:"url,omitempty"`
	Paused       bool      `json:"paused"`
	LastSequence int64     `json:"last_sequence"`
	Generation   int64     `json:"generation"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// 只在创建成功的响应中返回明文密钥，后续查询只返回 Subscription。
type CreatedSubscription struct {
	Subscription
	Secret string `json:"secret,omitempty"`
}

type Delivery struct {
	ID             string     `json:"id"`
	Sequence       int64      `json:"sequence"`
	SubscriptionID string     `json:"subscription_id"`
	EventID        string     `json:"event_id"`
	EventSequence  int64      `json:"event_sequence"`
	EventType      string     `json:"event_type"`
	Status         string     `json:"status"`
	AttemptCount   int        `json:"attempt_count"`
	LastError      string     `json:"last_error,omitempty"`
	HTTPStatus     int        `json:"http_status,omitempty"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	AttemptedAt    *time.Time `json:"attempted_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}
