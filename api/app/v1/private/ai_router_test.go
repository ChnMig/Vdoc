package private

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	app "vdoc/appstore"
)

func TestAIProviderRoutes_MaskAPIKeyAndEnforcePermissions_whenConfigured(t *testing.T) {
	// Given
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("ai-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	writerUser, err := store.CreateUser(superUser.ID, "ai-writer@example.com", "Writer", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "AI Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "AI Project", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := store.AddProjectMember(superUser.ID, project.ID, writerUser.ID, app.MemberRoleWriter); err != nil {
		t.Fatalf("add writer: %v", err)
	}
	superToken := issuePrivateTestToken(t, superUser.ID)
	writerToken := issuePrivateTestToken(t, writerUser.ID)

	// When
	body := `{"name":"fake","base_url":"https://ai.example.test","model":"gpt-test","api_mode":"chat_completions","api_key":"sk-secret-1234","enabled":true}`
	created := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/ai/provider", superToken, body))
	denied := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/projects/"+project.ID+"/ai/provider", writerToken, body))

	// Then
	if created.Code != 200 || strings.Contains(string(created.Detail), "sk-secret") {
		t.Fatalf("provider response = code %d detail %s", created.Code, string(created.Detail))
	}
	var provider struct {
		APIKeySet   bool   `json:"api_key_set"`
		APIKeyLast4 string `json:"api_key_last4"`
		APIMode     string `json:"api_mode"`
	}
	if err := json.Unmarshal(created.Detail, &provider); err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	if !provider.APIKeySet || provider.APIKeyLast4 != "1234" || provider.APIMode != "chat_completions" {
		t.Fatalf("provider = %+v", provider)
	}
	if denied.Code != 403 || denied.Status != "PERMISSION_DENIED" {
		t.Fatalf("writer provider update response = code %d status %q", denied.Code, denied.Status)
	}
}

func TestAIPromptRoutes_ReturnDefaultsAndProjectOverride_whenAdminOverrides(t *testing.T) {
	// Given
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("prompt-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "Prompt Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "Prompt Project", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	token := issuePrivateTestToken(t, superUser.ID)

	// When
	defaults := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, "/api/v1/private/ai/prompts", token, ""))
	overrideBody := `{"system_prompt":"Project system","user_prompt_template":"Project {{context}}","enabled":true}`
	overridden := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/projects/"+project.ID+"/ai/prompts/diff_change_summary", token, overrideBody))
	projectPrompts := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/ai/prompts", token, ""))

	// Then
	if defaults.Code != 200 || defaults.Total == nil || *defaults.Total != 4 {
		t.Fatalf("defaults response = code %d total %v body %s", defaults.Code, defaults.Total, string(defaults.Detail))
	}
	if overridden.Code != 200 || !strings.Contains(string(projectPrompts.Detail), "Project system") || !strings.Contains(string(projectPrompts.Detail), "AI cannot approve") {
		t.Fatalf("prompt override response = code %d prompts %s", overridden.Code, string(projectPrompts.Detail))
	}
}

func TestAISummaryAndChatRoutes_UseProviderWithinReadablePageScope(t *testing.T) {
	// Given
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"AI explains the scoped change"}}]}`))
	}))
	defer upstream.Close()
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("summary-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	readerUser, err := store.CreateUser(superUser.ID, "summary-reader@example.com", "Reader", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create reader: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "Summary Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "Summary Project", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := store.AddProjectMember(superUser.ID, project.ID, readerUser.ID, app.MemberRoleReader); err != nil {
		t.Fatalf("add reader: %v", err)
	}
	document, err := store.CreateDocument(superUser.ID, project.ID, "summary-api", app.DocumentTypeOpenAPI, "apis/summary.yaml", "")
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	branches, err := store.ListBranches(superUser.ID, project.ID, document.ID)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	from := privatePublishContractVersion(t, superUser.ID, project.ID, document.ID, branches[0].ID, "1.0.0", privateDiffRouteOpenAPI(true))
	to := privatePublishContractVersion(t, superUser.ID, project.ID, document.ID, branches[0].ID, "1.1.0", privateDiffRouteOpenAPI(false))
	diff, err := store.CompareDocumentVersions(superUser.ID, project.ID, document.ID, from.ID, to.ID)
	if err != nil {
		t.Fatalf("compare versions: %v", err)
	}
	superToken := issuePrivateTestToken(t, superUser.ID)
	readerToken := issuePrivateTestToken(t, readerUser.ID)
	providerBody := `{"name":"fake","base_url":"` + upstream.URL + `","model":"gpt-test","api_mode":"chat_completions","api_key":"sk-test-1234","enabled":true}`
	providerEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/ai/provider", superToken, providerBody))
	if providerEnvelope.Code != 200 {
		t.Fatalf("provider setup = code %d body %s", providerEnvelope.Code, string(providerEnvelope.Detail))
	}

	// When
	summaryPath := "/api/v1/private/projects/" + project.ID + "/documents/" + document.ID + "/diffs/" + diff.ID + "/ai-summary/regenerate"
	summary := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, summaryPath, superToken, ""))
	chatCreateBody := `{"document_id":"` + document.ID + `","context_type":"diff","context_id":"` + diff.ID + `","title":"Diff chat"}`
	chatSession := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/ai/chat-sessions", readerToken, chatCreateBody))
	var session app.AIChatSession
	if err := json.Unmarshal(chatSession.Detail, &session); err != nil {
		t.Fatalf("decode chat session: %v", err)
	}
	message := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/ai/chat-sessions/"+session.ID+"/messages", readerToken, `{"content":"What changed?"}`))

	// Then
	if summary.Code != 200 || !strings.Contains(string(summary.Detail), "AI explains the scoped change") {
		t.Fatalf("summary response = code %d detail %s", summary.Code, string(summary.Detail))
	}
	if chatSession.Code != 200 || message.Code != 200 || !strings.Contains(string(message.Detail), "AI explains the scoped change") {
		t.Fatalf("chat response = session %d message %d detail %s", chatSession.Code, message.Code, string(message.Detail))
	}
}
