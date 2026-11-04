package vdoc

import (
	"context"
	"errors"
	"reflect"
	"testing"

	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
)

func TestRenameDocumentFolderUpdatesCatalogAndKeepsHistory(t *testing.T) {
	s := newTask5Store()
	root, err := s.CreateDocumentFolder("admin", "project-a", "", "Docs")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateDocumentFolder("admin", "project-a", root.ID, "Empty")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := s.CreateDocument("admin", "project-a", "Legacy", DocumentTypeText, "Docs/legacy.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Guide", RelativePath: "docs/API/guide.md", DocumentType: DocumentTypeMarkdown, Content: "original\r\n", IdempotencyKey: "folder-rename-create"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Other", RelativePath: "DocsExtra/other.md", DocumentType: DocumentTypeMarkdown, Content: "untouched", IdempotencyKey: "folder-rename-other"})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := s.RenameDocumentFolder("admin", "project-a", "docs", "Manual")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != root.ID || renamed.Path != "Manual" || s.documentFolders[child.ID].Path != "Manual/Empty" || s.apiServices[legacy.ID].RelativePath != "Manual/legacy.txt" {
		t.Fatalf("folder/legacy locations were not updated: %+v", renamed)
	}
	catalog, err := s.RepositoryDocuments("reader", "project-a")
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, d := range catalog {
		paths[d.ID] = d.RelativePath
	}
	if paths[created.Document.ID] != "Manual/API/guide.md" || paths[other.Document.ID] != "DocsExtra/other.md" {
		t.Fatalf("catalog paths: %v", paths)
	}
	scoped := s.WithRepositoryDocument(created.Document.ID)
	old, err := scoped.RepositoryContent("reader", "project-a", created.Commit.ID, created.Document.ID)
	if err != nil || old.Content != "original\r\n" || old.Document.RelativePath != "docs/API/guide.md" {
		t.Fatalf("history changed: %+v %v", old, err)
	}
	// 改名之前打开的编辑器继续保存正文，也不能复原旧目录。
	saved, err := scoped.RepositorySave("writer", "project-a", created.Branch.ID, repo.SaveInput{ExpectedHeadCommitID: created.Commit.ID, ExpectedGeneration: created.Branch.Generation, IdempotencyKey: "save-after-folder-rename", Changes: []repo.Change{{Operation: "upsert", DocumentID: created.Document.ID, Name: "Guide", RelativePath: "docs/API/guide.md", DocumentType: DocumentTypeMarkdown, Content: "updated\r\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Files[0].RelativePath != "Manual/API/guide.md" {
		t.Fatalf("save restored old path: %+v", saved.Files)
	}
	old, err = scoped.RepositoryContent("reader", "project-a", created.Commit.ID, created.Document.ID)
	if err != nil || old.Document.RelativePath != "docs/API/guide.md" {
		t.Fatal("save rewrote old commit")
	}
}

func TestRenameDocumentFolderSupportsImplicitPathsAndRejectsInvalidMoves(t *testing.T) {
	s := newTask5Store()
	_, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Guide", RelativePath: "Guides/API/guide.md", DocumentType: DocumentTypeMarkdown, Content: "source", IdempotencyKey: "implicit-folder-create"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateDocumentFolder("admin", "project-a", "", "Taken"); err != nil {
		t.Fatal(err)
	}
	before := s.cloneStateLocked()
	for _, tc := range []struct {
		actor, project, path, name string
		want                       error
	}{
		{"writer", "project-a", "Guides", "Denied", ErrPermissionDenied},
		{"reader", "project-a", "Guides", "Denied", ErrPermissionDenied},
		{"reader", "project-b", "Guides", "Denied", ErrPermissionDenied},
		{"admin", "project-a", "Guides", "taken", ErrAlreadyExists},
		{"admin", "project-a", "Missing", "New", ErrNotFound},
		{"admin", "project-a", "Guides", "../bad", ErrInvalidArgument},
		{"admin", "project-a", "Guides", "", ErrInvalidArgument},
		{"admin", "project-a", "", "Root", ErrInvalidArgument},
	} {
		if _, err = s.RenameDocumentFolder(tc.actor, tc.project, tc.path, tc.name); !errors.Is(err, tc.want) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if !reflect.DeepEqual(before, s.cloneStateLocked()) {
		t.Fatal("failed rename changed state")
	}
	result, err := s.RenameDocumentFolder("admin", "project-a", "Guides/API", "Reference")
	if err != nil || result.Path != "Guides/Reference" {
		t.Fatalf("implicit folder: %+v %v", result, err)
	}
	if _, err = s.RenameDocumentFolder("admin", "project-a", "Guides/Reference", "reference"); err != nil {
		t.Fatalf("case-only rename: %v", err)
	}
	s.projects["project-a"].Status = ProjectStatusArchived
	if _, err = s.RenameDocumentFolder("admin", "project-a", "Guides", "Archived"); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("archived project: %v", err)
	}
}

// 此夹具同时保存可变目录和仓库图，故意在最后的审计写入/事务提交处失败。
type folderRenameRepository struct {
	domain.Repository
	state *domain.State
	*repositoryMemory
	failAudit, failCommit bool
}

func (r *folderRenameRepository) LoadState(context.Context) (*domain.State, error) {
	s := NewStore()
	s.applyStateLocked(r.state)
	return s.cloneStateLocked(), nil
}
func (r *folderRenameRepository) WithinTransaction(ctx context.Context, fn func(domain.Repository) error) error {
	state, _ := r.LoadState(ctx)
	tx := &folderRenameRepository{state: state, repositoryMemory: r.repositoryMemory.clone(), failAudit: r.failAudit}
	if err := fn(tx); err != nil {
		return err
	}
	if r.failCommit {
		return errInjectedAuditFailure
	}
	r.state, r.repositoryMemory = tx.state, tx.repositoryMemory
	return nil
}
func (r *folderRenameRepository) UpsertDocumentFolder(_ context.Context, folder *DocumentFolder) error {
	copy := *folder
	r.state.DocumentFolders[folder.ID] = &copy
	return nil
}
func (r *folderRenameRepository) DeleteDocumentFolder(_ context.Context, projectID, folderID string) error {
	if folder := r.state.DocumentFolders[folderID]; folder == nil || folder.ProjectID != projectID {
		return ErrNotFound
	}
	delete(r.state.DocumentFolders, folderID)
	return nil
}
func (r *folderRenameRepository) RecordAudit(_ context.Context, log *AuditLog) error {
	if r.failAudit {
		return errInjectedAuditFailure
	}
	copy := *log
	r.state.AuditLogs[log.ID] = &copy
	return nil
}

func TestRenameDocumentFolderRollsBackStateAndCatalogTogether(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		inferred, commitFailure bool
	}{
		{"inferred/audit_failure", true, false},
		{"inferred/commit_failure", true, true},
		{"persisted/audit_failure", false, false},
		{"persisted/commit_failure", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTask5Store()
			created, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Guide", RelativePath: "Docs/guide.md", DocumentType: DocumentTypeMarkdown, Content: "source", IdempotencyKey: "rollback-folder-create"})
			if err != nil {
				t.Fatal(err)
			}
			folder := s.folderByPathLocked("project-a", "Docs")
			if folder == nil || len(s.documentFolders) != 1 {
				t.Fatal("document creation did not persist its parent folder")
			}
			folderID := folder.ID
			if tc.inferred {
				// 保留旧库仅由 relative_path 推导目录、回滚后仍无目录行的原始覆盖。
				s.documentFolders = map[string]*DocumentFolder{}
			}
			beforeMemory, beforePersisted := s.cloneStateLocked(), s.cloneStateLocked()
			beforeMemoryGraph, beforePersistedGraph := s.repoMemory.clone(), s.repoMemory.clone()
			p := &folderRenameRepository{state: s.cloneStateLocked(), repositoryMemory: s.repoMemory.clone(), failAudit: !tc.commitFailure, failCommit: tc.commitFailure}
			s.persistence = &postgresPersistence{repo: p}
			if renamed, err := s.RenameDocumentFolder("admin", "project-a", "Docs", "Manual"); renamed != nil || !errors.Is(err, errInjectedAuditFailure) {
				t.Fatalf("failed transaction result=%+v err=%v", renamed, err)
			}
			if tc.inferred && (len(s.documentFolders) != 0 || len(p.state.DocumentFolders) != 0) {
				t.Fatal("failed transaction retained newly materialized folders")
			}
			if !reflect.DeepEqual(beforeMemory, s.cloneStateLocked()) || !reflect.DeepEqual(beforePersisted, p.state) || !reflect.DeepEqual(beforePersisted, s.persisted) {
				t.Fatal("failed transaction changed in-memory or persisted state")
			}
			if !reflect.DeepEqual(beforeMemoryGraph, s.repoMemory) || !reflect.DeepEqual(beforePersistedGraph, p.repositoryMemory) {
				t.Fatal("failed transaction changed the repository graph or content")
			}
			d, err := repositoryLoad[repo.Document](context.Background(), p, "project-a", "document", created.Document.ID)
			if err != nil || d.RelativePath != "Docs/guide.md" {
				t.Fatalf("failed transaction changed catalog: %+v %v", d, err)
			}
			p.failAudit, p.failCommit = false, false
			renamed, err := s.RenameDocumentFolder("admin", "project-a", "Docs", "Manual")
			if err != nil || renamed == nil || renamed.Path != "Manual" || (!tc.inferred && renamed.ID != folderID) {
				t.Fatalf("retry changed folder identity or failed: %+v %v", renamed, err)
			}
			d, err = repositoryLoad[repo.Document](context.Background(), p, "project-a", "document", created.Document.ID)
			if err != nil || d.RelativePath != "Manual/guide.md" || len(p.state.DocumentFolders) != 1 || !reflect.DeepEqual(s.documentFolders, p.state.DocumentFolders) {
				t.Fatalf("retry did not synchronize state and catalog: %+v %v", d, err)
			}
			historical, err := s.WithRepositoryDocument(created.Document.ID).RepositoryContent("reader", "project-a", created.Commit.ID, created.Document.ID)
			if err != nil || historical.Document.RelativePath != "Docs/guide.md" || historical.Content != "source" {
				t.Fatalf("rollback/retry changed immutable history: %+v %v", historical, err)
			}
		})
	}
}

func TestMoveDocumentFolderPreservesIDsAndHistory(t *testing.T) {
	s := newTask5Store()
	root, err := s.CreateDocumentFolder("admin", "project-a", "", "Docs")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateDocumentFolder("admin", "project-a", root.ID, "Empty")
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.CreateDocumentFolder("admin", "project-a", "", "Archive")
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Guide", RelativePath: "Docs/guide.md", DocumentType: DocumentTypeMarkdown, Content: "source", IdempotencyKey: "move-folder-create"})
	if err != nil {
		t.Fatal(err)
	}
	parent := target.Path
	moved, err := s.UpdateDocumentFolderLocation("admin", "project-a", "Docs", nil, &parent)
	if err != nil || moved.ID != root.ID || moved.ParentID != target.ID || moved.Path != "Archive/Docs" || s.documentFolders[child.ID].Path != "Archive/Docs/Empty" {
		t.Fatalf("moved folder: %+v %v", moved, err)
	}
	catalog, err := s.RepositoryDocuments("reader", "project-a")
	if err != nil || len(catalog) != 1 || catalog[0].RelativePath != "Archive/Docs/guide.md" {
		t.Fatalf("moved catalog: %+v %v", catalog, err)
	}
	old, err := s.WithRepositoryDocument(created.Document.ID).RepositoryContent("reader", "project-a", created.Commit.ID, created.Document.ID)
	if err != nil || old.Document.RelativePath != "Docs/guide.md" || old.Content != "source" {
		t.Fatalf("historical content: %+v %v", old, err)
	}
	parent = ""
	moved, err = s.UpdateDocumentFolderLocation("admin", "project-a", moved.Path, nil, &parent)
	if err != nil || moved.ID != root.ID || moved.ParentID != "" || moved.Path != "Docs" || s.documentFolders[child.ID].ParentID != root.ID {
		t.Fatalf("move to root: %+v %v", moved, err)
	}
}

func TestMoveDocumentFolderRejectsCyclesCollisionsAndInvalidDestinations(t *testing.T) {
	s := newTask5Store()
	_, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Guide", RelativePath: "Docs/API/guide.md", DocumentType: DocumentTypeMarkdown, Content: "source", IdempotencyKey: "move-invalid-create"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.CreateDocumentFolder("admin", "project-a", "", "Archive")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateDocumentFolder("admin", "project-a", target.ID, "Docs"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		actor, parent string
		want          error
	}{
		{"writer", "Archive", ErrPermissionDenied},
		{"admin", "Docs", ErrInvalidArgument},
		{"admin", "docs/API", ErrInvalidArgument},
		{"admin", "Archive", ErrAlreadyExists},
		{"admin", "Missing", ErrNotFound},
		{"admin", "../bad", ErrInvalidArgument},
	} {
		before := s.cloneStateLocked()
		if _, err = s.UpdateDocumentFolderLocation(tc.actor, "project-a", "Docs", nil, &tc.parent); !errors.Is(err, tc.want) {
			t.Fatalf("%+v: %v", tc, err)
		}
		if !reflect.DeepEqual(before, s.cloneStateLocked()) {
			t.Fatal("rejected move mutated state")
		}
	}
	s.projects["project-a"].Status = ProjectStatusArchived
	parent := ""
	if _, err = s.UpdateDocumentFolderLocation("admin", "project-a", "Docs/API", nil, &parent); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("archived: %v", err)
	}
}

func TestDeleteDocumentFolderOnlyAllowsEmptyDirectories(t *testing.T) {
	s := newTask5Store()
	root, err := s.CreateDocumentFolder("admin", "project-a", "", "Docs")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateDocumentFolder("admin", "project-a", root.ID, "Empty")
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"writer", "reader"} {
		if err = s.DeleteDocumentFolder(actor, "project-a", child.Path); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("%s: %v", actor, err)
		}
	}
	if err = s.DeleteDocumentFolder("admin", "project-a", root.Path); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("nonempty subtree: %v", err)
	}
	if err = s.DeleteDocumentFolder("admin", "project-a", child.Path); err != nil {
		t.Fatal(err)
	}
	if s.documentFolders[child.ID] != nil {
		t.Fatal("empty child still exists")
	}
	if _, err = s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Guide", RelativePath: "Docs/guide.md", DocumentType: DocumentTypeMarkdown, Content: "source", IdempotencyKey: "delete-folder-create"}); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteDocumentFolder("admin", "project-a", root.Path); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("nonempty document: %v", err)
	}
	if err = s.DeleteDocumentFolder("admin", "project-b", root.Path); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other project: %v", err)
	}
	s.projects["project-a"].Status = ProjectStatusArchived
	if err = s.DeleteDocumentFolder("admin", "project-a", root.Path); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("archived: %v", err)
	}
}

func TestDeleteDocumentFolderRollsBackPersistentState(t *testing.T) {
	for _, commitFailure := range []bool{false, true} {
		s := newTask5Store()
		folder, err := s.CreateDocumentFolder("admin", "project-a", "", "Empty")
		if err != nil {
			t.Fatal(err)
		}
		p := &folderRenameRepository{state: s.cloneStateLocked(), repositoryMemory: s.repoMemory.clone(), failAudit: !commitFailure, failCommit: commitFailure}
		s.persistence = &postgresPersistence{repo: p}
		if err = s.DeleteDocumentFolder("admin", "project-a", folder.Path); !errors.Is(err, errInjectedAuditFailure) {
			t.Fatalf("failure=%v: %v", commitFailure, err)
		}
		if s.documentFolders[folder.ID] == nil || p.state.DocumentFolders[folder.ID] == nil {
			t.Fatal("failed delete removed folder")
		}
		p.failAudit, p.failCommit = false, false
		if err = s.DeleteDocumentFolder("admin", "project-a", folder.Path); err != nil {
			t.Fatal(err)
		}
		if p.state.DocumentFolders[folder.ID] != nil || s.documentFolders[folder.ID] != nil {
			t.Fatal("delete retry did not persist")
		}
	}
}
