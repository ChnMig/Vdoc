package vdoc

import (
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
	Provider    *AIProviderConfig
	APIKey      string
	System      string
	User        string
	Temperature float64
	MaxTokens   int
}

func (s *Store) completeAI(ctx context.Context, input aiCompletionRequest) (string, error) {
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
		return "", fmt.Errorf("%w: unsupported ai api_mode", ErrInvalidArgument)
	}
}

func callChatCompletions(ctx context.Context, client *http.Client, input aiCompletionRequest) (string, error) {
	payload := chatCompletionPayload{Model: input.Provider.Model, Messages: []aiMessagePayload{{Role: "system", Content: input.System}, {Role: "user", Content: input.User}}, Temperature: input.Temperature, MaxTokens: input.MaxTokens}
	body, err := postAIJSON(ctx, client, input.Provider, input.APIKey, "/v1/chat/completions", payload)
	if err != nil {
		return "", err
	}
	var out chatCompletionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse chat completions response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("%w: provider returned no choices", ErrFailedPrecondition)
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("%w: provider returned empty content", ErrFailedPrecondition)
	}
	return content, nil
}

func callResponses(ctx context.Context, client *http.Client, input aiCompletionRequest) (string, error) {
	payload := responsesPayload{Model: input.Provider.Model, Instructions: input.System, Input: input.User, Temperature: input.Temperature, MaxOutputTokens: input.MaxTokens, Store: false}
	body, err := postAIJSON(ctx, client, input.Provider, input.APIKey, "/v1/responses", payload)
	if err != nil {
		return "", err
	}
	var out responsesResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse responses response: %w", err)
	}
	content := strings.TrimSpace(out.OutputText)
	if content == "" {
		content = strings.TrimSpace(out.JoinedText())
	}
	if content == "" {
		return "", fmt.Errorf("%w: provider returned empty content", ErrFailedPrecondition)
	}
	return content, nil
}

func postAIJSON(ctx context.Context, client *http.Client, provider *AIProviderConfig, apiKey, path string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal ai request: %w", err)
	}
	url := strings.TrimRight(provider.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create ai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call ai provider: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ai response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: provider status %d", ErrFailedPrecondition, resp.StatusCode)
	}
	return data, nil
}

type aiMessagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionPayload struct {
	Model       string             `json:"model"`
	Messages    []aiMessagePayload `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type responsesPayload struct {
	Model           string  `json:"model"`
	Instructions    string  `json:"instructions"`
	Input           string  `json:"input"`
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"max_output_tokens,omitempty"`
	Store           bool    `json:"store"`
}

type responsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
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
