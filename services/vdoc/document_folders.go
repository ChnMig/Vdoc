package vdoc

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	commonvdoc "vdoc/common/vdoc"
	domaindocument "vdoc/domain/document"
	domainbranch "vdoc/domain/documentbranch"
	"vdoc/utils/id"
)

type DocumentLocation struct {
	FolderID string
	FileName string
}

type InitialDocumentContent struct {
	Content     string `json:"content"`
	VersionName string `json:"version_name,omitempty"`
	Changelog   string `json:"changelog,omitempty"`
}

type CreateDocumentResult struct {
	Document *APIService
	Draft    *ContractDraft
}

func (s *Store) ListDocumentFolders(actorID, projectID string) ([]*DocumentFolder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if s.projects[projectID] == nil {
		return nil, ErrNotFound
	}
	result := make([]*DocumentFolder, 0)
	for _, folder := range s.documentFolders {
		if folder.ProjectID == projectID {
			copied := *folder
			result = append(result, &copied)
		}
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Path) < strings.ToLower(result[j].Path) })
	return result, nil
}

func (s *Store) CreateDocumentFolder(actorID, projectID, parentID, name string, auditCtx ...AuditContext) (_ *DocumentFolder, err error) {
	s = s.withMutationGuard(actorID, projectID, "", MemberRoleAdmin)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	project := s.projects[projectID]
	if project == nil {
		return nil, ErrNotFound
	}
	if project.Status != ProjectStatusActive {
		return nil, ErrFailedPrecondition
	}
	name = strings.TrimSpace(name)
	if err = commonvdoc.ValidateDocumentFolderName(name); err != nil {
		return nil, err
	}
	parentPath := ""
	if parentID != "" {
		parent := s.documentFolders[parentID]
		if parent == nil || parent.ProjectID != projectID {
			return nil, ErrNotFound
		}
		parentPath = parent.Path
	}
	folderPath := name
	if parentPath != "" {
		folderPath = parentPath + "/" + name
	}
	if s.folderByPathLocked(projectID, folderPath) != nil {
		return nil, ErrAlreadyExists
	}
	before := s.cloneStateLocked()
	defer func() {
		if err != nil {
			s.applyStateLocked(before)
		}
	}()
	now := time.Now()
	folder := &DocumentFolder{ID: id.GenerateID(), ProjectID: projectID, ParentID: parentID, Name: name, Path: folderPath, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	s.documentFolders[folder.ID] = folder
	s.auditLocked(auditContext(auditCtx), AuditActorUser, actorID, "document_folder.create", "document_folder", folder.ID, projectID, "", auditMetadata("result", "success", "path", folder.Path))
	if err = s.persistLocked(); err != nil {
		return nil, err
	}
	copied := *folder
	return &copied, nil
}

func (s *Store) folderByPathLocked(projectID, folderPath string) *DocumentFolder {
	for _, folder := range s.documentFolders {
		if folder.ProjectID == projectID && strings.EqualFold(folder.Path, folderPath) {
			return folder
		}
	}
	return nil
}

// 兼容旧客户端传入路径：逐层补齐目录，但不改写原始文档路径和历史快照。
func (s *Store) ensureDocumentParentFoldersLocked(actorID, projectID, relativePath string, now time.Time) (string, error) {
	if relativePath == "" {
		return "", nil
	}
	if err := commonvdoc.ValidateRelativePath(relativePath); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	segments := strings.Split(relativePath, "/")
	for _, segment := range segments {
		if err := commonvdoc.ValidateDocumentFolderName(segment); err != nil {
			return "", err
		}
	}
	parentID, folderPath := "", ""
	for _, name := range segments[:len(segments)-1] {
		if folderPath == "" {
			folderPath = name
		} else {
			folderPath += "/" + name
		}
		if existing := s.folderByPathLocked(projectID, folderPath); existing != nil {
			parentID, folderPath = existing.ID, existing.Path
			continue
		}
		folder := &DocumentFolder{ID: id.GenerateID(), ProjectID: projectID, ParentID: parentID, Name: name, Path: folderPath, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
		s.documentFolders[folder.ID] = folder
		parentID = folder.ID
	}
	return parentID, nil
}

func (s *Store) resolveDocumentLocationLocked(actorID, projectID string, documentType int, relativePath string, location *DocumentLocation, now time.Time) (string, string, error) {
	if location == nil {
		folderID, err := s.ensureDocumentParentFoldersLocked(actorID, projectID, relativePath, now)
		return relativePath, folderID, err
	}
	if relativePath != "" {
		return "", "", fmt.Errorf("%w: relative_path cannot be combined with file_name or folder_id", ErrInvalidArgument)
	}
	filename, err := commonvdoc.NormalizeDocumentFileName(documentType, location.FileName)
	if err != nil {
		return "", "", err
	}
	if location.FolderID == "" {
		return filename, "", nil
	}
	folder := s.documentFolders[location.FolderID]
	if folder == nil || folder.ProjectID != projectID {
		return "", "", ErrNotFound
	}
	return folder.Path + "/" + filename, folder.ID, nil
}

func (s *Store) ensureDocumentPathAvailableLocked(projectID, documentID, relativePath string) error {
	if relativePath == "" {
		return nil
	}
	for _, existing := range s.apiServices {
		if existing.ID != documentID && existing.ProjectID == projectID && strings.EqualFold(existing.RelativePath, relativePath) {
			return ErrAlreadyExists
		}
	}
	return nil
}

// CreateDocumentWithLocation 将文档、默认环境分支和可选首个草稿一次提交。
func (s *Store) CreateDocumentWithLocation(actorID, projectID, name string, documentType int, relativePath, description string, location *DocumentLocation, initial *InitialDocumentContent, auditCtx ...AuditContext) (_ *CreateDocumentResult, err error) {
	s = s.withMutationGuard(actorID, projectID, "", MemberRoleAdmin)
	s.creatingDocument = true
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err = s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	project := s.projects[projectID]
	if project == nil {
		return nil, ErrNotFound
	}
	if project.Status != ProjectStatusActive {
		return nil, ErrFailedPrecondition
	}
	before := s.cloneStateLocked()
	defer func() {
		if err != nil {
			s.applyStateLocked(before)
		}
	}()
	if documentType == 0 {
		documentType = DocumentTypeOpenAPI
	}
	now := time.Now()
	resolvedPath, folderID, err := s.resolveDocumentLocationLocked(actorID, projectID, documentType, relativePath, location, now)
	if err != nil {
		return nil, err
	}
	if err = s.ensureDocumentPathAvailableLocked(projectID, "", resolvedPath); err != nil {
		return nil, err
	}
	document, err := domaindocument.Create(domaindocument.CreateParams{ID: id.GenerateID(), ProjectID: projectID, Name: name, DocumentType: documentType, RelativePath: resolvedPath, Description: description, ActorID: actorID, Now: now, Existing: s.servicesForProjectLocked(projectID)})
	if err != nil {
		return nil, err
	}
	document.FolderID = folderID
	s.apiServices[document.ID] = document
	defaultBranchID := ""
	for _, branch := range domainbranch.DefaultEnvironmentBranches(domainbranch.DefaultBranchesParams{DocumentID: document.ID, ActorID: actorID, Now: now, NewID: id.GenerateID}) {
		s.branches[branch.ID] = branch
		if branch.IsDefault {
			defaultBranchID = branch.ID
		}
	}
	s.auditLocked(ctx, AuditActorUser, actorID, "document.create", "document", document.ID, projectID, document.ID, auditMetadata("result", "success", "name", document.Name, "document_type", fmt.Sprint(document.DocumentType), "relative_path", document.RelativePath))
	result := &CreateDocumentResult{Document: cloneService(document)}
	if initial != nil {
		versionName := strings.TrimSpace(initial.VersionName)
		if versionName == "" {
			versionName = "0.1.0"
		}
		input := DraftInput{BranchID: defaultBranchID, VersionName: versionName, Changelog: initial.Changelog, SchemaContent: initial.Content}
		if isTextDocumentType(document.DocumentType) {
			result.Draft, err = s.createMarkdownDraftLocked(actorID, projectID, document.ID, input, SourceTypeWebUpload, ctx)
		} else {
			result.Draft, err = s.createDraftLocked(actorID, projectID, document.ID, input, SourceTypeWebUpload, ctx)
		}
		if err != nil {
			return nil, err
		}
	} else if err = s.persistLocked(); err != nil {
		return nil, err
	}
	result.Document = s.documentWithWorkflowLocked(actorID, document)
	return result, nil
}

func documentFileName(document *APIService) string {
	if document == nil || document.RelativePath == "" {
		return ""
	}
	return path.Base(document.RelativePath)
}
