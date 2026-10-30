package mcp

import (
	"time"

	app "vdoc/appstore"
	"vdoc/domain/documentrepo"
)

type mcpRepositoryAnalysis struct {
	ID                string     `json:"id"`
	Sequence          int64      `json:"sequence"`
	ProjectID         string     `json:"project_id"`
	FromCommitID      string     `json:"from_commit_id,omitempty"`
	ToCommitID        string     `json:"to_commit_id"`
	DocumentID        string     `json:"document_id,omitempty"`
	Message           string     `json:"message,omitempty"`
	Language          string     `json:"language,omitempty"`
	InputFingerprint  string     `json:"input_fingerprint"`
	Model             string     `json:"model"`
	Status            string     `json:"status"`
	Content           string     `json:"content"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	EvidenceTruncated bool       `json:"evidence_truncated"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

func executeRepositoryAnalysis(store *app.Store, actorID, projectID string, input documentrepo.AnalysisInput) (any, error) {
	value, err := store.RepositoryAnalyze(actorID, projectID, input)
	if err != nil {
		return nil, err
	}
	return mcpRepositoryAnalysis{ID: value.ID, Sequence: value.Sequence, ProjectID: value.ProjectID, FromCommitID: value.FromCommitID, ToCommitID: value.ToCommitID, DocumentID: value.DocumentID, Message: value.Message, Language: value.Language, InputFingerprint: value.InputFingerprint, Model: value.Model, Status: value.Status, Content: value.Content, ErrorMessage: value.ErrorMessage, EvidenceTruncated: value.EvidenceTruncated, CreatedBy: value.CreatedBy, CreatedAt: value.CreatedAt, CompletedAt: value.CompletedAt}, nil
}
