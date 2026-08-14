package private

import (
	"encoding/json"
	"io"
	"net/http"
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
	body := `{"name":"fake","base_url":"https://ai.example.test","model":"gpt-test","api_mode":"chat_completions","api_key":"sk-secret-1234","enabled":true,"temperature":0,"timeout_ms":45000,"max_output_tokens":2048}`
	created := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/ai/provider", superToken, body))
	deniedRead := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/ai/provider", writerToken, ""))
	denied := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/projects/"+project.ID+"/ai/provider", writerToken, body))

	// Then
	if created.Code != 200 || strings.Contains(string(created.Detail), "sk-secret") {
		t.Fatalf("provider response = code %d detail %s", created.Code, string(created.Detail))
	}
	var provider struct {
		APIKeySet       bool    `json:"api_key_set"`
		APIKeyLast4     string  `json:"api_key_last4"`
		APIMode         string  `json:"api_mode"`
		Temperature     float64 `json:"temperature"`
		TimeoutMS       int     `json:"timeout_ms"`
		MaxOutputTokens int     `json:"max_output_tokens"`
	}
	if err := json.Unmarshal(created.Detail, &provider); err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	var providerFields map[string]any
	if err := json.Unmarshal(created.Detail, &providerFields); err != nil {
		t.Fatalf("decode provider fields: %v", err)
	}
	if !provider.APIKeySet || provider.APIKeyLast4 != "1234" || provider.APIMode != "chat_completions" || provider.Temperature != 0 || provider.TimeoutMS != 45000 || provider.MaxOutputTokens != 2048 {
		t.Fatalf("provider = %+v", provider)
	}
	if _, ok := providerFields["api_key"]; ok {
		t.Fatalf("provider detail exposes api_key field: %s", string(created.Detail))
	}
	if denied.Code != 403 || denied.Status != "PERMISSION_DENIED" {
		t.Fatalf("writer provider update response = code %d status %q", denied.Code, denied.Status)
	}
	if deniedRead.Code != 403 || deniedRead.Status != "PERMISSION_DENIED" {
		t.Fatalf("writer provider read response = code %d status %q", deniedRead.Code, deniedRead.Status)
	}
}

func TestAIProviderRoutes_ReturnDefaultTuning_whenSystemProviderUnset(t *testing.T) {
	// Given
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("ai-defaults-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	token := issuePrivateTestToken(t, superUser.ID)

	// When
	envelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, "/api/v1/private/ai/provider", token, ""))

	// Then
	if envelope.Code != 200 {
		t.Fatalf("system provider response = code %d detail %s", envelope.Code, string(envelope.Detail))
	}
	var provider struct {
		APIKeySet       bool    `json:"api_key_set"`
		Temperature     float64 `json:"temperature"`
		TimeoutMS       int     `json:"timeout_ms"`
		MaxOutputTokens int     `json:"max_output_tokens"`
	}
	if err := json.Unmarshal(envelope.Detail, &provider); err != nil {
		t.Fatalf("decode provider: %v", err)
	}
	if provider.APIKeySet || provider.Temperature != 0.2 || provider.TimeoutMS != 30000 || provider.MaxOutputTokens != 1000 {
		t.Fatalf("provider defaults = %+v", provider)
	}
}

func TestAIConfigurationRoutes_DenyReaderAndWriterReads_whenPageAIRemainsAvailable(t *testing.T) {
	// Given
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("ai-config-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	readerUser, err := store.CreateUser(superUser.ID, "ai-config-reader@example.com", "Reader", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create reader: %v", err)
	}
	writerUser, err := store.CreateUser(superUser.ID, "ai-config-writer@example.com", "Writer", privateTestPassword, false)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "AI Configuration Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "AI Configuration Project", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := store.AddProjectMember(superUser.ID, project.ID, readerUser.ID, app.MemberRoleReader); err != nil {
		t.Fatalf("add reader: %v", err)
	}
	if _, err := store.AddProjectMember(superUser.ID, project.ID, writerUser.ID, app.MemberRoleWriter); err != nil {
		t.Fatalf("add writer: %v", err)
	}

	// When / Then
	for role, userID := range map[string]string{"reader": readerUser.ID, "writer": writerUser.ID} {
		token := issuePrivateTestToken(t, userID)
		for _, path := range []string{
			"/api/v1/private/projects/" + project.ID + "/ai/provider",
			"/api/v1/private/projects/" + project.ID + "/ai/prompts",
		} {
			envelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, path, token, ""))
			if envelope.Code != 403 || envelope.Status != "PERMISSION_DENIED" {
				t.Fatalf("%s GET %s = code %d status %q detail %s", role, path, envelope.Code, envelope.Status, string(envelope.Detail))
			}
		}
	}
}

func TestProjectAIProviderTest_UsesSystemFallback_whenProjectOverrideIsUnset(t *testing.T) {
	// Given
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	var authorization string
	store.SetAIHTTPClient(&http.Client{Transport: privateAIRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		authorization = r.Header.Get("Authorization")
		return privateAIJSONResponse(`{"choices":[{"message":{"content":"system fallback reached"}}]}`), nil
	})})
	superUser, err := store.Register("ai-fallback-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "AI Fallback Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "AI Fallback Project", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	token := issuePrivateTestToken(t, superUser.ID)
	providerBody := `{"name":"system","base_url":"https://ai.example.test","model":"gpt-system","api_mode":"chat_completions","api_key":"sk-system-fallback","enabled":true}`
	providerEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/ai/provider", token, providerBody))
	if providerEnvelope.Code != 200 {
		t.Fatalf("system provider setup = code %d detail %s", providerEnvelope.Code, string(providerEnvelope.Detail))
	}

	// When
	testEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/projects/"+project.ID+"/ai/provider/test", token, ""))

	// Then
	if testEnvelope.Code != 200 || !strings.Contains(string(testEnvelope.Detail), "system fallback reached") {
		t.Fatalf("project fallback test = code %d detail %s", testEnvelope.Code, string(testEnvelope.Detail))
	}
	if authorization != "Bearer sk-system-fallback" {
		t.Fatalf("fallback Authorization = %q, want stored system key", authorization)
	}
	audit := requirePrivateAudit(t, store.AuditLogsForTest(), "ai.provider.test")
	if audit.ProjectID != project.ID || audit.Metadata["scope"] != "system" {
		t.Fatalf("fallback audit = project %q metadata %+v", audit.ProjectID, audit.Metadata)
	}
}

func TestAIProviderRoutes_ReturnInvalidArgument_whenSystemProviderBaseURLUnsafe(t *testing.T) {
	// Given
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("ai-unsafe-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	token := issuePrivateTestToken(t, superUser.ID)

	// When
	body := `{"name":"unsafe","base_url":"http://127.0.0.1:11434","model":"gpt-test","api_mode":"chat_completions","api_key":"sk-secret-1234","enabled":true}`
	envelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/ai/provider", token, body))

	// Then
	if envelope.Code != 400 || envelope.Status != "INVALID_ARGUMENT" {
		t.Fatalf("unsafe provider response = code %d status %q detail %s", envelope.Code, envelope.Status, string(envelope.Detail))
	}
}

func TestAIProviderRoutes_RecordProviderTestAuditWithRequestContext_whenProviderTestRuns(t *testing.T) {
	// Given
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	store.SetAIHTTPClient(&http.Client{Transport: privateAIRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return privateAIJSONResponse(`{"choices":[{"message":{"content":"route provider ok"}}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`), nil
	})})
	superUser, err := store.Register("ai-route-audit-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	token := issuePrivateTestToken(t, superUser.ID)
	body := `{"name":"fake","base_url":"https://ai.example.test","model":"gpt-test","api_mode":"chat_completions","api_key":"sk-route-secret","enabled":true}`

	// When
	envelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/ai/provider/test", token, body))

	// Then
	if envelope.Code != 200 || !strings.Contains(string(envelope.Detail), "route provider ok") {
		t.Fatalf("provider test response = code %d detail %s", envelope.Code, string(envelope.Detail))
	}
	audit := requirePrivateAudit(t, store.AuditLogsForTest(), "ai.provider.test")
	if audit.RequestID == "" || audit.Metadata["result"] != "success" || audit.Metadata["api_mode"] != "chat_completions" || audit.Metadata["prompt_tokens"] != "4" || audit.Metadata["completion_tokens"] != "3" || audit.Metadata["total_tokens"] != "7" {
		t.Fatalf("provider test audit = %+v", audit)
	}
	if privateAuditContainsValue(store.AuditLogsForTest(), "sk-route-secret") || privateAuditContainsValue(store.AuditLogsForTest(), token) || privateAuditContainsValue(store.AuditLogsForTest(), "Authorization") {
		t.Fatalf("provider test audit leaked secret: %+v", store.AuditLogsForTest())
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

func TestAIPromptRoutes_RejectTemplatesThatDropRequiredContext(t *testing.T) {
	// Given
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	superUser, err := store.Register("prompt-validation-super@example.com", "Super", privateTestPassword)
	if err != nil {
		t.Fatalf("register super: %v", err)
	}
	team, err := store.CreateTeam(superUser.ID, "Prompt Validation Team", "")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	project, err := store.CreateProject(superUser.ID, team.ID, "Prompt Validation Project", "", superUser.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	token := issuePrivateTestToken(t, superUser.ID)

	// When
	missingContext := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/projects/"+project.ID+"/ai/prompts/diff_change_summary", token, `{"system_prompt":"Summarize safely","user_prompt_template":"Ignore the document","enabled":true}`))
	missingMessage := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPut, "/api/v1/private/projects/"+project.ID+"/ai/prompts/page_chat", token, `{"system_prompt":"Answer safely","user_prompt_template":"Use {{context}}","enabled":true}`))

	// Then
	for name, envelope := range map[string]privateTestEnvelope{"missing context": missingContext, "missing message": missingMessage} {
		if envelope.Code != 400 || envelope.Status != "INVALID_ARGUMENT" {
			t.Fatalf("%s response = code %d status %q detail %s", name, envelope.Code, envelope.Status, string(envelope.Detail))
		}
	}
}

func TestAISummaryAndChatRoutes_UseProviderWithinReadablePageScope(t *testing.T) {
	// Given
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	store.SetAIHTTPClient(&http.Client{Transport: privateAIRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		if r.URL.String() != "https://ai.example.test/v1/chat/completions" {
			t.Fatalf("upstream url = %q", r.URL.String())
		}
		return privateAIJSONResponse(`{"choices":[{"message":{"content":"AI explains the scoped change"}}]}`), nil
	})})
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
	providerBody := `{"name":"fake","base_url":"https://ai.example.test","model":"gpt-test","api_mode":"chat_completions","api_key":"sk-test-1234","enabled":true}`
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
	chatHistory := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, "/api/v1/private/projects/"+project.ID+"/ai/chat-sessions?document_id="+document.ID+"&context_type=diff&context_id="+diff.ID, readerToken, ""))

	// Then
	if summary.Code != 200 || !strings.Contains(string(summary.Detail), "AI explains the scoped change") {
		t.Fatalf("summary response = code %d detail %s", summary.Code, string(summary.Detail))
	}
	if chatSession.Code != 200 || message.Code != 200 || !strings.Contains(string(message.Detail), "AI explains the scoped change") {
		t.Fatalf("chat response = session %d message %d detail %s", chatSession.Code, message.Code, string(message.Detail))
	}
	if chatHistory.Code != 200 || chatHistory.Total == nil || *chatHistory.Total != 1 || !strings.Contains(string(chatHistory.Detail), session.ID) {
		t.Fatalf("chat history response = code %d total %v detail %s", chatHistory.Code, chatHistory.Total, string(chatHistory.Detail))
	}
}

type privateAIRoundTripFunc func(*http.Request) (*http.Response, error)

func (f privateAIRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func privateAIJSONResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func requirePrivateAudit(t *testing.T, logs []*app.AuditLog, action string) *app.AuditLog {
	t.Helper()
	for _, audit := range logs {
		if audit.Action == action {
			return audit
		}
	}
	t.Fatalf("missing audit action=%s logs=%+v", action, logs)
	return nil
}

func privateAuditContainsValue(logs []*app.AuditLog, forbidden string) bool {
	for _, audit := range logs {
		for key, value := range audit.Metadata {
			if strings.Contains(key, forbidden) || strings.Contains(value, forbidden) {
				return true
			}
		}
	}
	return false
}
