package vdoc

import (
	"errors"
	"testing"

	repo "vdoc/domain/documentrepo"
)

func assertRepositoryFolderParentChain(t *testing.T, s *Store, paths ...string) map[string]string {
	t.Helper()
	folders, err := s.ListDocumentFolders("reader", "project-a")
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]*DocumentFolder{}
	for _, folder := range folders {
		byPath[folder.Path] = folder
	}
	ids, parentID := map[string]string{}, ""
	for _, path := range paths {
		folder := byPath[path]
		if folder == nil || folder.ID == "" || folder.ParentID != parentID {
			t.Fatalf("folder %q = %+v, want persisted parent %q; all=%+v", path, folder, parentID, byPath)
		}
		ids[path], parentID = folder.ID, folder.ID
	}
	return ids
}

func TestRepositoryLastDocumentMutationPreservesFolderParents(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, operation := range []string{"delete", "move"} {
			name := operation + "/persisted"
			if legacy {
				name = operation + "/inferred"
			}
			t.Run(name, func(t *testing.T) {
				s := newTask5Store()
				created, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Only file", RelativePath: "Source/Nested/Leaf/file.md", DocumentType: DocumentTypeMarkdown, Content: "original\r\n", IdempotencyKey: "folder-parent-persistence"})
				if err != nil {
					t.Fatal(err)
				}
				before := assertRepositoryFolderParentChain(t, s, "Source", "Source/Nested", "Source/Nested/Leaf")
				if legacy {
					// 模拟升级前只有仓库 relative_path、没有实际目录行的旧数据。
					s.documentFolders = map[string]*DocumentFolder{}
				}
				if operation == "delete" {
					err = s.RepositoryDeleteDocument("admin", "project-a", created.Document.ID)
				} else {
					target := "root.md"
					_, err = s.RepositoryUpdateDocument("admin", "project-a", created.Document.ID, RepositoryDocumentPatchInput{RelativePath: &target})
				}
				if err != nil {
					t.Fatal(err)
				}
				after := assertRepositoryFolderParentChain(t, s, "Source", "Source/Nested", "Source/Nested/Leaf")
				if !legacy {
					for path, id := range before {
						if after[path] != id {
							t.Fatalf("folder %s identity changed from %s to %s", path, id, after[path])
						}
					}
				}
				if err = s.DeleteDocumentFolder("admin", "project-a", "Source"); !errors.Is(err, ErrFailedPrecondition) {
					t.Fatalf("parent containing empty child deleted: %v", err)
				}
				for _, path := range []string{"Source/Nested/Leaf", "Source/Nested", "Source"} {
					if err = s.DeleteDocumentFolder("admin", "project-a", path); err != nil {
						t.Fatalf("delete empty folder %s: %v", path, err)
					}
				}
			})
		}
	}
}

func TestRepositoryMovingInferredFolderPreservesSourceAncestors(t *testing.T) {
	s := newTask5Store()
	created, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Only file", RelativePath: "Source/Nested/Leaf/file.md", DocumentType: DocumentTypeMarkdown, Content: "exact body", IdempotencyKey: "inferred-folder-move"})
	if err != nil {
		t.Fatal(err)
	}
	s.documentFolders = map[string]*DocumentFolder{}
	root := ""
	moved, err := s.UpdateDocumentFolderLocation("admin", "project-a", "Source/Nested/Leaf", nil, &root)
	if err != nil || moved.Path != "Leaf" || moved.ParentID != "" {
		t.Fatalf("move inferred folder: %+v, %v", moved, err)
	}
	assertRepositoryFolderParentChain(t, s, "Source", "Source/Nested")
	assertRepositoryFolderParentChain(t, s, "Leaf")
	document, err := s.RepositoryDocument("reader", "project-a", created.Document.ID)
	if err != nil || document.RelativePath != "Leaf/file.md" {
		t.Fatalf("moved document path: %+v, %v", document, err)
	}
	if err = s.DeleteDocumentFolder("admin", "project-a", "Leaf"); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("nonempty moved folder deleted: %v", err)
	}
	if err = s.DeleteDocumentFolder("admin", "project-a", "Source/Nested"); err != nil {
		t.Fatal(err)
	}
}
