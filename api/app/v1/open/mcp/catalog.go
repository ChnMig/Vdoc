package mcp

import (
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
)

func catalogFolderSchema(operation string) gin.H {
	properties := gin.H{"project_id": stringProperty("Project ID from list_projects.")}
	required := []string{"project_id"}
	switch operation {
	case "create":
		properties["name"] = stringProperty("New folder name, without path separators.")
		properties["parent_path"] = stringProperty("Existing parent folder path; omit or use an empty string for the project root.")
		required = append(required, "name")
	case "update":
		properties["path"] = stringProperty("Current folder path from list_folders.")
		properties["name"] = stringProperty("Optional replacement name, without path separators.")
		properties["parent_path"] = stringProperty("Optional destination folder path; an empty string moves to the project root.")
		required = append(required, "path")
	case "delete":
		properties["path"] = stringProperty("Exact path of an empty folder from list_folders.")
		required = append(required, "path")
	}
	schema := inputSchema(required, properties)
	if operation == "update" {
		schema["anyOf"] = []gin.H{{"required": []string{"name"}}, {"required": []string{"parent_path"}}}
	}
	return schema
}

func catalogDocumentSchema(deleting bool) gin.H {
	properties := gin.H{
		"project_id":  stringProperty("Project ID from list_projects."),
		"document_id": stringProperty("Stable document ID from list_documents."),
	}
	if !deleting {
		properties["name"] = stringProperty("Optional replacement document display name.")
		properties["relative_path"] = stringProperty("Optional new file path in this project; moves or renames the file without rewriting history.")
		properties["description"] = stringProperty("Optional replacement description; an empty string clears it.")
	}
	schema := inputSchema([]string{"project_id", "document_id"}, properties)
	if !deleting {
		schema["anyOf"] = []gin.H{{"required": []string{"name"}}, {"required": []string{"relative_path"}}, {"required": []string{"description"}}}
	}
	return schema
}

type mcpCatalogFolder struct {
	ID         string `json:"id,omitempty"`
	ProjectID  string `json:"project_id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	ParentPath string `json:"parent_path"`
}

func catalogFolderDTO(projectID, id, folderPath string) mcpCatalogFolder {
	parent := path.Dir(folderPath)
	if parent == "." {
		parent = ""
	}
	return mcpCatalogFolder{ID: id, ProjectID: projectID, Name: path.Base(folderPath), Path: folderPath, ParentPath: parent}
}

func catalogWriteTypes(scopes []int) []int {
	types := []int{}
	for _, kind := range []int{app.DocumentTypeOpenAPI, app.DocumentTypeMarkdown, app.DocumentTypeText} {
		if repositoryWriteScope(scopes, kind) {
			types = append(types, kind)
		}
	}
	return types
}

func executeCatalogTool(store *app.Store, actor string, scopes []int, tool string, raw json.RawMessage, auditCtx ...app.AuditContext) (any, error) {
	write := tool != "list_folders"
	if write && !hasAnyScope(scopes, app.ScopeAPIDraft, app.ScopeDocDraft) || !write && !hasAnyScope(scopes, app.ScopeAPIRead, app.ScopeDocRead) {
		return nil, app.ErrPermissionDenied
	}
	if err := validateRepositoryArguments(tool, raw); err != nil {
		return nil, err
	}
	var args struct {
		ProjectID    string  `json:"project_id"`
		DocumentID   string  `json:"document_id"`
		Path         string  `json:"path"`
		Name         *string `json:"name"`
		ParentPath   *string `json:"parent_path"`
		RelativePath *string `json:"relative_path"`
		Description  *string `json:"description"`
	}
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(field("project_id", args.ProjectID)); err != nil {
		return nil, err
	}
	switch tool {
	case "list_folders":
		stored, err := store.ListDocumentFolders(actor, args.ProjectID)
		if err != nil {
			return nil, err
		}
		documents, err := store.RepositoryDocuments(actor, args.ProjectID)
		if err != nil {
			return nil, err
		}
		folders := map[string]mcpCatalogFolder{}
		for _, folder := range stored {
			folders[strings.ToLower(folder.Path)] = catalogFolderDTO(args.ProjectID, folder.ID, folder.Path)
		}
		for _, document := range documents {
			if !repositoryReadScope(scopes, document.DocumentType) {
				continue
			}
			for parent := path.Dir(document.RelativePath); parent != "." && parent != "" && parent != "/"; parent = path.Dir(parent) {
				key := strings.ToLower(parent)
				if _, exists := folders[key]; !exists {
					folders[key] = catalogFolderDTO(args.ProjectID, "", parent)
				}
			}
		}
		result := make([]mcpCatalogFolder, 0, len(folders))
		for _, folder := range folders {
			result = append(result, folder)
		}
		sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Path) < strings.ToLower(result[j].Path) })
		return result, nil
	case "create_folder":
		if args.Name == nil {
			return nil, invalidArgument("name is required")
		}
		parent := ""
		if args.ParentPath != nil {
			parent = *args.ParentPath
		}
		folder, err := store.CreateDocumentFolderAtPath(actor, args.ProjectID, parent, *args.Name, auditCtx...)
		if err != nil {
			return nil, err
		}
		return catalogFolderDTO(args.ProjectID, folder.ID, folder.Path), nil
	case "update_folder":
		if err := requireNonEmpty(field("path", args.Path)); err != nil {
			return nil, err
		}
		folder, err := store.UpdateDocumentFolderLocationWithTypes(actor, args.ProjectID, args.Path, args.Name, args.ParentPath, catalogWriteTypes(scopes), auditCtx...)
		if err != nil {
			return nil, err
		}
		return catalogFolderDTO(args.ProjectID, folder.ID, folder.Path), nil
	case "delete_folder":
		if err := requireNonEmpty(field("path", args.Path)); err != nil {
			return nil, err
		}
		if err := store.DeleteDocumentFolder(actor, args.ProjectID, args.Path, auditCtx...); err != nil {
			return nil, err
		}
		return gin.H{"deleted": true}, nil
	case "update_document":
		if err := requireNonEmpty(field("document_id", args.DocumentID)); err != nil {
			return nil, err
		}
		if args.Name == nil && args.RelativePath == nil && args.Description == nil {
			return nil, invalidArgument("supply name, relative_path or description")
		}
		document, err := store.RepositoryUpdateDocumentWithTypes(actor, args.ProjectID, args.DocumentID, app.RepositoryDocumentPatchInput{Name: args.Name, RelativePath: args.RelativePath, Description: args.Description}, catalogWriteTypes(scopes), auditCtx...)
		if err != nil {
			return nil, err
		}
		return repositoryDocumentDTO(*document), nil
	case "delete_document":
		if err := requireNonEmpty(field("document_id", args.DocumentID)); err != nil {
			return nil, err
		}
		if err := store.RepositoryDeleteDocumentWithTypes(actor, args.ProjectID, args.DocumentID, catalogWriteTypes(scopes), auditCtx...); err != nil {
			return nil, err
		}
		return gin.H{"deleted": true}, nil
	default:
		return nil, errUnknownTool
	}
}
