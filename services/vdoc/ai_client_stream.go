package vdoc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SSE 的网络分片不等于事件边界；只在空行后解码完整 data，保留换行及空格。
func readAIEvents(reader io.Reader, handle func(string, []byte) (bool, error)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), aiProviderMaxResponseBytes)
	var event string
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(data) > 0 {
				done, err := handle(event, []byte(strings.Join(data, "\n")))
				if err != nil || done {
					return err
				}
			}
			event, data = "", nil
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read ai stream: %w", err)
	}
	return fmt.Errorf("%w: AI stream ended before completion; retry the request", ErrFailedPrecondition)
}

func emitAIDelta(onDelta func(string) error, delta string) error {
	if onDelta != nil && delta != "" {
		return onDelta(delta)
	}
	return nil
}

func readChatCompletionStream(reader io.Reader, onDelta func(string) error) (aiCompletionResult, error) {
	return readChatCompletionStreamWithReasoning(reader, onDelta, nil)
}

func readChatCompletionStreamWithReasoning(reader io.Reader, onDelta, onReasoning func(string) error) (aiCompletionResult, error) {
	var result aiCompletionResult
	var text strings.Builder
	var reasoning strings.Builder
	var finished bool
	var finishReason string
	err := readAIEvents(reader, func(_ string, data []byte) (bool, error) {
		if string(data) == "[DONE]" {
			if !finished {
				return false, fmt.Errorf("%w: provider did not finish the streamed response", ErrFailedPrecondition)
			}
			return true, nil
		}
		var chunk struct {
			Error   json.RawMessage `json:"error"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content   string `json:"content"`
					Reasoning string `json:"reasoning_content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			return false, fmt.Errorf("%w: invalid AI stream event", ErrFailedPrecondition)
		}
		if chunk.Usage != nil {
			result.Usage = aiTokenUsage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens, TotalTokens: chunk.Usage.TotalTokens}
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return false, providerStreamError(data)
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
			if finished && (choice.Delta.Content != "" || choice.Delta.Reasoning != "") {
				return false, fmt.Errorf("%w: provider sent content after completion", ErrFailedPrecondition)
			}
			text.WriteString(choice.Delta.Content)
			reasoning.WriteString(choice.Delta.Reasoning)
			if err := emitAIDelta(onReasoning, choice.Delta.Reasoning); err != nil {
				return false, err
			}
			if err := emitAIDelta(onDelta, choice.Delta.Content); err != nil {
				return false, err
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				finished, finishReason = true, *choice.FinishReason
			}
		}
		return false, nil
	})
	if err != nil {
		return result, err
	}
	if finishReason == "length" {
		return result, truncatedAIOutputError()
	}
	if finishReason != "stop" {
		return result, fmt.Errorf("%w: provider did not complete a text response", ErrFailedPrecondition)
	}
	result.Content = strings.TrimSpace(text.String())
	result.Reasoning = strings.TrimSpace(reasoning.String())
	if result.Content == "" {
		return result, fmt.Errorf("%w: provider returned empty content", ErrFailedPrecondition)
	}
	return result, nil
}

func readResponsesStream(reader io.Reader, onDelta func(string) error) (aiCompletionResult, error) {
	return readResponsesStreamWithReasoning(reader, onDelta, nil)
}

func readResponsesStreamWithReasoning(reader io.Reader, onDelta, onReasoning func(string) error) (aiCompletionResult, error) {
	var result aiCompletionResult
	var text strings.Builder
	var reasoning strings.Builder
	err := readAIEvents(reader, func(event string, data []byte) (bool, error) {
		var chunk struct {
			Type     string          `json:"type"`
			Delta    string          `json:"delta"`
			Response json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			return false, fmt.Errorf("%w: invalid AI stream event", ErrFailedPrecondition)
		}
		if chunk.Type != "" {
			event = chunk.Type
		}
		switch event {
		case "response.reasoning_summary_text.delta":
			reasoning.WriteString(chunk.Delta)
			return false, emitAIDelta(onReasoning, chunk.Delta)
		case "response.output_text.delta":
			text.WriteString(chunk.Delta)
			return false, emitAIDelta(onDelta, chunk.Delta)
		case "response.completed", "response.incomplete", "response.failed":
			parsed, err := parseResponses(chunk.Response)
			result.Usage = parsed.Usage
			if event != "response.completed" {
				if err == nil {
					err = fmt.Errorf("%w: provider did not complete a text response", ErrFailedPrecondition)
				}
				return false, err
			}
			if err != nil {
				return false, err
			}
			// 最终事件是权威正文；不能把 incomplete 或断流的片段作为成功结果。
			result.Content = parsed.Content
			result.Reasoning = parsed.Reasoning
			if result.Reasoning == "" {
				result.Reasoning = strings.TrimSpace(reasoning.String())
			}
			if reasoning.Len() == 0 && result.Reasoning != "" {
				if err := emitAIDelta(onReasoning, result.Reasoning); err != nil {
					return false, err
				}
			}
			if text.Len() == 0 {
				if err := emitAIDelta(onDelta, parsed.Content); err != nil {
					return false, err
				}
			}
			return true, nil
		case "error":
			return false, providerStreamError(data)
		}
		return false, nil
	})
	return result, err
}
