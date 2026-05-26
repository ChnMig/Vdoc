package audit

import "time"

type Context struct {
	ActorType    int
	ActorTokenID string
	IPAddress    string
	UserAgent    string
	RequestID    string
}

type BuildParams struct {
	ID           string
	Now          time.Time
	Context      Context
	ActorType    int
	ActorUserID  string
	Action       string
	ResourceType string
	ResourceID   string
	ProjectID    string
	ServiceID    string
	Metadata     map[string]string
}
