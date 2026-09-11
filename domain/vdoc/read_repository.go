package vdoc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

type ReadScope struct {
	ActorID, ProjectID, DocumentID, VersionID, TokenID string
	Versions, Endpoints, Tokens                        bool
	SummaryOwnerType, SummaryOwnerID                   string
}

type PageQuery struct {
	Limit  int
	Offset int
	Search string
	Path   string
}

func (q PageQuery) Validate() error {
	if q.Limit < 1 || q.Limit > 200 || q.Offset < 0 || q.Offset > 1000000 || len(q.Search) > 256 || len(q.Path) > 2048 {
		return fmt.Errorf("%w: page size must be 1–200, offset 0–1000000, search at most 256 bytes", ErrInvalidArgument)
	}
	return nil
}

type AuditQuery struct {
	ProjectID, Action, ResourceType, ResourceID string
	Limit                                       int
	Cursor                                      string
	From, To                                    *time.Time
	TokenIDs                                    []string
}

type AuditCursor struct {
	Time time.Time `json:"time"`
	ID   string    `json:"id"`
}

func EncodeAuditCursor(log *AuditLog) string {
	data, _ := json.Marshal(AuditCursor{Time: log.CreatedAt, ID: log.ID})
	return base64.RawURLEncoding.EncodeToString(data)
}

func DecodeAuditCursor(value string) (AuditCursor, error) {
	var cursor AuditCursor
	if value == "" {
		return cursor, nil
	}
	if len(value) > 512 {
		return cursor, ErrInvalidArgument
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Time.IsZero() || cursor.ID == "" || len(cursor.ID) > 64 {
		return cursor, fmt.Errorf("%w: invalid audit cursor", ErrInvalidArgument)
	}
	return cursor, nil
}

type AuditPage struct {
	Items      []*AuditLog
	NextCursor string
}

// ReadRepository 提供有界读取，权限仍由应用层使用原领域规则判定。
type ReadRepository interface {
	LoadReadState(context.Context, ReadScope) (*State, error)
	ReadVersions(context.Context, string, string, PageQuery) ([]*ContractVersion, int, error)
	ReadEndpoints(context.Context, string, PageQuery) ([]*Endpoint, int, error)
	ReadAudits(context.Context, AuditQuery) (*AuditPage, error)
}
