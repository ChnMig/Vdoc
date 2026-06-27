package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestAIClient_ParsesChatCompletionsContent_whenProviderUsesChatMode(t *testing.T) {
	// Given
	store := NewStore()
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		if r.URL.String() != testAIProviderBaseURL+"/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("request url=%q auth=%q", r.URL.String(), r.Header.Get("Authorization"))
		}
		var body chatCompletionPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "gpt-test" || len(body.Messages) != 2 || body.Messages[0].Role != "system" {
			t.Fatalf("chat payload = %+v", body)
		}
		return aiJSONResponse(`{"choices":[{"message":{"content":"chat summary"}}]}`), nil
	})})
	provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions}

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
	store := NewStore()
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		if r.URL.String() != testAIProviderBaseURL+"/v1/responses" {
			t.Fatalf("request url=%q", r.URL.String())
		}
		var body responsesPayload
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Store || !strings.Contains(body.Instructions, "system") || !strings.Contains(body.Input, "user") {
			t.Fatalf("responses payload = %+v", body)
		}
		return aiJSONResponse(`{"output":[{"content":[{"text":"part one"},{"text":"part two"}]}]}`), nil
	})})
	provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeResponses}

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
	store := newAISuperStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		if r.Header.Get("Authorization") != "Bearer stored-key" {
			t.Fatalf("auth header = %q, want stored key", r.Header.Get("Authorization"))
		}
		return aiJSONResponse(`{"choices":[{"message":{"content":"provider ok"}}]}`), nil
	})})
	_, err := store.UpsertSystemAIProvider(testAISuperUserID, AIProviderInput{Name: "stored", BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions, APIKey: "stored-key", Enabled: true})
	if err != nil {
		t.Fatalf("UpsertSystemAIProvider() error = %v", err)
	}

	// When
	content, err := store.TestSystemAIProvider(testAISuperUserID, &AIProviderInput{Name: "stored", BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions, Enabled: true})

	// Then
	if err != nil {
		t.Fatalf("TestSystemAIProvider() error = %v", err)
	}
	if content != "provider ok" {
		t.Fatalf("content = %q, want provider ok", content)
	}
}

func TestAIClient_RejectsUnsafeBaseURLBeforeTransport_whenProviderTargetsLoopback(t *testing.T) {
	// Given
	called := false
	store := NewStore()
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return aiJSONResponse(`{"choices":[{"message":{"content":"should not call"}}]}`), nil
	})})
	provider := &AIProviderConfig{BaseURL: "http://127.0.0.1:11434", Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions}

	// When
	_, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", System: "system", User: "user"})

	// Then
	if called {
		t.Fatalf("transport was called for unsafe base_url")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("completeAI() error = %v, want ErrInvalidArgument", err)
	}
}

type aiRoundTripFunc func(*http.Request) (*http.Response, error)

func (f aiRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func aiJSONResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
