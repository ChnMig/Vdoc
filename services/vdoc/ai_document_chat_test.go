package vdoc

import (
	"fmt"
	"strings"
	"testing"
	"time"

	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
)

func TestDocumentChatReadsBranchesPublishedVersionsAndDraftsWithinDocument(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "document-chat-fixture-key")
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "1.0.0", "# Published\nReviewed retry policy", "doc-chat-published")
	branch, err := store.CreateBranch("admin", projectID, documentID, "feature/webhooks", "")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branch.ID, VersionName: "2.0.0-draft", SchemaContent: "# Webhook draft\nUnreviewed webhook signature policy"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateDocument("admin", projectID, "Other document", DocumentTypeMarkdown, "other.md", "")
	if err != nil {
		t.Fatal(err)
	}
	otherBranches, err := store.ListBranches("admin", projectID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateMarkdownDraft("writer", projectID, other.ID, DraftInput{BranchID: otherBranches[0].ID, VersionName: "unrelated", SchemaContent: "PRIVATE_OTHER_DOCUMENT_SENTINEL"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: domainai.ChatContextDocument, ContextID: documentID})
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.prepareAIChatRequest("reader", projectID, session.ID, "Compare all branches and the webhook draft")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{version.ID, draft.ID, "feature/webhooks", "Reviewed retry policy", "Unreviewed webhook signature policy", `"kind":"published"`, `"kind":"draft"`} {
		if !strings.Contains(request.ContextText, want) {
			t.Errorf("document context missing %q", want)
		}
	}
	if strings.Contains(request.ContextText, "PRIVATE_OTHER_DOCUMENT_SENTINEL") || strings.Contains(request.ContextText, other.ID) {
		t.Fatal("another document leaked into chat")
	}
	if !strings.Contains(request.Completion.System, "cite the branch") {
		t.Fatal("document chat lacks source attribution instructions")
	}
	if _, err := store.finishAIChatMessage("reader", projectID, session.ID, request, aiCompletionResult{Content: "Attributed answer"}, nil); err != nil {
		t.Fatal(err)
	}
	_, messages, err := store.AIChatSession("reader", projectID, session.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("chat completion messages=%d err=%v", len(messages), err)
	}
	if _, err := store.ListAIChatSessions("unknown-user", request.Target); !Is(err, ErrPermissionDenied) {
		t.Fatalf("unauthorized document chat: %v", err)
	}
	if _, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: domainai.ChatContextDocument, ContextID: other.ID}); !Is(err, ErrInvalidArgument) {
		t.Fatalf("mismatched document context: %v", err)
	}
	if _, err := store.AISummary("reader", request.Target); !Is(err, ErrInvalidArgument) {
		t.Fatalf("document scope must not become a summary target: %v", err)
	}
}

func TestDocumentChatRejectsChangedSourcesInsideCompletionTransaction(t *testing.T) {
	for _, change := range []string{"draft-edit", "draft-insert", "draft-delete", "version-insert", "branch-rename", "member-remove"} {
		t.Run(change, func(t *testing.T) {
			store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
			upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "document-chat-race-key")
			draft, err := store.CreateMarkdownDraft("writer", projectID, documentID, DraftInput{BranchID: branchID, VersionName: "next", SchemaContent: "# Draft before request"})
			if err != nil {
				t.Fatal(err)
			}
			session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: domainai.ChatContextDocument, ContextID: documentID})
			if err != nil {
				t.Fatal(err)
			}
			request, err := store.prepareAIChatRequest("reader", projectID, session.ID, "Explain this document")
			if err != nil {
				t.Fatal(err)
			}
			repo := &aiCompletionRaceRepository{recordingRepository: &recordingRepository{state: store.cloneStateLocked()}}
			store.persistence = &postgresPersistence{repo: repo}
			store.persisted = store.cloneStateLocked()
			repo.beforeTransaction = func(state *domain.State) {
				switch change {
				case "draft-edit":
					state.Drafts[draft.ID].RawSchemaHash = "new-body"
				case "draft-insert":
					copy := *state.Drafts[draft.ID]
					copy.ID = "inserted-draft"
					state.Drafts[copy.ID] = &copy
				case "draft-delete":
					delete(state.Drafts, draft.ID)
				case "version-insert":
					state.Versions["new-version"] = &ContractVersion{ID: "new-version", ProjectID: projectID, ServiceID: documentID, BranchID: branchID}
				case "branch-rename":
					state.Branches[branchID].Name = "renamed-branch"
				case "member-remove":
					delete(state.Members, memberKey(projectID, "reader"))
				}
			}
			_, err = store.finishAIChatMessage("reader", projectID, session.ID, request, aiCompletionResult{Content: "stale answer"}, nil)
			if !Is(err, ErrFailedPrecondition) {
				t.Fatalf("changed context committed: %v", err)
			}
			if len(repo.state.AIMessages) != 0 || repo.state.AIChats[session.ID].GenerationToken != "" {
				t.Fatal("stale completion persisted messages or retained its reservation")
			}
		})
	}
}

func TestDocumentChatExcerptRetrievesRelevantLongContent(t *testing.T) {
	content := strings.Repeat("前置说明。", 3000) + "webhook-signature-secret" + strings.Repeat("后续说明。", 3000)
	excerpt := aiDocumentExcerpt(content, aiQuestionTerms("webhook-signature-secret"), 3000)
	if !strings.Contains(excerpt, "webhook-signature-secret") || !strings.Contains(excerpt, "other characters omitted") || len([]rune(excerpt)) > 3200 {
		t.Fatalf("invalid bounded excerpt: length=%d", len([]rune(excerpt)))
	}
}

func TestDocumentChatFindsNamedOldSourcesBeyondTheBodyBudget(t *testing.T) {
	store, projectID, documentID, branchID := newMarkdownDocumentFlowStore(t)
	version := publishMarkdownDocumentDraft(t, store, projectID, documentID, branchID, "historical-webhook-v1", "# Webhooks\nHISTORICAL_WEBHOOK_POLICY", "historical-chat-source")
	delete(store.drafts, version.DraftID)
	for i := range 24 {
		copy := *version
		copy.ID, copy.VersionName = fmt.Sprintf("recent-version-%d", i), fmt.Sprintf("release-%d", i)
		copy.NormalizedSchema = "# Unrelated recent release"
		copy.NormalizedSchemaHash = sha(copy.NormalizedSchema)
		copy.NormalizedObjectKey = ""
		copy.PublishedAt = copy.PublishedAt.Add(time.Duration(i+1) * time.Minute)
		store.versions[copy.ID] = &copy
	}
	context, err := store.documentAIChatContextLocked(AISummaryTarget{ProjectID: projectID, DocumentID: documentID, OwnerType: domainai.ChatContextDocument, OwnerID: documentID}, "请解释historical-webhook-v1版本的签名校验")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(context, "HISTORICAL_WEBHOOK_POLICY") || !strings.Contains(context, "RETRIEVED BODIES: 16 of") || !strings.Contains(context, "omitted content is unavailable") {
		t.Fatal("explicit historical source was not retrieved with the body limit disclosed")
	}
}
