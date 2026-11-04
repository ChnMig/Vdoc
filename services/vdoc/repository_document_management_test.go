package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	domainai "vdoc/domain/ai"
	repo "vdoc/domain/documentrepo"
	"vdoc/domain/documentshare"
)

func repositoryMetadataChange(file repo.File, content string) repo.Change {
	return repo.Change{Operation: "upsert", DocumentID: file.DocumentID, Name: file.Name, RelativePath: file.RelativePath, DocumentType: file.DocumentType, Description: file.Description, Content: content}
}

func TestRepositoryDocumentManagementPreservesIdentityHistoryAndMetadata(t *testing.T) {
	s := newTask5Store()
	created, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Guide", RelativePath: "guide.md", DocumentType: DocumentTypeMarkdown, Content: "one\r\ntwo\r\n", IdempotencyKey: "metadata-initial"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateDocumentFolderAtPath("admin", "project-a", "", "Manual"); err != nil {
		t.Fatal(err)
	}
	scoped := s.WithRepositoryDocument(created.Document.ID)
	dev := gitBranch(t, scoped, "dev", created.Commit.ID)
	name, target, description := "Renamed guide", "Manual/new.md", "Current description"
	document, err := s.RepositoryUpdateDocument("admin", "project-a", created.Document.ID, RepositoryDocumentPatchInput{Name: &name, RelativePath: &target, Description: &description})
	if err != nil || document.ID != created.Document.ID || document.DefaultBranchID != created.Branch.ID || document.HeadVersionName != "main.1" {
		t.Fatalf("metadata changed identity: %+v %v", document, err)
	}
	branch, err := scoped.RepositoryBranch("reader", "project-a", created.Branch.ID)
	if err != nil || branch.HeadCommitID != created.Commit.ID || branch.Generation != 1 {
		t.Fatalf("metadata changed branch history: %+v %v", branch, err)
	}
	original, err := scoped.RepositoryContent("reader", "project-a", created.Commit.ID, created.Document.ID)
	if err != nil || original.Content != "one\r\ntwo\r\n" || original.Document.RelativePath != "guide.md" || original.Document.Name != "Guide" {
		t.Fatalf("original content changed: %+v %v", original, err)
	}
	main := created.Branch
	saved := gitSave(t, scoped, &main, "metadata-content-save", repositoryMetadataChange(created.Commit.Files[0], "ONE\r\ntwo\r\n"))
	if saved.Files[0].RelativePath != target || saved.Files[0].Name != name || saved.Files[0].Description != description {
		t.Fatalf("body save reverted metadata: %+v", saved.Files[0])
	}
	gitSave(t, scoped, &dev, "metadata-dev-content", repositoryMetadataChange(created.Commit.Files[0], "one\r\nTWO\r\n"))
	request, err := scoped.RepositoryCreateMerge("writer", "project-a", repo.MergeInput{SourceBranchID: dev.ID, TargetBranchID: main.ID})
	if err != nil || len(request.Conflicts) != 0 {
		t.Fatalf("merge request: %+v %v", request, err)
	}
	name, description = "Final name", "Final description"
	if _, err = s.RepositoryUpdateDocument("admin", "project-a", created.Document.ID, RepositoryDocumentPatchInput{Name: &name, Description: &description}); err != nil {
		t.Fatal(err)
	}
	if _, err = scoped.RepositoryApproveMerge("admin", "project-a", request.ID); err != nil {
		t.Fatal(err)
	}
	merged, err := scoped.RepositoryMerge("admin", "project-a", request.ID)
	if err != nil {
		t.Fatal(err)
	}
	document, err = s.RepositoryDocument("reader", "project-a", created.Document.ID)
	if err != nil || document.Name != name || document.Description != description || document.RelativePath != target || document.HeadVersionName != merged.VersionName {
		t.Fatalf("merge reverted metadata: %+v %v", document, err)
	}
	root := "new.md"
	if _, err = s.RepositoryUpdateDocument("admin", "project-a", created.Document.ID, RepositoryDocumentPatchInput{RelativePath: &root}); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteDocumentFolder("admin", "project-a", "Manual"); err != nil {
		t.Fatalf("moved document left old folder nonempty: %v", err)
	}
}

func TestRepositoryDocumentManagementRejectsInvalidAndUnauthorizedMutations(t *testing.T) {
	s := newTask5Store()
	d := createScopedDocument(t, s, "managed", "main")
	other := createScopedDocument(t, s, "other", "main")
	name := "Renamed"
	for _, actor := range []string{"reader", "writer"} {
		if _, err := s.RepositoryUpdateDocument(actor, "project-a", d.Document.ID, RepositoryDocumentPatchInput{Name: &name}); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("%s metadata permission: %v", actor, err)
		}
		if err := s.RepositoryDeleteDocument(actor, "project-a", d.Document.ID); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("%s delete permission: %v", actor, err)
		}
	}
	for _, target := range []string{"", "../escape.md", "unknown/moved.md", other.Document.RelativePath, "OTHER.md", "managed.md/child.md"} {
		if _, err := s.RepositoryUpdateDocument("admin", "project-a", d.Document.ID, RepositoryDocumentPatchInput{Name: &name, RelativePath: &target}); err == nil {
			t.Fatalf("invalid target accepted: %q", target)
		}
		current, err := s.RepositoryDocument("reader", "project-a", d.Document.ID)
		if err != nil || current.Name != d.Document.Name || current.RelativePath != d.Document.RelativePath {
			t.Fatalf("invalid patch partially mutated document: %+v %v", current, err)
		}
	}
	if _, err := s.RepositoryUpdateDocumentWithTypes("admin", "project-a", d.Document.ID, RepositoryDocumentPatchInput{Name: &name}, []int{DocumentTypeOpenAPI}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("metadata type scope: %v", err)
	}
	if err := s.RepositoryDeleteDocumentWithTypes("admin", "project-a", d.Document.ID, []int{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("delete empty type scope: %v", err)
	}
	for _, body := range []string{`{}`, `null`, `{"name":null}`, `{"description":null,"name":"x"}`, `{"status":2}`, `{"name":1}`} {
		var input RepositoryDocumentPatchInput
		if err := json.Unmarshal([]byte(body), &input); err == nil {
			t.Fatalf("invalid JSON patch accepted: %s", body)
		}
	}
}

func TestRepositoryDocumentDeleteDisablesReadsSharesAndAllowsPathReuse(t *testing.T) {
	s := newTask5Store()
	if _, err := s.CreateDocumentFolderAtPath("admin", "project-a", "", "Docs"); err != nil {
		t.Fatal(err)
	}
	created, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Guide", RelativePath: "Docs/guide.md", DocumentType: DocumentTypeMarkdown, Content: "exact\r\noriginal\r\n", IdempotencyKey: "delete-initial-document"})
	if err != nil {
		t.Fatal(err)
	}
	scoped := s.WithRepositoryDocument(created.Document.ID)
	release, err := scoped.RepositoryPublish("admin", "project-a", repo.PublishInput{BranchID: created.Branch.ID, CommitID: created.Commit.ID, ExpectedGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.CreateRepositoryShare("admin", "project-a", release.ID, created.Document.ID, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicRepositoryShareContent(share.Share.ID, share.Secret, "", release.ID); err != nil {
		t.Fatal(err)
	}
	dev := gitBranch(t, scoped, "dev", created.Commit.ID)
	mergeRequest, err := scoped.RepositoryCreateMerge("writer", "project-a", repo.MergeInput{SourceBranchID: dev.ID, TargetBranchID: created.Branch.ID})
	if err != nil {
		t.Fatal(err)
	}
	records, blobs := len(s.repoMemory.records), len(s.repoMemory.blobs)
	if err = s.RepositoryDeleteDocumentWithTypes("admin", "project-a", created.Document.ID, []int{DocumentTypeMarkdown}); err != nil {
		t.Fatal(err)
	}
	if err = s.RepositoryDeleteDocumentWithTypes("admin", "project-a", created.Document.ID, []int{DocumentTypeMarkdown}); err != nil {
		t.Fatalf("delete retry: %v", err)
	}
	if err = s.RepositoryDeleteDocumentWithTypes("admin", "project-a", created.Document.ID, []int{DocumentTypeOpenAPI}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("retry bypassed tombstone type authorization: %v", err)
	}
	if len(s.repoMemory.records) != records || len(s.repoMemory.blobs) != blobs {
		t.Fatal("deletion removed immutable history or content")
	}
	for name, call := range map[string]func() error{
		"document": func() error { _, e := s.RepositoryDocument("reader", "project-a", created.Document.ID); return e },
		"content": func() error {
			_, e := s.RepositoryContent("reader", "project-a", created.Commit.ID, created.Document.ID)
			return e
		},
		"commit":          func() error { _, e := s.RepositoryCommit("reader", "project-a", created.Commit.ID); return e },
		"branch":          func() error { _, e := s.RepositoryBranch("reader", "project-a", created.Branch.ID); return e },
		"scoped branches": func() error { _, e := scoped.RepositoryBranches("reader", "project-a"); return e },
		"history": func() error {
			_, e := s.RepositoryHistory("reader", "project-a", created.Branch.ID, created.Document.ID, "")
			return e
		},
		"compare": func() error {
			_, e := s.RepositoryCompare("reader", "project-a", repo.CompareInput{FromCommitID: created.Commit.ID, ToCommitID: created.Commit.ID})
			return e
		},
		"create branch": func() error {
			_, e := scoped.RepositoryCreateBranch("writer", "project-a", repo.BranchInput{Name: "new", FromCommitID: created.Commit.ID})
			return e
		},
		"publish known branch": func() error {
			_, e := s.RepositoryPublish("admin", "project-a", repo.PublishInput{BranchID: created.Branch.ID, CommitID: created.Commit.ID, ExpectedGeneration: 1})
			return e
		},
		"merge known branches": func() error {
			_, e := s.RepositoryCreateMerge("writer", "project-a", repo.MergeInput{SourceBranchID: dev.ID, TargetBranchID: created.Branch.ID})
			return e
		},
		"approve known merge": func() error {
			_, e := s.RepositoryApproveMerge("admin", "project-a", mergeRequest.ID)
			return e
		},
		"new share": func() error {
			_, e := s.CreateRepositoryShare("admin", "project-a", release.ID, created.Document.ID, repo.ShareInput{ExpiryPreset: documentshare.ExpiryPresetPermanent})
			return e
		},
		"share metadata": func() error { _, e := s.PublicRepositoryShareMetadata(share.Share.ID, share.Secret, ""); return e },
		"share content": func() error {
			_, e := s.PublicRepositoryShareContent(share.Share.ID, share.Secret, "", release.ID)
			return e
		},
		"share download": func() error {
			_, e := s.PublicRepositoryShareDownload(share.Share.ID, share.Secret, "", release.ID)
			return e
		},
		"search ref": func() error {
			_, e := s.RepositorySearchDocuments("reader", "project-a", []int{DocumentTypeMarkdown}, RepositorySearchInput{Query: "original", DocumentID: created.Document.ID, CommitID: created.Commit.ID})
			return e
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("deleted document remains accessible")
			}
		})
	}
	catalog, err := s.RepositoryDocuments("reader", "project-a")
	if err != nil || len(catalog) != 0 {
		t.Fatalf("deleted catalog: %+v %v", catalog, err)
	}
	if err = s.DeleteDocumentFolder("admin", "project-a", "Docs"); err != nil {
		t.Fatalf("tombstone blocked empty-folder deletion: %v", err)
	}
	reused, err := s.RepositoryCreateDocument("writer", "project-a", repo.DocumentInput{Name: "Replacement", RelativePath: "Docs/guide.md", DocumentType: DocumentTypeMarkdown, Content: "replacement", IdempotencyKey: "replacement-document"})
	if err != nil || reused.Document.ID == created.Document.ID {
		t.Fatalf("deleted path cannot be reused: %+v %v", reused, err)
	}
	retained, err := repositoryLoad[repo.Commit](context.Background(), s.repoMemory, "project-a", "commit", created.Commit.ID)
	if err != nil || retained.Files[0].RelativePath != "Docs/guide.md" {
		t.Fatalf("immutable history not retained: %+v %v", retained, err)
	}
	if body, err := s.repositoryFileContent(retained.Files[0]); err != nil || body != "exact\r\noriginal\r\n" {
		t.Fatalf("original raw object not retained: %q %v", body, err)
	}
}

func TestRepositoryFolderMutationChecksAllAffectedDocumentTypesAtomically(t *testing.T) {
	s := newTask5Store()
	for _, input := range []repo.DocumentInput{
		{Name: "Guide", RelativePath: "Mixed/a.md", DocumentType: DocumentTypeMarkdown, Content: "guide", IdempotencyKey: "folder-type-markdown"},
		{Name: "Spec", RelativePath: "Mixed/spec.json", DocumentType: DocumentTypeOpenAPI, Content: "{}", IdempotencyKey: "folder-type-openapi"},
	} {
		if _, err := s.RepositoryCreateDocument("writer", "project-a", input); err != nil {
			t.Fatal(err)
		}
	}
	originalFolder := *s.folderByPathLocked("project-a", "Mixed")
	name := "Renamed"
	if _, err := s.UpdateDocumentFolderLocationWithTypes("admin", "project-a", "Mixed", &name, nil, []int{DocumentTypeMarkdown}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("mixed folder type scope: %v", err)
	}
	catalog, err := s.RepositoryDocuments("reader", "project-a")
	if err != nil || len(catalog) != 2 || catalog[0].RelativePath != "Mixed/a.md" || catalog[1].RelativePath != "Mixed/spec.json" || len(s.documentFolders) != 1 || *s.folderByPathLocked("project-a", "Mixed") != originalFolder {
		t.Fatalf("rejected folder mutation partially persisted: %+v %v", catalog, err)
	}
	if _, err = s.UpdateDocumentFolderLocationWithTypes("admin", "project-a", "Mixed", &name, nil, []int{DocumentTypeMarkdown, DocumentTypeOpenAPI}); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryDeleteDisablesLegacyHistorySharesAndChat(t *testing.T) {
	s, project, document, branch := newMarkdownDocumentFlowStore(t)
	version := publishMarkdownDocumentDraft(t, s, project, document, branch, "1.0.0", markdownV1(), "deleted-legacy-version")
	draft, err := s.CreateMarkdownDraft("writer", project, document, DraftInput{BranchID: branch, VersionName: "1.1.0", SchemaContent: markdownV2()})
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.CreateDocumentShare("admin", project, document, DocumentShareInput{BranchID: branch, VersionScope: DocumentShareScopeAllVersions, ExpiryPreset: documentshare.ExpiryPresetPermanent})
	if err != nil {
		t.Fatal(err)
	}
	target := AISummaryTarget{ProjectID: project, DocumentID: document, OwnerType: domainai.ChatContextDocument, OwnerID: document}
	chat, err := s.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: project, DocumentID: document, ContextType: target.OwnerType, ContextID: document})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RepositoryDeleteDocument("admin", project, document); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"document": func() error { _, e := s.Document("reader", project, document); return e },
		"versions": func() error { _, e := s.ListDocumentVersions("reader", project, document); return e },
		"version":  func() error { _, e := s.Version("reader", project, document, version.ID); return e },
		"version raw": func() error {
			_, e := s.DocumentVersionSchema("reader", project, document, version.ID, "raw")
			return e
		},
		"markdown raw": func() error {
			_, e := s.MarkdownVersionContent("reader", project, document, version.ID, "raw")
			return e
		},
		"draft raw":      func() error { _, e := s.DocumentDraftContent("reader", project, document, draft.ID, "raw"); return e },
		"markdown draft": func() error { _, e := s.MarkdownDraftContent("reader", project, document, draft.ID, "raw"); return e },
		"share metadata": func() error { _, e := s.PublicDocumentShareMetadata(share.Share.ID, share.Secret, ""); return e },
		"share raw": func() error {
			_, e := s.PublicDocumentShareContent(share.Share.ID, share.Secret, "", version.ID)
			return e
		},
		"share download": func() error {
			_, e := s.PublicDocumentShareDownload(share.Share.ID, share.Secret, "", version.ID)
			return e
		},
		"chat list": func() error { _, e := s.ListAIChatSessions("reader", target); return e },
		"chat read": func() error { _, _, e := s.AIChatSession("reader", project, chat.ID); return e },
		"chat create": func() error {
			_, e := s.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: project, DocumentID: document, ContextType: target.OwnerType, ContextID: document})
			return e
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("deleted legacy document remains accessible")
			}
		})
	}
	documents, err := s.ListDocuments("reader", project)
	if err != nil || len(documents) != 0 {
		t.Fatalf("legacy deleted document listed: %+v %v", documents, err)
	}
	if s.versions[version.ID] == nil || s.drafts[draft.ID] == nil || s.aiChats[chat.ID] == nil || s.shares[share.Share.ID] == nil {
		t.Fatal("delete removed retained legacy history")
	}
}

func TestRepositoryMetadataBeforeFirstProjectionKeepsDocumentReadable(t *testing.T) {
	t.Run("project commit then patch and folder move", func(t *testing.T) {
		s, _, work := gitTestRepository(t)
		original := gitSave(t, s, &work, "unconverted-initial", gitChange("unopened", "Docs/guide.md", "original\r\n"))
		name := "Updated before opening"
		if _, err := s.RepositoryUpdateDocument("admin", "project-a", "unopened", RepositoryDocumentPatchInput{Name: &name}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.RenameDocumentFolder("admin", "project-a", "Docs", "Manual"); err != nil {
			t.Fatal(err)
		}
		scoped := s.WithRepositoryDocument("unopened")
		branches, err := scoped.RepositoryBranches("reader", "project-a")
		if err != nil || len(branches) != 1 {
			t.Fatalf("first projection after metadata failed: %+v %v", branches, err)
		}
		current, err := s.RepositoryDocument("reader", "project-a", "unopened")
		if err != nil || current.Name != name || current.RelativePath != "Manual/guide.md" || current.DefaultBranchID != branches[0].ID {
			t.Fatalf("projection lost metadata: %+v %v", current, err)
		}
		gitAssertContent(t, scoped, branches[0].HeadCommitID, "unopened", "original\r\n")
		retained, err := repositoryLoad[repo.Commit](context.Background(), s.repoMemory, "project-a", "commit", original.ID)
		if err != nil || retained.Files[0].RelativePath != "Docs/guide.md" {
			t.Fatalf("old project commit changed: %+v %v", retained, err)
		}
	})
	t.Run("legacy API document then patch", func(t *testing.T) {
		s, project, document, branch := newMarkdownDocumentFlowStore(t)
		publishMarkdownDocumentDraft(t, s, project, document, branch, "1.0.0", markdownV1(), "unopened-legacy-version")
		name, description := "Renamed legacy", "Updated before conversion"
		if _, err := s.RepositoryUpdateDocument("admin", project, document, RepositoryDocumentPatchInput{Name: &name, Description: &description}); err != nil {
			t.Fatal(err)
		}
		branches, err := s.WithRepositoryDocument(document).RepositoryBranches("reader", project)
		if err != nil || len(branches) != 1 {
			t.Fatalf("legacy first projection: %+v %v", branches, err)
		}
		current, err := s.RepositoryDocument("reader", project, document)
		if err != nil || current.Name != name || current.Description != description {
			t.Fatalf("legacy projection lost metadata: %+v %v", current, err)
		}
	})
}

func TestRepositoryDeletionDisablesSavedAIAnalyses(t *testing.T) {
	s := newTask5Store()
	created := createScopedDocument(t, s, "analysis", "main")
	for _, documentID := range []string{"", created.Document.ID} {
		analysis := repo.Analysis{ID: "analysis-" + documentID, ProjectID: "project-a", DocumentID: documentID, ToCommitID: created.Commit.ID, Status: "succeeded", Content: "Original body quoted in a prior answer"}
		if err := putRepositoryAutomation(context.Background(), s.repoMemory, "project-a", "analysis", analysis.ID, analysis, 1, nil); err != nil {
			t.Fatal(err)
		}
	}
	if values, err := s.RepositoryAnalyses("reader", "project-a", 0); err != nil || len(values) != 2 {
		t.Fatalf("active analyses: %+v %v", values, err)
	}
	if err := s.RepositoryDeleteDocument("admin", "project-a", created.Document.ID); err != nil {
		t.Fatal(err)
	}
	for _, documentID := range []string{"", created.Document.ID} {
		if _, err := s.RepositoryAnalysis("reader", "project-a", "analysis-"+documentID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted analysis readable: %v", err)
		}
	}
	if values, err := s.RepositoryAnalyses("reader", "project-a", 0); err != nil || len(values) != 0 {
		t.Fatalf("deleted analyses listed: %+v %v", values, err)
	}
}

func TestRepositoryAnalysisDiscardsCompletionAfterDocumentDeletion(t *testing.T) {
	s, branch, _, target := newRepositoryAutomationFixture(t)
	upsertAuditProvider(t, s, domainai.ProviderModeChatCompletions, "delete-analysis-secret")
	s.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		_ = request.Body.Close()
		if err := s.RepositoryDeleteDocument("admin", branch.ProjectID, target.Files[0].DocumentID); err != nil {
			t.Fatal(err)
		}
		return aiJSONResponse(`{"choices":[{"message":{"content":"Original content copied after deletion"}}]}`), nil
	})})
	if _, err := s.RepositoryAnalyze("reader", branch.ProjectID, repo.AnalysisInput{ToCommitID: target.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted analysis completion returned content: %v", err)
	}
	rows, err := repositoryList[repo.Analysis](context.Background(), s.repoMemory, branch.ProjectID, "analysis")
	if err != nil || len(rows) != 1 || rows[0].Status != "failed" || rows[0].Content != "" {
		t.Fatalf("deleted analysis completion persisted answer: %+v %v", rows, err)
	}
}
