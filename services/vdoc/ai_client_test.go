package vdoc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainai "vdoc/domain/ai"
)

func TestAIClient_ParsesChatCompletionsContent_whenProviderUsesChatMode(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("request path=%q auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body chatCompletionPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "gpt-test" || len(body.Messages) != 2 || body.Messages[0].Role != "system" {
			t.Fatalf("chat payload = %+v", body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"chat summary"}}]}`))
	}))
	defer server.Close()
	store := NewStore()
	provider := &AIProviderConfig{BaseURL: server.URL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions}

	// When
	content, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", System: "system", User: "user"})

	// Then
	if err != nil {
		t.Fatalf("completeAI() error = %v", err)
	}
	if content != "chat summary" {
		t.Fatalf("content = %q, want chat summary", content)
	}
}

func TestAIClient_ParsesResponsesOutputContent_whenOutputTextMissing(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("request path=%q", r.URL.Path)
		}
		var body responsesPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Store || !strings.Contains(body.Instructions, "system") || !strings.Contains(body.Input, "user") {
			t.Fatalf("responses payload = %+v", body)
		}
		_, _ = w.Write([]byte(`{"output":[{"content":[{"text":"part one"},{"text":"part two"}]}]}`))
	}))
	defer server.Close()
	store := NewStore()
	provider := &AIProviderConfig{BaseURL: server.URL, Model: "gpt-test", APIMode: domainai.ProviderModeResponses}

	// When
	content, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", System: "system", User: "user"})

	// Then
	if err != nil {
		t.Fatalf("completeAI() error = %v", err)
	}
	if content != "part one\npart two" {
		t.Fatalf("content = %q, want joined output text", content)
	}
}

func TestAIProviderTest_UsesStoredAPIKey_whenInputOmitsKey(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer stored-key" {
			t.Fatalf("auth header = %q, want stored key", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"provider ok"}}]}`))
	}))
	defer server.Close()
	now := time.Now()
	store := NewStore()
	store.users["super"] = &User{ID: "super", Email: "super@example.com", IsSuperAdmin: true, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	_, err := store.UpsertSystemAIProvider("super", AIProviderInput{Name: "stored", BaseURL: server.URL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions, APIKey: "stored-key", Enabled: true})
	if err != nil {
		t.Fatalf("UpsertSystemAIProvider() error = %v", err)
	}

	// When
	content, err := store.TestSystemAIProvider("super", &AIProviderInput{Name: "stored", BaseURL: server.URL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions, Enabled: true})

	// Then
	if err != nil {
		t.Fatalf("TestSystemAIProvider() error = %v", err)
	}
	if content != "provider ok" {
		t.Fatalf("content = %q, want provider ok", content)
	}
}
