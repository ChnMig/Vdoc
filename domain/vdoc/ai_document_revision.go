package vdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// AIDocumentRevision 只涵盖当前文档的来源元数据，不因内容缓存水合而变化。
func AIDocumentRevision(state *State, projectID, documentID string) string {
	rows := make([]string, 0)
	add := func(fields ...any) {
		encoded, _ := json.Marshal(fields)
		rows = append(rows, string(encoded))
	}
	if document := state.APIServices[documentID]; document != nil && document.ProjectID == projectID {
		add("document", document.ID, document.Name, document.RelativePath, document.Status)
	}
	for _, branch := range state.Branches {
		if branch.ServiceID == documentID {
			add("branch", branch.ID, branch.Name, branch.Description, branch.Status)
		}
	}
	for _, draft := range state.Drafts {
		if draft.ProjectID == projectID && draft.ServiceID == documentID {
			add("draft", draft.ID, draft.Revision(), draft.NormalizedSchemaHash)
		}
	}
	for _, version := range state.Versions {
		if version.ProjectID == projectID && version.ServiceID == documentID {
			add("version", version.ID, version.BranchID, version.VersionName, version.RawSchemaHash, version.NormalizedSchemaHash, version.Changelog, version.Status)
		}
	}
	sort.Strings(rows)
	encoded, _ := json.Marshal(rows)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
