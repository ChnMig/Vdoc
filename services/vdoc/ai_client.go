package vdoc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	domainai "vdoc/domain/ai"
)

type aiCompletionRequest struct {
	Provider        *AIProviderConfig
	APIKey          string
	System          string
	User            string
	History         []aiMessagePayload
	Attachments     []domainai.ChatAttachment
	ReasoningEffort string
	// 辅助任务可省略强度，同时保留思考模型要求的 token 与采样参数格式。
	UseReasoningParameters bool
	OnDelta                func(string) error
}

const aiProviderMaxResponseBytes = 4 << 20

type aiCompletionResult struct {
	Content string
	Usage   aiTokenUsage
}

type aiTokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	InputTokens      int
	OutputTokens     int
	TotalTokens      int
}

func (s *Store) completeAI(ctx context.Context, input aiCompletionRequest) (aiCompletionResult, error) {
	if !input.Provider.StreamingEnabled() {
		input.OnDelta = nil
	}
	client := s.aiHTTP
	if client == nil {
		client = http.DefaultClient
	}
	switch input.Provider.APIMode {
	case domainai.ProviderModeChatCompletions:
		return callChatCompletions(ctx, client, input)
	case domainai.ProviderModeResponses:
		return callResponses(ctx, client, input)
	default:
		return aiCompletionResult{}, fmt.Errorf("%w: unsupported ai api_mode", ErrInvalidArgument)
	}
}

func callChatCompletions(ctx context.Context, client *http.Client, input aiCompletionRequest) (aiCompletionResult, error) {
	messages := make([]aiMessagePayload, 0, len(input.History)+2)
	messages = append(messages, aiMessagePayload{Role: "system", Content: input.System})
	messages = append(messages, input.History...)
	messages = append(messages, aiMessagePayload{Role: "user", Content: input.User, Attachments: input.Attachments})
	payload := chatCompletionPayload{Model: input.Provider.Model, Messages: messages, Temperature: input.Provider.Temperature, MaxTokens: input.Provider.MaxOutputTokens, Stream: input.Provider.StreamingEnabled()}
	if input.ReasoningEffort != "" || input.UseReasoningParameters {
		payload.ReasoningEffort = input.ReasoningEffort
		payload.MaxCompletionTokens, payload.MaxTokens = payload.MaxTokens, 0
		payload.OmitTemperature = true
	}
	if payload.Stream {
		payload.StreamOptions = &chatStreamOptions{IncludeUsage: true}
	}
	return postAI(ctx, client, input.Provider, input.APIKey, "chat/completions", payload, func(reader io.Reader, streaming bool) (aiCompletionResult, error) {
		if streaming {
			return readChatCompletionStream(reader, input.OnDelta)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			return aiCompletionResult{}, err
		}
		return parseChatCompletion(body)
	})
}

func parseChatCompletion(body []byte) (aiCompletionResult, error) {
	var out chatCompletionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return aiCompletionResult{}, fmt.Errorf("parse chat completions response: %w", err)
	}
	// 文本被拒收也可能已消耗 token；仅保留上游明确返回的用量。
	result := aiCompletionResult{Usage: aiTokenUsage{PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens, TotalTokens: out.Usage.TotalTokens}}
	if len(out.Choices) == 0 {
		return result, fmt.Errorf("%w: provider returned no choices", ErrFailedPrecondition)
	}
	switch out.Choices[0].FinishReason {
	case "", "stop": // 部分兼容服务省略完成原因。
	case "length":
		return result, truncatedAIOutputError()
	default:
		return result, fmt.Errorf("%w: provider did not complete a text response", ErrFailedPrecondition)
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content == "" {
		return result, fmt.Errorf("%w: provider returned empty content", ErrFailedPrecondition)
	}
	result.Content = content
	return result, nil
}

func callResponses(ctx context.Context, client *http.Client, input aiCompletionRequest) (aiCompletionResult, error) {
	payload := responsesPayload{Model: input.Provider.Model, Instructions: input.System, Input: responsesConversationInput(input.History, input.User), Temperature: input.Provider.Temperature, MaxOutputTokens: input.Provider.MaxOutputTokens, Store: false, Stream: input.Provider.StreamingEnabled()}
	if input.ReasoningEffort != "" {
		payload.Reasoning = &reasoningPayload{Effort: input.ReasoningEffort}
	}
	payload.OmitTemperature = input.ReasoningEffort != "" || input.UseReasoningParameters
	if hasAIChatAttachments(input) {
		payload.MultimodalInput = responsesMultimodalInput(input)
	}
	return postAI(ctx, client, input.Provider, input.APIKey, "responses", payload, func(reader io.Reader, streaming bool) (aiCompletionResult, error) {
		if streaming {
			return readResponsesStream(reader, input.OnDelta)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			return aiCompletionResult{}, err
		}
		return parseResponses(body)
	})
}

func parseResponses(body []byte) (aiCompletionResult, error) {
	var out responsesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return aiCompletionResult{}, fmt.Errorf("parse responses response: %w", err)
	}
	result := aiCompletionResult{Usage: aiTokenUsage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens, TotalTokens: out.Usage.TotalTokens}}
	if out.Status == "incomplete" {
		if out.IncompleteDetails.Reason == "max_output_tokens" {
			return result, truncatedAIOutputError()
		}
		return result, fmt.Errorf("%w: provider returned incomplete output", ErrFailedPrecondition)
	}
	if out.Status != "" && out.Status != "completed" {
		return result, fmt.Errorf("%w: provider did not complete a text response", ErrFailedPrecondition)
	}
	for _, output := range out.Output {
		if output.Status != "" && output.Status != "completed" {
			return result, fmt.Errorf("%w: provider returned an incomplete output item", ErrFailedPrecondition)
		}
	}
	content := strings.TrimSpace(out.OutputText)
	if content == "" {
		content = strings.TrimSpace(out.JoinedText())
	}
	if content == "" {
		return result, fmt.Errorf("%w: provider returned empty content", ErrFailedPrecondition)
	}
	result.Content = content
	return result, nil
}

func truncatedAIOutputError() error {
	return fmt.Errorf("%w: AI output was truncated; increase the output token limit and retry", ErrFailedPrecondition)
}

func responsesConversationInput(history []aiMessagePayload, currentUser string) string {
	if len(history) == 0 {
		return currentUser
	}
	parts := make([]string, 0, len(history)+1)
	for _, message := range history {
		role := "User"
		if message.Role == domainai.ChatRoleAssistant {
			role = "Assistant"
		}
		parts = append(parts, role+":\n"+message.Content)
	}
	parts = append(parts, "Current user request:\n"+currentUser)
	return strings.Join(parts, "\n\n")
}

func postAI(ctx context.Context, client *http.Client, provider *AIProviderConfig, apiKey, path string, payload any, consume func(io.Reader, bool) (aiCompletionResult, error)) (aiCompletionResult, error) {
	endpoint, err := aiProviderEndpoint(provider.BaseURL, path)
	if err != nil {
		return aiCompletionResult{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return aiCompletionResult{}, fmt.Errorf("marshal ai request: %w", err)
	}
	watch := newAIResponseWatchdog(ctx, provider)
	defer watch.close()
	req, err := http.NewRequestWithContext(watch.ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return aiCompletionResult{}, fmt.Errorf("create ai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.StreamingEnabled() {
		req.Header.Set("Accept", "text/event-stream, application/json")
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	safeClient := *client
	// 总请求时限由 provider 配置和上游 context 决定，避免客户端默认值再次缩短。
	safeClient.Timeout = 0
	safeClient.CheckRedirect = rejectAIProviderRedirect
	resp, err := safeClient.Do(req)
	if err != nil {
		return aiCompletionResult{}, fmt.Errorf("call ai provider: %w", watch.err(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp.StatusCode == http.StatusNotFound {
			return aiCompletionResult{}, fmt.Errorf("%w: provider status 404 (%s); check the Base URL, API mode and model supported by the provider", ErrFailedPrecondition, provider.APIMode)
		}
		return aiCompletionResult{}, fmt.Errorf("%w: provider status %d", ErrFailedPrecondition, resp.StatusCode)
	}
	limited := &io.LimitedReader{R: aiActivityReader{reader: resp.Body, watch: watch}, N: aiProviderMaxResponseBytes + 1}
	reader := bufio.NewReader(limited)
	// 兼容忽略 stream 参数并返回 JSON 的服务，但不把意外的 SSE 当作非流式成功。
	streaming := strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	if !streaming && provider.StreamingEnabled() {
		first, _ := reader.Peek(5)
		streaming = bytes.HasPrefix(first, []byte("data:")) || bytes.HasPrefix(first, []byte("event")) || bytes.HasPrefix(first, []byte(":"))
	}
	result, err := consume(reader, streaming)
	if limited.N <= 0 {
		return aiCompletionResult{Usage: result.Usage}, fmt.Errorf("%w: provider response exceeds %d bytes", ErrFailedPrecondition, aiProviderMaxResponseBytes)
	}
	return result, watch.err(err)
}

type aiMessagePayload struct {
	Role        string                    `json:"role"`
	Content     string                    `json:"content"`
	Attachments []domainai.ChatAttachment `json:"-"`
}

type chatCompletionPayload struct {
	Model               string             `json:"model"`
	Messages            []aiMessagePayload `json:"messages"`
	Temperature         float64            `json:"temperature"`
	MaxTokens           int                `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string             `json:"reasoning_effort,omitempty"`
	OmitTemperature     bool               `json:"-"`
	Stream              bool               `json:"stream"`
	StreamOptions       *chatStreamOptions `json:"stream_options,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatCompletionResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type responsesPayload struct {
	Model           string                    `json:"model"`
	Instructions    string                    `json:"instructions"`
	Input           string                    `json:"input"`
	Temperature     float64                   `json:"temperature"`
	MaxOutputTokens int                       `json:"max_output_tokens"`
	Store           bool                      `json:"store"`
	Stream          bool                      `json:"stream"`
	Reasoning       *reasoningPayload         `json:"reasoning,omitempty"`
	OmitTemperature bool                      `json:"-"`
	MultimodalInput []responsesMessagePayload `json:"-"`
}

type reasoningPayload struct {
	Effort string `json:"effort"`
}

type responsesResponse struct {
	Status            string `json:"status"`
	IncompleteDetails struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Status  string `json:"status"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func (r responsesResponse) JoinedText() string {
	parts := []string{}
	for _, output := range r.Output {
		for _, content := range output.Content {
			if text := strings.TrimSpace(content.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}
