package vdoc

import (
	"encoding/json"
	"net/http"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestAutomaticChatTitleRetainsReasoningModelCompatibility(t *testing.T) {
	for _, mode := range []string{domainai.ProviderModeChatCompletions, domainai.ProviderModeResponses} {
		for _, effort := range []string{"", "high"} {
			t.Run(mode+"/"+effort, func(t *testing.T) {
				store, projectID, documentID, _ := newMarkdownDocumentFlowStore(t)
				upsertAuditProvider(t, store, mode, "title-wire-fixture-key")
				session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: "document", ContextID: documentID, ReasoningEffort: effort})
				if err != nil {
					t.Fatal(err)
				}
				request, err := store.prepareAIChatRequest("reader", projectID, session.ID, "Explain the document")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.finishAIChatMessage("reader", projectID, session.ID, request, aiCompletionResult{Content: "Saved answer"}, nil); err != nil {
					t.Fatal(err)
				}
				store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					var payload map[string]any
					if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if payload["reasoning"] != nil || payload["reasoning_effort"] != nil || payload["stream"] != false {
						t.Fatalf("title inherited chat effort or streaming: %v", payload)
					}
					if effort != "" {
						if _, present := payload["temperature"]; present {
							t.Fatal("title sent temperature to a reasoning model")
						}
						if mode == domainai.ProviderModeChatCompletions && (payload["max_tokens"] != nil || payload["max_completion_tokens"] == nil) {
							t.Fatalf("title used an incompatible reasoning token limit: %v", payload)
						}
					} else if _, present := payload["temperature"]; !present {
						t.Fatal("default provider sampling changed")
					}
					if mode == domainai.ProviderModeResponses {
						return aiJSONResponse(`{"status":"completed","output_text":"Document explanation"}`), nil
					}
					return aiJSONResponse(`{"choices":[{"message":{"content":"Document explanation"}}]}`), nil
				})})
				if title, err := store.GenerateAIChatTitle("reader", projectID, session.ID); err != nil || title.Title != "Document explanation" {
					t.Fatalf("title generation failed: %+v %v", title, err)
				}
			})
		}
	}
}
