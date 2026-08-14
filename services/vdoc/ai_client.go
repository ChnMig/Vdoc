package vdoc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	domainai "vdoc/domain/ai"
)

type aiCompletionRequest struct {
	Provider *AIProviderConfig
	APIKey   string
	System   string
	User     string
	History  []aiMessagePayload
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
	messages = append(messages, aiMessagePayload{Role: "user", Content: input.User})
	payload := chatCompletionPayload{Model: input.Provider.Model, Messages: messages, Temperature: input.Provider.Temperature, MaxTokens: input.Provider.MaxOutputTokens}
	body, err := postAIJSON(ctx, client, input.Provider, input.APIKey, "/v1/chat/completions", payload)
	if err != nil {
		return aiCompletionResult{}, err
	}
	var out chatCompletionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return aiCompletionResult{}, fmt.Errorf("parse chat completions response: %w", err)
	}
	if len(out.Choices) == 0 {
		return aiCompletionResult{}, fmt.Errorf("%w: provider returned no choices", ErrFailedPrecondition)
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content == "" {
		return aiCompletionResult{}, fmt.Errorf("%w: provider returned empty content", ErrFailedPrecondition)
	}
	return aiCompletionResult{Content: content, Usage: aiTokenUsage{PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens, TotalTokens: out.Usage.TotalTokens}}, nil
}

func callResponses(ctx context.Context, client *http.Client, input aiCompletionRequest) (aiCompletionResult, error) {
	payload := responsesPayload{Model: input.Provider.Model, Instructions: input.System, Input: responsesConversationInput(input.History, input.User), Temperature: input.Provider.Temperature, MaxOutputTokens: input.Provider.MaxOutputTokens, Store: false}
	body, err := postAIJSON(ctx, client, input.Provider, input.APIKey, "/v1/responses", payload)
	if err != nil {
		return aiCompletionResult{}, err
	}
	var out responsesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return aiCompletionResult{}, fmt.Errorf("parse responses response: %w", err)
	}
	content := strings.TrimSpace(out.OutputText)
	if content == "" {
		content = strings.TrimSpace(out.JoinedText())
	}
	if content == "" {
		return aiCompletionResult{}, fmt.Errorf("%w: provider returned empty content", ErrFailedPrecondition)
	}
	return aiCompletionResult{Content: content, Usage: aiTokenUsage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens, TotalTokens: out.Usage.TotalTokens}}, nil
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

func postAIJSON(ctx context.Context, client *http.Client, provider *AIProviderConfig, apiKey, path string, payload any) ([]byte, error) {
	baseURL, err := normalizeAIProviderBaseURL(provider.BaseURL)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal ai request: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(providerTimeoutMS(provider))*time.Millisecond)
	defer cancel()
	url := baseURL + path
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create ai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	safeClient := *client
	safeClient.CheckRedirect = rejectAIProviderRedirect
	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call ai provider: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, aiProviderMaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read ai response: %w", err)
	}
	if len(data) > aiProviderMaxResponseBytes {
		return nil, fmt.Errorf("%w: provider response exceeds %d bytes", ErrFailedPrecondition, aiProviderMaxResponseBytes)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: provider status %d", ErrFailedPrecondition, resp.StatusCode)
	}
	return data, nil
}

func providerTimeoutMS(provider *AIProviderConfig) int {
	if provider.TimeoutMS == 0 {
		return domainai.ProviderDefaultTimeoutMS
	}
	return provider.TimeoutMS
}

type aiMessagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionPayload struct {
	Model       string             `json:"model"`
	Messages    []aiMessagePayload `json:"messages"`
	Temperature float64            `json:"temperature"`
	MaxTokens   int                `json:"max_tokens"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
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
	Model           string  `json:"model"`
	Instructions    string  `json:"instructions"`
	Input           string  `json:"input"`
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	Store           bool    `json:"store"`
}

type responsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
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
