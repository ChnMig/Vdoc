package documentdiff

import "time"

type Endpoint struct {
	ID                  string    `json:"id"`
	ContractVersionID   string    `json:"contract_version_id"`
	Method              string    `json:"method"`
	Path                string    `json:"path"`
	OperationID         string    `json:"operation_id,omitempty"`
	Summary             string    `json:"summary,omitempty"`
	Tags                []string  `json:"tags,omitempty"`
	Deprecated          bool      `json:"deprecated"`
	Parameters          any       `json:"parameters,omitempty"`
	RequestBody         any       `json:"request_body,omitempty"`
	Responses           any       `json:"responses,omitempty"`
	Security            any       `json:"security,omitempty"`
	Servers             any       `json:"servers,omitempty"`
	NormalizedOperation any       `json:"normalized_operation,omitempty"`
	SchemaRefs          any       `json:"schema_refs,omitempty"`
	Hash                string    `json:"hash"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Diff struct {
	ID            string      `json:"id"`
	DocumentID    string      `json:"document_id,omitempty"`
	ServiceID     string      `json:"service_id"`
	FromVersionID string      `json:"from_version_id,omitempty"`
	ToVersionID   string      `json:"to_version_id,omitempty"`
	ObjectKey     string      `json:"diff_object_key,omitempty"`
	Hash          string      `json:"diff_hash,omitempty"`
	DiffStatus    int         `json:"diff_status"`
	Summary       DiffSummary `json:"summary"`
	Items         []DiffItem  `json:"items"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

type DiffSummary struct {
	AddedEndpoints    int `json:"added_endpoints"`
	RemovedEndpoints  int `json:"removed_endpoints"`
	ModifiedEndpoints int `json:"modified_endpoints"`
	BreakingChanges   int `json:"breaking_changes"`
	DocumentFormat    int `json:"document_format,omitempty"`
	AddedLines        int `json:"added_lines,omitempty"`
	RemovedLines      int `json:"removed_lines,omitempty"`
	ModifiedLines     int `json:"modified_lines,omitempty"`
	ModifiedBlocks    int `json:"modified_blocks,omitempty"`
}

type DiffItem struct {
	ID             string `json:"id"`
	ChangeType     int    `json:"change_type"`
	Severity       int    `json:"severity"`
	Method         string `json:"method,omitempty"`
	Path           string `json:"path,omitempty"`
	OperationID    string `json:"operation_id,omitempty"`
	Location       string `json:"location,omitempty"`
	OldValue       any    `json:"old_value,omitempty"`
	NewValue       any    `json:"new_value,omitempty"`
	Message        string `json:"message"`
	FrontendImpact string `json:"frontend_impact,omitempty"`
	IsBreaking     bool   `json:"is_breaking"`
	MustHandle     bool   `json:"must_handle"`
	SortOrder      int    `json:"sort_order"`
}
