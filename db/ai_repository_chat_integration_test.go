package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domainai "vdoc/domain/ai"
	repo "vdoc/domain/documentrepo"
	app "vdoc/services/vdoc"
)

func TestPostgresRepositoryDocumentChatPersistsAndRejectsChangedSources(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx := context.Background()
	// 先用 beta.3 的原始迁移建立旧库，创建既有会话后再应用新迁移。
	migrations, err := databasepkg.EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Exec(`CREATE TABLE schema_migrations (version text PRIMARY KEY, name text NOT NULL, checksum text, applied_at timestamptz NOT NULL DEFAULT now())`).Error; err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version >= "022" {
			continue
		}
		if err = database.Exec(migration.SQL).Error; err != nil {
			t.Fatal(err)
		}
		if err = database.Exec(`INSERT INTO schema_migrations(version,name,checksum) VALUES(?,?,?)`, migration.Version, migration.Name, migration.Checksum).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: pgvdoc.NewRepository(database), ObjectStorage: &optimizationObjects{bodies: map[string][]byte{}}, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	s := app.DefaultStore()
	s.StopSummaryWorker()
	u, err := s.Register("repository-chat@example.test", "Chat owner", "Repository-chat-fixture-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.CreateTeam(u.ID, "Chat team", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(u.ID, team.ID, "Chat project", "", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.RepositoryCreateDocument(u.ID, project.ID, repo.DocumentInput{Name: "New guide", RelativePath: "guide.md", DocumentType: 2, Content: "# New repository content", IdempotencyKey: "repository-chat-document"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpsertSystemAIProvider(u.ID, app.AIProviderInput{Name: "Fixture", BaseURL: "https://api.openai.com", Model: "test", APIMode: domainai.ProviderModeChatCompletions, APIKey: "disposable-test-key", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.CreateDocument(u.ID, project.ID, "Legacy guide", app.DocumentTypeMarkdown, "legacy.md", "")
	if err != nil {
		t.Fatal(err)
	}
	oldSession, err := s.CreateAIChatSession(u.ID, app.AIChatSessionInput{ProjectID: project.ID, DocumentID: legacy.ID, ContextType: "document", ContextID: legacy.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AIChatSession(u.ID, project.ID, oldSession.ID); err != nil {
		t.Fatalf("upgrade lost old conversation: %v", err)
	}
	anotherLegacy, err := s.CreateDocument(u.ID, project.ID, "Legacy after migration", app.DocumentTypeMarkdown, "legacy-after.md", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateAIChatSession(u.ID, app.AIChatSessionInput{ProjectID: project.ID, DocumentID: anotherLegacy.ID, ContextType: "document", ContextID: anotherLegacy.ID}); err != nil {
		t.Fatal(err)
	}
	session, err := s.CreateAIChatSession(u.ID, app.AIChatSessionInput{ProjectID: project.ID, DocumentID: d.Document.ID, ContextType: "document", ContextID: d.Document.ID})
	if err != nil {
		t.Fatal(err)
	}
	mutate := false
	client := &http.Client{Transport: aiCompletionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "New repository content") || !strings.Contains(string(body), "main.1") {
			t.Fatal("repository content missing from provider request")
		}
		if mutate {
			_, err = s.WithRepositoryDocument(d.Document.ID).RepositorySave(u.ID, project.ID, d.Branch.ID, repo.SaveInput{ExpectedHeadCommitID: d.Commit.ID, ExpectedGeneration: d.Branch.Generation, IdempotencyKey: "chat-concurrent-save", Changes: []repo.Change{{Operation: "upsert", DocumentID: d.Document.ID, Name: "New guide", RelativePath: "guide.md", DocumentType: 2, Content: "# Changed while answering"}}})
			if err != nil {
				t.Fatal(err)
			}
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": "Answer grounded in main.1"}}}})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	})}
	s.SetAIHTTPClient(client)
	if _, err = s.SendAIChatMessage(u.ID, project.ID, session.ID, "Explain main.1"); err != nil {
		t.Fatal(err)
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	if err = app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	s = app.DefaultStore()
	s.StopSummaryWorker()
	s.SetAIHTTPClient(client)
	sessions, err := s.ListAIChatSessions(u.ID, app.AISummaryTarget{ProjectID: project.ID, DocumentID: d.Document.ID, OwnerType: "document", OwnerID: d.Document.ID})
	if err != nil || len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("sessions after restart: %+v %v", sessions, err)
	}
	_, messages, err := s.AIChatSession(u.ID, project.ID, session.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages after restart: %d %v", len(messages), err)
	}
	mutate = true
	if _, err = s.SendAIChatMessage(u.ID, project.ID, session.ID, "Explain again"); !errors.Is(err, app.ErrFailedPrecondition) {
		t.Fatalf("stale completion accepted: %v", err)
	}
	_, messages, err = s.AIChatSession(u.ID, project.ID, session.ID)
	if err != nil || len(messages) != 2 {
		t.Fatalf("stale answer was persisted: %d %v", len(messages), err)
	}
}
