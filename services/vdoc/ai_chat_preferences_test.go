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

func TestChatReasoningWireContractForBothProviderModes(t *testing.T) {
	for _, mode := range []string{domainai.ProviderModeChatCompletions, domainai.ProviderModeResponses} {
		for _, effort := range []string{"", "none", "high"} {
			t.Run(mode+"/"+effort, func(t *testing.T) {
				client := &http.Client{Transport: aiRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					var payload map[string]any
					if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if effort == "" {
						if payload["reasoning"] != nil || payload["reasoning_effort"] != nil || payload["temperature"] != 0.2 {
							t.Fatalf("default payload changed: %v", payload)
						}
					} else {
						if _, ok := payload["temperature"]; ok {
							t.Fatal("explicit reasoning sent temperature")
						}
						if mode == domainai.ProviderModeResponses {
							if payload["reasoning"].(map[string]any)["effort"] != effort {
								t.Fatal("missing Responses effort")
							}
						} else if payload["reasoning_effort"] != effort || payload["max_tokens"] != nil || payload["max_completion_tokens"] != float64(1200) {
							t.Fatalf("invalid chat reasoning payload: %v", payload)
						}
					}
					if mode == domainai.ProviderModeResponses {
						return aiJSONResponse(`{"status":"completed","output_text":"Answer"}`), nil
					}
					return aiJSONResponse(`{"choices":[{"message":{"content":"Answer"}}]}`), nil
				})}
				input := aiCompletionRequest{Provider: &AIProviderConfig{BaseURL: testAIProviderBaseURL, APIMode: mode, Model: "test", Temperature: 0.2, MaxOutputTokens: 1200}, ReasoningEffort: effort}
				var err error
				if mode == domainai.ProviderModeResponses {
					_, err = callResponses(context.Background(), client, input)
				} else {
					_, err = callChatCompletions(context.Background(), client, input)
				}
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestChatReasoningPersistsAndRejectsChangesDuringGeneration(t *testing.T) {
	store, projectID, documentID, _ := newMarkdownDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "reasoning-fixture-key")
	input := AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: "document", ContextID: documentID}
	input.ReasoningEffort = "unsupported"
	if _, err := store.CreateAIChatSession("reader", input); !Is(err, ErrInvalidArgument) {
		t.Fatal("invalid effort accepted")
	}
	input.ReasoningEffort = ""
	session, err := store.CreateAIChatSession("reader", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateAIChatReasoning("unknown", projectID, session.ID, "high"); !Is(err, ErrPermissionDenied) {
		t.Fatalf("unauthorized update: %v", err)
	}
	if _, err := store.UpdateAIChatReasoning("reader", projectID, session.ID, "high"); err != nil {
		t.Fatal(err)
	}
	request, err := store.prepareAIChatRequest("reader", projectID, session.ID, "Explain")
	if err != nil || request.Completion.ReasoningEffort != "high" {
		t.Fatalf("request effort: %v", err)
	}
	if _, err := store.UpdateAIChatReasoning("reader", projectID, session.ID, "none"); !Is(err, ErrFailedPrecondition) {
		t.Fatalf("changed active generation: %v", err)
	}
	if _, err := store.finishAIChatMessage("reader", projectID, session.ID, request, aiCompletionResult{Content: "Answer"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateAIChatReasoning("reader", projectID, session.ID, ""); err != nil {
		t.Fatal(err)
	}
	loaded, _, _ := store.AIChatSession("reader", projectID, session.ID)
	if loaded.ReasoningEffort != "" {
		t.Fatal("default reset not saved")
	}
}

func TestAutomaticChatTitleIsBoundedIdempotentAndIndependentOfMessages(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failure], func(t *testing.T) {
			store, projectID, documentID, _ := newMarkdownDocumentFlowStore(t)
			upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "title-fixture-key")
			session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: "document", ContextID: documentID, Title: "临时标题", ReasoningEffort: "high"})
			if err != nil {
				t.Fatal(err)
			}
			request, err := store.prepareAIChatRequest("reader", projectID, session.ID, strings.Repeat("问题", 400))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.finishAIChatMessage("reader", projectID, session.ID, request, aiCompletionResult{Content: "保存的回答"}, nil); err != nil {
				t.Fatal(err)
			}
			calls := 0
			store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				body, _ := io.ReadAll(request.Body)
				if len(body) > 12000 || strings.Contains(string(body), "reasoning_effort") || strings.Contains(string(body), `"stream":true`) {
					t.Fatalf("invalid title request: %s", body)
				}
				if failure {
					return nil, errors.New("upstream unavailable")
				}
				return aiJSONResponse(`{"choices":[{"message":{"content":"\"文档问题解析\""}}],"usage":{"total_tokens":12}}`), nil
			})})
			if _, err := store.GenerateAIChatTitle("unknown", projectID, session.ID); !Is(err, ErrPermissionDenied) || calls != 0 {
				t.Fatal("unauthorized title request")
			}
			generated, err := store.GenerateAIChatTitle("reader", projectID, session.ID)
			if failure && err == nil {
				t.Fatal("provider error swallowed")
			}
			if !failure {
				if err != nil || !generated.TitleGenerated || generated.Title != "文档问题解析" {
					t.Fatalf("generated title: %+v %v", generated, err)
				}
				if _, err := store.GenerateAIChatTitle("reader", projectID, session.ID); err != nil || calls != 1 {
					t.Fatal("idempotent title repeated provider call")
				}
			}
			loaded, messages, _ := store.AIChatSession("reader", projectID, session.ID)
			if len(messages) != 2 || loaded.ReasoningEffort != "high" || (failure && (loaded.Title != "临时标题" || loaded.TitleGenerated)) {
				t.Fatal("title request corrupted session or messages")
			}
		})
	}
	if len([]rune(normalizeAIChatTitle(strings.Repeat("题", 100)))) != 80 {
		t.Fatal("title not bounded")
	}
}
