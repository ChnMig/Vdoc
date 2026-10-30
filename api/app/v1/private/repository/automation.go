package repository

import (
	"strconv"
	"time"

	"vdoc/api/app/v1/private/shared"
	repo "vdoc/domain/documentrepo"
	app "vdoc/services/vdoc"

	"github.com/gin-gonic/gin"
)

func RegisterAutomationRoutes(group *gin.RouterGroup) {
	group.POST("/ai/analyses", analyzeCommit)
	group.GET("/ai/analyses", analyses)
	group.GET("/ai/analyses/:analysis_id", analysis)
	group.POST("/subscriptions", createSubscription)
	group.GET("/subscriptions", subscriptions)
	group.POST("/subscriptions/:subscription_id/pause", pauseSubscription)
	group.POST("/subscriptions/:subscription_id/resume", resumeSubscription)
	group.GET("/event-deliveries", deliveries)
	group.POST("/event-deliveries/:delivery_id/retry", retryDelivery)
}

type analysisDTO struct {
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

func analysisView(v *repo.Analysis) analysisDTO {
	return analysisDTO{ID: v.ID, Sequence: v.Sequence, ProjectID: v.ProjectID, FromCommitID: v.FromCommitID, ToCommitID: v.ToCommitID, DocumentID: v.DocumentID, Message: v.Message, Language: v.Language, InputFingerprint: v.InputFingerprint, ProviderID: v.ProviderID, Model: v.Model, Status: v.Status, Content: v.Content, ErrorMessage: v.ErrorMessage, EvidenceTruncated: v.EvidenceTruncated, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, CompletedAt: v.CompletedAt}
}

type subscriptionDTO struct {
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

func subscriptionView(v repo.Subscription) subscriptionDTO {
	return subscriptionDTO{ID: v.ID, ProjectID: v.ProjectID, Name: v.Name, Kind: v.Kind, EventTypes: append([]string{}, v.EventTypes...), BranchID: v.BranchID, DocumentID: v.DocumentID, URL: v.URL, Paused: v.Paused, LastSequence: v.LastSequence, Generation: v.Generation, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt}
}

type deliveryDTO struct {
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

func deliveryView(v repo.Delivery) deliveryDTO {
	return deliveryDTO{ID: v.ID, Sequence: v.Sequence, SubscriptionID: v.SubscriptionID, EventID: v.EventID, EventSequence: v.EventSequence, EventType: v.EventType, Status: v.Status, AttemptCount: v.AttemptCount, LastError: v.LastError, HTTPStatus: v.HTTPStatus, NextAttemptAt: v.NextAttemptAt, AttemptedAt: v.AttemptedAt, CompletedAt: v.CompletedAt, CreatedAt: v.CreatedAt}
}

func automationAfter(c *gin.Context) (int64, bool) {
	value, err := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	if err != nil || value < 0 {
		shared.ReturnAppError(c, app.ErrInvalidArgument)
		return 0, false
	}
	return value, true
}

func analyzeCommit(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input struct {
		FromCommitID string `json:"from_commit_id"`
		ToCommitID   string `json:"to_commit_id"`
		DocumentID   string `json:"document_id"`
		Message      string `json:"message"`
		Language     string `json:"language"`
	}
	if !bind(c, &input) {
		return
	}
	v, err := shared.Store(c).RepositoryAnalyze(a, c.Param("project_id"), repo.AnalysisInput{FromCommitID: input.FromCommitID, ToCommitID: input.ToCommitID, DocumentID: input.DocumentID, Message: input.Message, Language: input.Language})
	if err != nil {
		result(c, nil, err)
		return
	}
	result(c, analysisView(v), nil)
}

func analysis(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, err := shared.Store(c).RepositoryAnalysis(a, c.Param("project_id"), c.Param("analysis_id"))
	if err != nil {
		result(c, nil, err)
		return
	}
	result(c, analysisView(v), nil)
}

func analyses(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	after, ok := automationAfter(c)
	if !ok {
		return
	}
	values, err := shared.Store(c).RepositoryAnalyses(a, c.Param("project_id"), after)
	out := make([]analysisDTO, 0, len(values))
	for i := range values {
		out = append(out, analysisView(&values[i]))
	}
	result(c, out, err)
}

func createSubscription(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input struct {
		Name       string   `json:"name"`
		Kind       string   `json:"kind"`
		EventTypes []string `json:"event_types"`
		BranchID   string   `json:"branch_id"`
		DocumentID string   `json:"document_id"`
		URL        string   `json:"url"`
	}
	if !bind(c, &input) {
		return
	}
	v, err := shared.Store(c).RepositoryCreateSubscription(a, c.Param("project_id"), repo.SubscriptionInput{Name: input.Name, Kind: input.Kind, EventTypes: input.EventTypes, BranchID: input.BranchID, DocumentID: input.DocumentID, URL: input.URL})
	if err != nil {
		result(c, nil, err)
		return
	}
	result(c, struct {
		subscriptionDTO
		Secret string `json:"secret,omitempty"`
	}{subscriptionView(v.Subscription), v.Secret}, nil)
}

func subscriptions(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	values, err := shared.Store(c).RepositorySubscriptions(a, c.Param("project_id"))
	out := make([]subscriptionDTO, 0, len(values))
	for _, v := range values {
		out = append(out, subscriptionView(v))
	}
	result(c, out, err)
}

func pauseSubscription(c *gin.Context)  { setSubscriptionPaused(c, true) }
func resumeSubscription(c *gin.Context) { setSubscriptionPaused(c, false) }
func setSubscriptionPaused(c *gin.Context, paused bool) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, err := shared.Store(c).RepositoryPauseSubscription(a, c.Param("project_id"), c.Param("subscription_id"), paused)
	if err != nil {
		result(c, nil, err)
		return
	}
	result(c, subscriptionView(*v), nil)
}

func deliveries(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	after, ok := automationAfter(c)
	if !ok {
		return
	}
	values, err := shared.Store(c).RepositoryDeliveries(a, c.Param("project_id"), after)
	out := make([]deliveryDTO, 0, len(values))
	for _, v := range values {
		out = append(out, deliveryView(v))
	}
	result(c, out, err)
}

func retryDelivery(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	v, err := shared.Store(c).RepositoryRetryDelivery(a, c.Param("project_id"), c.Param("delivery_id"))
	if err != nil {
		result(c, nil, err)
		return
	}
	result(c, deliveryView(*v), nil)
}
