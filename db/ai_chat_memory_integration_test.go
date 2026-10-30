package db_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domainai "vdoc/domain/ai"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
	"vdoc/utils/id"
)

func TestPostgresAIChatMemoryAndRegenerationSurviveStoreReload(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx := context.Background()
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	repo := pgvdoc.NewRepository(database)
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, ObjectStorage: &optimizationObjects{bodies: map[string][]byte{}}, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	store := app.DefaultStore()
	store.StopSummaryWorker()
	user, err := store.Register("chat-memory@example.test", "Chat memory", "Chat-memory-fixture-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(user.ID, "Memory team", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Memory project", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := store.CreateDocument(user.ID, project.ID, "Memory document", app.DocumentTypeMarkdown, "memory.md", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertSystemAIProvider(user.ID, app.AIProviderInput{Name: "Memory fixture", BaseURL: "https://api.openai.com", Model: "test", APIMode: domainai.ProviderModeChatCompletions, APIKey: "disposable-memory-key", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateAIChatSession(user.ID, app.AIChatSessionInput{ProjectID: project.ID, DocumentID: document.ID, ContextType: "document", ContextID: document.ID})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Minute)
	for i := 0; i < 10; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		message := &domain.AIChatMessage{ID: id.GenerateID(), SessionID: session.ID, Role: role, Content: "Deadline 2031-09-19. " + strings.Repeat("context ", 300), CreatedAt: now.Add(time.Duration(i) * time.Second)}
		if err := repo.InsertAIChatMessageIfAbsent(ctx, message); err != nil {
			t.Fatal(err)
		}
	}
	client := &http.Client{Transport: aiCompletionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		text := "Deadline 2031-09-19."
		if !strings.Contains(payload.Messages[0].Content, "compact conversation memory") && !strings.Contains(payload.Messages[1].Content, "2031-09-19") {
			t.Fatal("memory not included")
		}
		data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": text, "reasoning_content": "Checked retained facts."}}}})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	})}
	store.SetAIHTTPClient(client)
	answer, err := store.SendAIChatMessage(user.ID, project.ID, session.ID, "What deadline?")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	reloaded := app.DefaultStore()
	reloaded.StopSummaryWorker()
	reloaded.SetAIHTTPClient(client)
	saved, messages, err := reloaded.AIChatSession(user.ID, project.ID, session.ID)
	if err != nil || saved.ContextSummaryCount == 0 || len(messages) != 12 || messages[len(messages)-1].Reasoning != "Checked retained facts." {
		t.Fatalf("saved=%+v len=%d err=%v", saved, len(messages), err)
	}
	replacement, err := reloaded.SendAIChatMessageWithEvents(user.ID, project.ID, session.ID, "", nil, answer.ID, app.AIChatEvents{})
	if err != nil {
		t.Fatal(err)
	}
	_, messages, err = reloaded.AIChatSession(user.ID, project.ID, session.ID)
	if err != nil || len(messages) != 13 || replacement.RetryOf != answer.ID {
		t.Fatalf("replacement=%+v len=%d err=%v", replacement, len(messages), err)
	}
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
}
