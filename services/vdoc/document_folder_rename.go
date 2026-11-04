package vdoc

import (
	"errors"
	"strings"
	"time"

	commonvdoc "vdoc/common/vdoc"
	repo "vdoc/domain/documentrepo"
	"vdoc/utils/id"
)

// CreateDocumentFolderAtPath 支持仓库目录推导出的父目录，也可在项目根目录创建空文件夹。
func (s *Store) CreateDocumentFolderAtPath(actorID, projectID, parentPath, name string, auditCtx ...AuditContext) (out *DocumentFolder, err error) {
	name = strings.TrimSpace(name)
	if err = commonvdoc.ValidateDocumentFolderName(name); err != nil {
		return nil, err
	}
	if parentPath != "" {
		for _, segment := range strings.Split(parentPath, "/") {
			if err = commonvdoc.ValidateDocumentFolderName(segment); err != nil {
				return nil, err
			}
		}
	}
	err = s.withRepositoryStateWrite(actorID, projectID, MemberRoleAdmin, func(storage repo.Storage) error {
		catalog, e := s.repositoryDocumentsLocked(storage, projectID)
		if e != nil {
			return e
		}
		if parentPath != "" {
			if parent := s.folderByPathLocked(projectID, parentPath); parent != nil {
				parentPath = parent.Path
			} else {
				exists := false
				for _, document := range catalog {
					if _, match := replaceFolderPrefix(document.RelativePath, parentPath, parentPath); match {
						exists = true
						break
					}
				}
				if !exists {
					return ErrNotFound
				}
			}
		}
		path := name
		if parentPath != "" {
			path = parentPath + "/" + name
		}
		if s.folderByPathLocked(projectID, path) != nil {
			return ErrAlreadyExists
		}
		for _, document := range catalog {
			if folderPathContains(document.RelativePath, path) {
				return ErrAlreadyExists
			}
		}
		now := time.Now().UTC()
		parentID := ""
		if parentPath != "" {
			parentID, e = s.ensureDocumentParentFoldersLocked(actorID, projectID, parentPath+"/__folder__.txt", now)
			if e != nil {
				return e
			}
			path = s.documentFolders[parentID].Path + "/" + name
		}
		folder := &DocumentFolder{ID: id.GenerateID(), ProjectID: projectID, ParentID: parentID, Name: name, Path: path, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
		s.documentFolders[folder.ID] = folder
		s.auditLocked(auditContext(auditCtx), AuditActorUser, actorID, "document_folder.create", "document_folder", folder.ID, projectID, "", auditMetadata("result", "success", "path", folder.Path))
		copy := *folder
		out = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RenameDocumentFolder 只更新当前目录，保留提交、发布和旧版本的历史路径。
// 路径也可指向由文档仓库目录推导出的文件夹，无需客户端了解内部文件夹 ID。
func (s *Store) RenameDocumentFolder(actorID, projectID, folderPath, name string, auditCtx ...AuditContext) (out *DocumentFolder, err error) {
	return s.UpdateDocumentFolderLocation(actorID, projectID, folderPath, &name, nil, auditCtx...)
}

func (s *Store) UpdateDocumentFolderLocation(actorID, projectID, folderPath string, requestedName, parentPath *string, auditCtx ...AuditContext) (out *DocumentFolder, err error) {
	if requestedName == nil && parentPath == nil {
		return nil, ErrInvalidArgument
	}
	if requestedName != nil {
		name := strings.TrimSpace(*requestedName)
		if err = commonvdoc.ValidateDocumentFolderName(name); err != nil {
			return nil, err
		}
		requestedName = &name
	}
	if folderPath == "" {
		return nil, ErrInvalidArgument
	}
	for _, segment := range strings.Split(folderPath, "/") {
		if err = commonvdoc.ValidateDocumentFolderName(segment); err != nil {
			return nil, err
		}
	}
	if parentPath != nil && *parentPath != "" {
		for _, segment := range strings.Split(*parentPath, "/") {
			if err = commonvdoc.ValidateDocumentFolderName(segment); err != nil {
				return nil, err
			}
		}
	}
	err = s.withRepositoryStateWrite(actorID, projectID, MemberRoleAdmin, func(storage repo.Storage) error {
		catalog, e := s.repositoryDocumentsLocked(storage, projectID)
		if e != nil {
			return e
		}
		folder := s.folderByPathLocked(projectID, folderPath)
		if folder != nil {
			folderPath = folder.Path
		} else {
			exists := false
			for _, document := range catalog {
				if _, matches := replaceFolderPrefix(document.RelativePath, folderPath, folderPath); matches {
					exists = true
					break
				}
			}
			if !exists {
				return ErrNotFound
			}
		}
		segments := strings.Split(folderPath, "/")
		name := segments[len(segments)-1]
		if requestedName != nil {
			name = *requestedName
		}
		parent := strings.Join(segments[:len(segments)-1], "/")
		if parentPath != nil {
			parent = *parentPath
			if folderPathContains(parent, folderPath) {
				return ErrInvalidArgument
			}
			if parent != "" {
				if target := s.folderByPathLocked(projectID, parent); target != nil {
					parent = target.Path
				} else {
					exists := false
					for _, document := range catalog {
						if _, matches := replaceFolderPrefix(document.RelativePath, parent, parent); matches {
							exists = true
							break
						}
					}
					if !exists {
						return ErrNotFound
					}
				}
			}
		}
		newPath := name
		if parent != "" {
			newPath = parent + "/" + name
		}
		for _, other := range s.documentFolders {
			if other.ProjectID != projectID || folderPathContains(other.Path, folderPath) {
				continue
			}
			if folderPathContains(other.Path, newPath) {
				return ErrAlreadyExists
			}
		}
		for _, document := range catalog {
			if !folderPathContains(document.RelativePath, folderPath) && folderPathContains(document.RelativePath, newPath) {
				return ErrAlreadyExists
			}
		}
		now := time.Now().UTC()
		if folder == nil {
			folderID, e := s.ensureDocumentParentFoldersLocked(actorID, projectID, folderPath+"/__folder__.txt", now)
			if e != nil {
				return e
			}
			folder = s.documentFolders[folderID]
		}
		if parentPath != nil {
			folder.ParentID = ""
			if parent != "" {
				parentID, e := s.ensureDocumentParentFoldersLocked(actorID, projectID, parent+"/__folder__.txt", now)
				if e != nil {
					return e
				}
				folder.ParentID = parentID
			}
		}
		for _, item := range s.documentFolders {
			if item.ProjectID != projectID {
				continue
			}
			if strings.EqualFold(item.Path, folderPath) {
				item.Name, item.Path, item.UpdatedAt = name, newPath, now
			} else if renamed, matches := replaceFolderPrefix(item.Path, folderPath, newPath); matches {
				item.Path, item.UpdatedAt = renamed, now
			}
		}
		for _, document := range s.apiServices {
			if document.ProjectID == projectID {
				if renamed, matches := replaceFolderPrefix(document.RelativePath, folderPath, newPath); matches {
					document.RelativePath, document.UpdatedAt = renamed, now
				}
			}
		}
		for _, document := range catalog {
			renamed, matches := replaceFolderPrefix(document.RelativePath, folderPath, newPath)
			if !matches {
				continue
			}
			document.RelativePath, document.UpdatedAt = renamed, now
			scoped := documentRepositoryStorage{Storage: storage, documentID: document.ID}
			record, e := scoped.RepositoryRecord(s.requestContext(), projectID, "document", document.ID)
			generation := int64(1)
			var expected *int64
			if e == nil {
				generation, expected = record.Generation+1, &record.Generation
			} else if !errors.Is(e, ErrNotFound) {
				return e
			}
			if e = repositoryPut(s.requestContext(), scoped, projectID, "document", document.ID, document, generation, expected); e != nil {
				return e
			}
		}
		action := "document_folder.rename"
		if parentPath != nil {
			action = "document_folder.move"
		}
		s.auditLocked(auditContext(auditCtx), AuditActorUser, actorID, action, "document_folder", folder.ID, projectID, "", auditMetadata("result", "success", "from_path", folderPath, "path", newPath))
		copy := *folder
		out = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) DeleteDocumentFolder(actorID, projectID, folderPath string, auditCtx ...AuditContext) error {
	if folderPath == "" {
		return ErrInvalidArgument
	}
	return s.withRepositoryStateWrite(actorID, projectID, MemberRoleAdmin, func(storage repo.Storage) error {
		folder := s.folderByPathLocked(projectID, folderPath)
		if folder == nil {
			return ErrNotFound
		}
		for _, child := range s.documentFolders {
			if child.ProjectID == projectID && child.ID != folder.ID && folderPathContains(child.Path, folder.Path) {
				return ErrFailedPrecondition
			}
		}
		catalog, err := s.repositoryDocumentsLocked(storage, projectID)
		if err != nil {
			return err
		}
		for _, document := range catalog {
			if _, contained := replaceFolderPrefix(document.RelativePath, folder.Path, folder.Path); contained {
				return ErrFailedPrecondition
			}
		}
		delete(s.documentFolders, folder.ID)
		s.auditLocked(auditContext(auditCtx), AuditActorUser, actorID, "document_folder.delete", "document_folder", folder.ID, projectID, "", auditMetadata("result", "success", "path", folder.Path))
		return nil
	})
}

func folderPathContains(value, folder string) bool {
	_, matches := replaceFolderPrefix(value, folder, folder)
	return strings.EqualFold(value, folder) || matches
}

func replaceFolderPrefix(value, oldPath, newPath string) (string, bool) {
	parts, prefix := strings.Split(value, "/"), strings.Split(oldPath, "/")
	if len(parts) <= len(prefix) {
		return value, false
	}
	for index := range prefix {
		if !strings.EqualFold(parts[index], prefix[index]) {
			return value, false
		}
	}
	return newPath + "/" + strings.Join(parts[len(prefix):], "/"), true
}
