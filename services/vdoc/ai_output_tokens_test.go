package vdoc

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestAIProviderUnlimitedOutputTokensPersistsOnUpdate(t *testing.T) {
	s := newAISuperStore(t)
	input := testAIProviderInput(testAIProviderBaseURL)
	input.MaxOutputTokens = intPtr(0)
	provider, err := s.UpsertSystemAIProvider(testAISuperUserID, input)
	if err != nil || provider.MaxOutputTokens != 0 {
		t.Fatalf("unlimited: %+v %v", provider, err)
	}
	input.MaxOutputTokens = nil
	input.Name = "updated"
	provider, err = s.UpsertSystemAIProvider(testAISuperUserID, input)
	if err != nil || provider.MaxOutputTokens != 0 {
		t.Fatalf("update reset unlimited: %+v %v", provider, err)
	}
	input.MaxOutputTokens = intPtr(2048)
	provider, err = s.UpsertSystemAIProvider(testAISuperUserID, input)
	if err != nil || provider.MaxOutputTokens != 2048 {
		t.Fatalf("custom limit: %+v %v", provider, err)
	}
}

func TestAIClientUnlimitedOutputOmitsAllProviderTokenCaps(t *testing.T) {
	for _, mode := range []string{domainai.ProviderModeResponses, domainai.ProviderModeChatCompletions} {
		for _, reasoning := range []string{"", "high"} {
			for _, stream := range []bool{false, true} {
				t.Run(mode+"/"+reasoning+"/"+map[bool]string{false: "json", true: "stream"}[stream], func(t *testing.T) {
					s := NewStore()
					called := false
					s.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						called = true
						defer r.Body.Close()
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Fatal(err)
						}
						for _, field := range []string{"max_output_tokens", "max_tokens", "max_completion_tokens"} {
							if _, exists := body[field]; exists {
								t.Fatalf("unlimited request contains %s: %v", field, body[field])
							}
						}
						if body["stream"] != stream {
							t.Fatalf("stream = %v", body["stream"])
						}
						if mode == domainai.ProviderModeResponses {
							return aiJSONResponse(`{"output_text":"complete response"}`), nil
						}
						return aiJSONResponse(`{"choices":[{"message":{"content":"complete response"}}]}`), nil
					})})
					provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "test", APIMode: mode, MaxOutputTokens: 0, Stream: &stream}
					result, err := s.completeAI(context.Background(), aiCompletionRequest{Provider: provider, APIKey: "synthetic-key", System: "system", User: "user", ReasoningEffort: reasoning})
					if err != nil || !called || result.Content != "complete response" {
						t.Fatalf("completion: %+v %v called=%v", result, err, called)
					}
				})
			}
		}
	}
}
