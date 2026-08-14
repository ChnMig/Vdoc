package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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
		return aiJSONResponse(`{"choices":[{"message":{"content":"chat summary"}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`), nil
	})})
	provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions}

	// When
	result, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", System: "system", User: "user"})

	// Then
	if err != nil {
		t.Fatalf("completeAI() error = %v", err)
	}
	if result.Content != "chat summary" {
		t.Fatalf("content = %q, want chat summary", result.Content)
	}
	if result.Usage.PromptTokens != 11 || result.Usage.CompletionTokens != 7 || result.Usage.TotalTokens != 18 {
		t.Fatalf("usage = %+v, want chat completion token counts", result.Usage)
	}
}

func TestAIClient_ChatCompletionsPayloadUsesProviderTuning_whenConfigured(t *testing.T) {
	// Given
	store := NewStore()
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 1600*time.Millisecond {
			t.Fatalf("request deadline = %v ok %t, want provider timeout", deadline, ok)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["temperature"] != float64(0) || body["max_tokens"] != float64(77) {
			t.Fatalf("chat payload tuning = temperature %v max_tokens %v, want 0/77", body["temperature"], body["max_tokens"])
		}
		return aiJSONResponse(`{"choices":[{"message":{"content":"chat tuned"}}]}`), nil
	})})
	provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions, Temperature: 0, TimeoutMS: 1500, MaxOutputTokens: 77}

	// When
	result, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", System: "system", User: "user"})

	// Then
	if err != nil {
		t.Fatalf("completeAI() error = %v", err)
	}
	if result.Content != "chat tuned" {
		t.Fatalf("content = %q, want chat tuned", result.Content)
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
		return aiJSONResponse(`{"output":[{"content":[{"text":"part one"},{"text":"part two"}]}],"usage":{"input_tokens":13,"output_tokens":5,"total_tokens":18}}`), nil
	})})
	provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeResponses}

	// When
	result, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", System: "system", User: "user"})

	// Then
	if err != nil {
		t.Fatalf("completeAI() error = %v", err)
	}
	if result.Content != "part one\npart two" {
		t.Fatalf("content = %q, want joined output text", result.Content)
	}
	if result.Usage.InputTokens != 13 || result.Usage.OutputTokens != 5 || result.Usage.TotalTokens != 18 {
		t.Fatalf("usage = %+v, want responses token counts", result.Usage)
	}
}

func TestAIClient_ResponsesPayloadUsesProviderTuning_whenConfigured(t *testing.T) {
	// Given
	store := NewStore()
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["temperature"] != float64(1.25) || body["max_output_tokens"] != float64(2048) {
			t.Fatalf("responses payload tuning = temperature %v max_output_tokens %v, want 1.25/2048", body["temperature"], body["max_output_tokens"])
		}
		return aiJSONResponse(`{"output_text":"responses tuned"}`), nil
	})})
	provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeResponses, Temperature: 1.25, TimeoutMS: 30000, MaxOutputTokens: 2048}

	// When
	result, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", System: "system", User: "user"})

	// Then
	if err != nil {
		t.Fatalf("completeAI() error = %v", err)
	}
	if result.Content != "responses tuned" {
		t.Fatalf("content = %q, want responses tuned", result.Content)
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

func TestAIClient_RejectsProviderRedirectWithoutFollowing(t *testing.T) {
	store := NewStore()
	calls := 0
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://api.example.test/insecure"}},
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Request:    r,
		}, nil
	})})
	provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions}

	_, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", System: "system", User: "user"})
	if calls != 1 {
		t.Fatalf("provider transport calls = %d, want 1 without following redirect", calls)
	}
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("completeAI() redirect error = %v, want failed precondition", err)
	}
}

func TestAIClient_RejectsOversizedProviderResponse(t *testing.T) {
	store := NewStore()
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return aiJSONResponse(strings.Repeat("x", aiProviderMaxResponseBytes+1)), nil
	})})
	provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "gpt-test", APIMode: domainai.ProviderModeResponses}

	_, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "test-key", System: "system", User: "user"})
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("completeAI() oversized response error = %v, want failed precondition", err)
	}
}

type aiRoundTripFunc func(*http.Request) (*http.Response, error)

func (f aiRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func aiJSONResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
