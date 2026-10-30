package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	domainai "vdoc/domain/ai"
)

func newResilientChat(t *testing.T, count int) (*Store, string, string) {
	t.Helper()
	store, projectID, documentID, _ := newMarkdownDocumentFlowStore(t)
	upsertAuditProvider(t, store, domainai.ProviderModeChatCompletions, "fixture-resilience-key")
	session, err := store.CreateAIChatSession("reader", AIChatSessionInput{ProjectID: projectID, DocumentID: documentID, ContextType: "document", ContextID: documentID, ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		role := domainai.ChatRoleUser
		if i%2 == 1 {
			role = domainai.ChatRoleAssistant
		}
		message := &AIChatMessage{ID: fmt.Sprintf("history-%02d", i), SessionID: session.ID, Role: role, Content: fmt.Sprintf("message-%d: initial deadline 2031-09-19; ", i) + strings.Repeat("重要事实。", 400), CreatedAt: time.Now().Add(time.Duration(i-count) * time.Second)}
		store.aiMessages[message.ID] = message
	}
	return store, projectID, session.ID
}

func TestAIChatMemoryRetainsEarlyFactsAndAllOriginalRecords(t *testing.T) {
	store, projectID, sessionID := newResilientChat(t, 12)
	var summaryCalls, answerCalls int
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Messages []aiMessagePayload `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(payload.Messages[0].Content, "Update a compact conversation memory") {
			summaryCalls++
			if summaryCalls == 1 && !strings.Contains(payload.Messages[1].Content, "message-0: initial deadline 2031-09-19") {
				t.Fatal("oldest facts missing from summarizer")
			}
			return aiJSONResponse(`{"choices":[{"message":{"content":"Deadline: 2031-09-19; preserve important facts."},"finish_reason":"stop"}]}`), nil
		}
		answerCalls++
		if !strings.Contains(payload.Messages[1].Content, "<conversation_memory>") || !strings.Contains(payload.Messages[1].Content, "2031-09-19") {
			t.Fatal("memory was not supplied to answer")
		}
		if !strings.Contains(payload.Messages[len(payload.Messages)-2].Content, "message-11") {
			t.Fatal("recent original reply missing")
		}
		return aiJSONResponse(`{"choices":[{"message":{"content":"The deadline is 2031-09-19.","reasoning_content":"Checked the conversation memory."},"finish_reason":"stop"}]}`), nil
	})})
	var states []string
	message, err := store.SendAIChatMessageWithEvents("reader", projectID, sessionID, "What deadline did I give?", nil, "", AIChatEvents{OnContext: func(state AIChatContextState) error { states = append(states, state.Status); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	session, messages, _ := store.AIChatSession("reader", projectID, sessionID)
	if summaryCalls == 0 || answerCalls != 1 || len(messages) != 14 || session.ContextSummaryCount != 8 || session.GenerationToken != "" || message.Reasoning == "" {
		t.Fatalf("calls=%d/%d messages=%d session=%+v", summaryCalls, answerCalls, len(messages), session)
	}
	if strings.Join(states, ",") != "compacting,ready" {
		t.Fatal(states)
	}
	// 后续消息直接复用已提交的摘要。
	request, err := store.prepareAIChatRequest("reader", projectID, sessionID, "continue")
	if err != nil || !strings.Contains(request.Completion.History[0].Content, "2031-09-19") {
		t.Fatalf("memory not reused: %v", err)
	}
}

func TestAIChatCompactionFailureAndSupersededRequestPreserveHistory(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprint(stale), func(t *testing.T) {
			store, projectID, sessionID := newResilientChat(t, 10)
			store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if stale {
					if _, err := store.prepareAIChatRequest("reader", projectID, sessionID, "newer request"); err != nil {
						t.Fatal(err)
					}
					return aiJSONResponse(`{"choices":[{"message":{"content":"Stale memory"}}]}`), nil
				}
				return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"temporary failure"}}`))}, nil
			})})
			_, err := store.SendAIChatMessage("reader", projectID, sessionID, "continue")
			if err == nil {
				t.Fatal("failure unexpectedly succeeded")
			}
			session, messages, _ := store.AIChatSession("reader", projectID, sessionID)
			if len(messages) != 10 || session.ContextSummary != "" || (!stale && session.GenerationToken != "") {
				t.Fatalf("failed compaction mutated original state: %+v, %d", session, len(messages))
			}
			if !stale {
				var detail *domainai.RequestError
				if !errors.As(err, &detail) || detail.Code != "context_compaction" || !detail.Retryable {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAIChatRegenerationIsAppendOnlyAndOnlyReplacesLatestAnswer(t *testing.T) {
	store, projectID, sessionID := newResilientChat(t, 4)
	fail := true
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload struct {
			Messages []aiMessagePayload `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if len(payload.Messages) != 4 || strings.Contains(payload.Messages[len(payload.Messages)-1].Content, "message-3") {
			t.Fatalf("replacement sent previous answer: %+v", payload.Messages)
		}
		if fail {
			return nil, fmt.Errorf("network unavailable")
		}
		return aiJSONResponse(`{"choices":[{"message":{"content":"Replacement"}}]}`), nil
	})})
	_, err := store.SendAIChatMessageWithEvents("reader", projectID, sessionID, "", nil, "history-03", AIChatEvents{})
	if err == nil {
		t.Fatal("failed regeneration succeeded")
	}
	_, messages, _ := store.AIChatSession("reader", projectID, sessionID)
	if len(messages) != 4 || messages[3].Content == "" {
		t.Fatal("original answer lost")
	}
	fail = false
	replacement, err := store.SendAIChatMessageWithEvents("reader", projectID, sessionID, "", nil, "history-03", AIChatEvents{})
	if err != nil {
		t.Fatal(err)
	}
	_, messages, _ = store.AIChatSession("reader", projectID, sessionID)
	effective := effectiveAIChatMessages(messages)
	if len(messages) != 5 || len(effective) != 4 || effective[2].ID != "history-02" || effective[3].ID != replacement.ID || replacement.RetryOf != "history-03" {
		t.Fatalf("invalid replacement history: %+v", effective)
	}
	if _, err := store.SendAIChatMessageWithEvents("reader", projectID, sessionID, "", nil, "history-03", AIChatEvents{}); !Is(err, ErrFailedPrecondition) {
		t.Fatal("stale retry accepted", err)
	}
	if _, err := store.SendAIChatMessageWithEvents("unknown", projectID, sessionID, "", nil, replacement.ID, AIChatEvents{}); !Is(err, ErrPermissionDenied) {
		t.Fatal("unauthorized retry", err)
	}
	if _, err := store.SendAIChatMessageWithEvents("reader", projectID, sessionID, "", nil, replacement.ID, AIChatEvents{}); err != nil {
		t.Fatal("retry chain failed", err)
	}
}

func TestAIChatMemoryDoesNotDropOversizeTextSegments(t *testing.T) {
	text := strings.Repeat("文", 25000) + "end-of-file-important-fact"
	sources := aiChatMemorySources([]*AIChatMessage{{Role: "user", Content: text}})
	var joined strings.Builder
	for _, source := range sources {
		joined.WriteString(source.Content)
	}
	if len(sources) != 3 || joined.String() != "user:\n"+text {
		t.Fatal("oversize source was truncated")
	}
}

func TestAIReasoningParsingOnlyExposesProviderVisibleSummary(t *testing.T) {
	data := `{"status":"completed","output":[{"type":"reasoning","encrypted_content":"PRIVATE","content":[{"text":"PRIVATE"}],"summary":[{"type":"summary_text","text":"Visible reasoning summary"},{"type":"encrypted","text":"PRIVATE"}]},{"type":"message","content":[{"type":"output_text","text":"Final answer"}]}]}`
	result, err := parseResponses([]byte(data))
	if err != nil || result.Content != "Final answer" || result.Reasoning != "Visible reasoning summary" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var output, reasoning string
	stream := "event: response.reasoning_summary_text.delta\ndata: {\"delta\":\"Visible reasoning summary\"}\n\nevent: response.output_text.delta\ndata: {\"delta\":\"Final answer\"}\n\nevent: response.completed\ndata: {\"response\":" + data + "}\n\n"
	result, err = readResponsesStreamWithReasoning(strings.NewReader(stream), func(delta string) error { output += delta; return nil }, func(delta string) error { reasoning += delta; return nil })
	if err != nil || reasoning != "Visible reasoning summary" || output != "Final answer" || result.Reasoning != reasoning {
		t.Fatalf("stream result=%+v err=%v", result, err)
	}
	stream = "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"Visible provider reasoning\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"Answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	result, err = readChatCompletionStreamWithReasoning(strings.NewReader(stream), nil, nil)
	if err != nil || result.Content != "Answer" || result.Reasoning != "Visible provider reasoning" {
		t.Fatal(result, err)
	}
}

func TestAIRequestErrorsAreSpecificSafeAndRetryable(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
		want   string
		retry  bool
	}{{401, "invalid_api_key", "authentication", false}, {403, "", "permission", false}, {404, "model_not_found", "model_configuration", false}, {429, "rate_limit_exceeded", "rate_limit", true}, {429, "insufficient_quota", "quota", false}, {400, "context_length_exceeded", "context_limit", false}, {503, "", "unavailable", true}} {
		body := fmt.Sprintf(`{"error":{"code":%q,"message":"key sk-secret-fixture-key Bearer hidden-secret api_key=another-secret"}}`, test.code)
		response := &http.Response{StatusCode: test.status, Header: http.Header{"X-Request-Id": []string{"request-fixture"}, "Retry-After": []string{"12"}}, Body: io.NopCloser(strings.NewReader(body))}
		err := aiHTTPError(response, "sk-secret-fixture-key")
		var detail *domainai.RequestError
		if !errors.As(err, &detail) || detail.Code != test.want || detail.Retryable != test.retry || detail.ProviderRequestID != "request-fixture" || detail.RetryAfterSeconds != 12 {
			t.Fatalf("%d: %+v", test.status, detail)
		}
		encoded, _ := json.Marshal(detail)
		if strings.Contains(string(encoded), "hidden-secret") || strings.Contains(string(encoded), "another-secret") || strings.Contains(string(encoded), "sk-secret") {
			t.Fatal("credential leaked", string(encoded))
		}
	}
	err := safeAIRequestError(context.DeadlineExceeded, 0, "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline cause lost")
	}
}
