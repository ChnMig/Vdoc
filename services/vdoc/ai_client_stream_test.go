package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAIStreamingModesAndCompletion(t *testing.T) {
	for _, mode := range []string{"responses", "chat_completions"} {
		for _, streaming := range []bool{true, false} {
			t.Run(mode+"/"+map[bool]string{true: "stream", false: "json"}[streaming], func(t *testing.T) {
				store := NewStore()
				store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if payload["stream"] != streaming {
						t.Fatalf("stream = %v", payload["stream"])
					}
					if mode == "chat_completions" && streaming && payload["stream_options"].(map[string]any)["include_usage"] != true {
						t.Fatal("usage not requested")
					}
					if !streaming {
						if _, exists := payload["stream_options"]; exists {
							t.Fatal("non-stream request includes stream_options")
						}
						if mode == "responses" {
							return aiJSONResponse(`{"status":"completed","output_text":"你好\n世界"}`), nil
						}
						return aiJSONResponse(`{"choices":[{"finish_reason":"stop","message":{"content":"你好\n世界"}}]}`), nil
					}
					return aiEventResponse(aiStreamFixture(mode, "stop", true)), nil
				})})
				var deltas []string
				provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "test", APIMode: mode, Stream: &streaming}
				result, err := store.completeAI(context.Background(), aiCompletionRequest{Provider: provider, OnDelta: func(delta string) error { deltas = append(deltas, delta); return nil }})
				if err != nil || result.Content != "你好\n世界" {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				if streaming && (len(deltas) != 2 || strings.Join(deltas, "") != "你好\n世界" || result.Usage.TotalTokens != 9) {
					t.Fatalf("deltas=%v usage=%+v", deltas, result.Usage)
				}
				if !streaming && len(deltas) != 0 {
					t.Fatal("non-stream emitted deltas")
				}
			})
		}
	}
}

func TestAIStreamsRejectIncompleteAndKeepUsage(t *testing.T) {
	for _, mode := range []string{"responses", "chat_completions"} {
		for _, finish := range []string{"length", "disconnect"} {
			t.Run(mode+"/"+finish, func(t *testing.T) {
				var result aiCompletionResult
				var err error
				body := aiStreamFixture(mode, finish, finish != "disconnect")
				if mode == "responses" {
					result, err = readResponsesStream(strings.NewReader(body), nil)
				} else {
					result, err = readChatCompletionStream(strings.NewReader(body), nil)
				}
				if err == nil || result.Content != "" {
					t.Fatalf("incomplete stream accepted: %+v %v", result, err)
				}
				if finish == "length" && result.Usage.TotalTokens != 9 {
					t.Fatalf("lost usage: %+v", result.Usage)
				}
			})
		}
	}
}

func TestAIStreamCallbackCancellation(t *testing.T) {
	for _, mode := range []string{"responses", "chat_completions"} {
		t.Run(mode, func(t *testing.T) {
			callback := func(string) error { return context.Canceled }
			var err error
			if mode == "responses" {
				_, err = readResponsesStream(strings.NewReader(aiStreamFixture(mode, "stop", true)), callback)
			} else {
				_, err = readChatCompletionStream(strings.NewReader(aiStreamFixture(mode, "stop", true)), callback)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

func TestProviderStreamingDefaultsAndExplicitFalseSurviveSave(t *testing.T) {
	store, projectID, _, _ := newOpenAPIDocumentFlowStore(t)
	input := AIProviderInput{BaseURL: testAIProviderBaseURL, Model: "test", APIMode: "responses", APIKey: "synthetic-key", Enabled: true}
	provider, err := store.UpsertSystemAIProvider("super", input)
	if err != nil || !provider.StreamingEnabled() {
		t.Fatalf("default = %+v %v", provider, err)
	}
	no := false
	input.Stream = &no
	provider, err = store.UpsertProjectAIProvider("admin", projectID, input)
	if err != nil || provider.StreamingEnabled() {
		t.Fatalf("explicit false = %+v %v", provider, err)
	}
	input.Stream = nil
	provider, err = store.UpsertProjectAIProvider("admin", projectID, input)
	if err != nil || provider.StreamingEnabled() {
		t.Fatalf("omitted field reset saved false: %+v %v", provider, err)
	}
	provider.Stream = new(bool)
	*provider.Stream = true
	saved, err := store.ProjectAIProvider("admin", projectID)
	if err != nil || saved.StreamingEnabled() {
		t.Fatal("returned config aliases stored stream pointer")
	}
}

func TestSendAIChatStreamDoesNotPersistIncompleteAnswer(t *testing.T) {
	for _, completed := range []bool{true, false} {
		t.Run(map[bool]string{true: "complete", false: "disconnect"}[completed], func(t *testing.T) {
			store, projectID, documentID, _ := newOpenAPIDocumentFlowStore(t)
			_, err := store.UpsertSystemAIProvider("super", AIProviderInput{BaseURL: testAIProviderBaseURL, Model: "test", APIMode: "responses", APIKey: "synthetic-key", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return aiEventResponse(aiStreamFixture("responses", "stop", completed)), nil
			})})
			session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: "document", ContextID: documentID})
			if err != nil {
				t.Fatal(err)
			}
			deltas := ""
			message, err := store.SendAIChatMessageStream("reader", projectID, session.ID, "Explain", func(delta string) error { deltas += delta; return nil })
			if (err == nil) != completed || deltas != "你好\n世界" {
				t.Fatalf("message=%+v delta=%q error=%v", message, deltas, err)
			}
			_, messages, err := store.AIChatSession("reader", projectID, session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if completed && len(messages) != 2 || !completed && len(messages) != 0 {
				t.Fatalf("saved %d messages", len(messages))
			}
		})
	}
}

func aiEventResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func aiStreamFixture(mode, finish string, terminal bool) string {
	if mode == "responses" {
		body := ": heartbeat\r\n\r\nevent: response.output_text.delta\r\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"你好\\n\"}\r\n\r\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"世界\"}\n\n"
		if !terminal {
			return body
		}
		if finish == "length" {
			return body + "event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"},\"usage\":{\"input_tokens\":5,\"output_tokens\":4,\"total_tokens\":9}}}\n\n"
		}
		return body + "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output_text\":\"你好\\n世界\",\"usage\":{\"input_tokens\":5,\"output_tokens\":4,\"total_tokens\":9}}}\n\n"
	}
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\\n\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"世界\"}}]}\n\n"
	if !terminal {
		return body
	}
	return body + "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"" + finish + "\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":4,\"total_tokens\":9}}\n\ndata: [DONE]\n\n"
}
