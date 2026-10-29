package vdoc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	domainai "vdoc/domain/ai"
)

func TestChatAttachmentPayloadReachesBothProviderEndpoints(t *testing.T) {
	for _, mode := range []string{domainai.ProviderModeChatCompletions, domainai.ProviderModeResponses} {
		t.Run(mode, func(t *testing.T) {
			called := false
			client := &http.Client{Transport: aiRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				called = true
				var payload map[string]any
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				key, imageKind := "messages", "image_url"
				if mode == domainai.ProviderModeResponses {
					key, imageKind = "input", "input_image"
				}
				items := payload[key].([]any)
				content := items[len(items)-1].(map[string]any)["content"].([]any)
				if content[1].(map[string]any)["type"] != imageKind {
					t.Fatal("image input was flattened into text")
				}
				if mode == domainai.ProviderModeResponses {
					return aiJSONResponse(`{"status":"completed","output_text":"Image received"}`), nil
				}
				return aiJSONResponse(`{"choices":[{"message":{"content":"Image received"}}]}`), nil
			})}
			input := aiCompletionRequest{Provider: &AIProviderConfig{BaseURL: testAIProviderBaseURL, APIMode: mode, Model: "test"}, User: "Read this image", Attachments: []domainai.ChatAttachment{{Name: "x.png", MediaType: "image/png", Data: "cGljdHVyZQ=="}}}
			var result aiCompletionResult
			var err error
			if mode == domainai.ProviderModeResponses {
				result, err = callResponses(context.Background(), client, input)
			} else {
				result, err = callChatCompletions(context.Background(), client, input)
			}
			if err != nil || !called || result.Content != "Image received" {
				t.Fatalf("provider request failed: %v", err)
			}
		})
	}
}

func TestChatAttachmentsUseNativeContentPartsInBothProviderModes(t *testing.T) {
	attachments := []domainai.ChatAttachment{
		{Name: "screen.png", MediaType: "image/png", Data: "cGljdHVyZQ=="},
		{Name: "guide.pdf", MediaType: "application/pdf", Data: "cGRm"},
		{Name: "notes.md", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("# Important context"))},
	}
	message := aiMessagePayload{Role: "user", Content: "Explain the attachments", Attachments: attachments}
	for _, responses := range []bool{false, true} {
		parts := message.contentParts(responses)
		if len(parts) != 4 {
			t.Fatalf("missing content parts: %d", len(parts))
		}
		encoded, err := json.Marshal(parts)
		if err != nil || !strings.Contains(string(encoded), "Important context") || !strings.Contains(string(encoded), "data:application/pdf;base64,") || !strings.Contains(string(encoded), "data:image/png;base64,") {
			t.Fatal("attachment data lost")
		}
		if responses {
			if parts[1]["type"] != "input_image" || parts[2]["type"] != "input_file" || parts[2]["filename"] != "guide.pdf" {
				t.Fatal("invalid Responses content shape")
			}
		} else if parts[1]["type"] != "image_url" || parts[2]["type"] != "file" {
			t.Fatal("invalid Chat Completions content shape")
		}
	}
	input := aiCompletionRequest{User: message.Content, Attachments: attachments, History: []aiMessagePayload{{Role: "assistant", Content: "Previous answer"}}}
	payload := responsesPayload{Input: "legacy", MultimodalInput: responsesMultimodalInput(input)}
	encoded, _ := json.Marshal(payload)
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	items, ok := body["input"].([]any)
	if !ok || len(items) != 2 || items[0].(map[string]any)["role"] != "assistant" {
		t.Fatal("Responses history or typed input lost")
	}
}

func TestChatAttachmentsPersistAndRemainAvailableToFollowupQuestions(t *testing.T) {
	store, projectID, documentID, _ := newMarkdownDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "attachment-fixture-key")
	session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: "document", ContextID: documentID})
	if err != nil {
		t.Fatal(err)
	}
	attachment := domainai.ChatAttachment{Name: "notes.txt", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("Exact attachment context"))}
	request, err := store.prepareAIChatRequest("reader", projectID, session.ID, "", attachment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.finishAIChatMessage("reader", projectID, session.ID, request, aiCompletionResult{Content: "Read the file"}, nil); err != nil {
		t.Fatal(err)
	}
	_, messages, err := store.AIChatSession("reader", projectID, session.ID)
	if err != nil || len(messages) != 2 || len(messages[0].Attachments) != 1 {
		t.Fatal("attachment-only message not saved")
	}
	messages[0].Attachments[0].Name = "changed.txt"
	followup, err := store.prepareAIChatRequest("reader", projectID, session.ID, "What did the file say?")
	if err != nil || len(followup.Completion.History) != 2 || len(followup.Completion.History[0].Attachments) != 1 || followup.Completion.History[0].Attachments[0].Name != "notes.txt" {
		t.Fatalf("history attachments lost or mutated: %v", err)
	}
	if _, err := store.prepareAIChatRequest("unknown-user", projectID, session.ID, "read it", attachment); !Is(err, ErrPermissionDenied) {
		t.Fatalf("attachment bypassed permissions: %v", err)
	}
}
