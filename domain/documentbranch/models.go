package documentbranch

import "time"

type ContractBranch struct {
	ID          string    `json:"id"`
	DocumentID  string    `json:"document_id,omitempty"`
	ServiceID   string    `json:"service_id"`
	Name        string    `json:"name"`
	Kind        int       `json:"kind"`
	Description string    `json:"description,omitempty"`
	IsDefault   bool      `json:"is_default"`
	IsProtected bool      `json:"is_protected"`
	Status      int       `json:"status"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
