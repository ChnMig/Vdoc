package vdoc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	domainai "vdoc/domain/ai"
)

type aiProviderFault struct{ Code, Message string }

func (e *aiProviderFault) Error() string { return "AI provider rejected the request" }
func (e *aiProviderFault) Unwrap() error { return ErrFailedPrecondition }

func providerStreamError(data []byte) error {
	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Code    string          `json:"code"`
		Message string          `json:"message"`
	}
	for range 4 {
		if err := json.Unmarshal(data, &envelope); err != nil {
			break
		}
		if len(envelope.Error) == 0 || string(envelope.Error) == "null" {
			break
		}
		data = envelope.Error
		envelope.Error = nil
	}
	return &aiProviderFault{Code: envelope.Code, Message: envelope.Message}
}

var aiSecretPattern = regexp.MustCompile(`(?i)(?:bearer\s+[^\s"'<>]+|sk-[a-z0-9_\-]+|(?:api[_ -]?key|authorization|token|secret)\s*[=:]\s*[^\s,;"'<>]+)`)
var aiRequestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_.:\-]{1,160}$`)

func safeAIErrorText(value, apiKey string) string {
	if apiKey != "" {
		value = strings.ReplaceAll(value, apiKey, "[redacted]")
	}
	value = aiSecretPattern.ReplaceAllString(value, "[redacted]")
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 400 {
		value = string(runes[:400]) + "…"
	}
	return value
}

func aiHTTPError(resp *http.Response, apiKey string) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	fault := providerStreamError(data)
	err := safeAIRequestError(fault, resp.StatusCode, apiKey)
	var detail *domainai.RequestError
	if errors.As(err, &detail) {
		requestID := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("request-id"))
		if aiRequestIDPattern.MatchString(requestID) {
			detail.ProviderRequestID = safeAIErrorText(requestID, apiKey)
		}
		retryAfter := resp.Header.Get("Retry-After")
		seconds, _ := strconv.Atoi(retryAfter)
		if seconds == 0 {
			if until, parseErr := http.ParseTime(retryAfter); parseErr == nil {
				seconds = int(time.Until(until).Seconds()) + 1
			}
		}
		if seconds > 0 && seconds <= 86400 {
			detail.RetryAfterSeconds = seconds
		}
	}
	return err
}

func safeAIRequestError(err error, status int, apiKey string) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var existing *domainai.RequestError
	if errors.As(err, &existing) {
		return err
	}
	if Is(err, ErrPermissionDenied) || Is(err, ErrUnauthenticated) || Is(err, ErrNotFound) || strings.Contains(err.Error(), "became stale") {
		return err
	}
	detail := &domainai.RequestError{Code: "provider_error", Message: "The AI provider could not complete the request. Please retry.", Retryable: true, UpstreamStatus: status, Cause: err}
	var fault *aiProviderFault
	code, message := "", ""
	if errors.As(err, &fault) {
		code, message = strings.ToLower(fault.Code), strings.ToLower(fault.Message)
		detail.ProviderMessage = safeAIErrorText(fault.Message, apiKey)
	}
	text := strings.ToLower(err.Error()) + " " + message
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(text, "timeout") || strings.Contains(text, "timed out"):
		detail.Code, detail.Message = "timeout", "The AI request timed out. Please retry or reduce the reasoning effort."
		if strings.Contains(err.Error(), "AI first response timeout") || strings.Contains(err.Error(), "AI response idle timeout") || strings.Contains(err.Error(), "AI total generation timeout") {
			detail.Message += " (" + safeAIErrorText(err.Error(), apiKey) + ")"
		}
	case code == "insufficient_quota" || strings.Contains(message, "quota") || strings.Contains(message, "credit") || strings.Contains(message, "balance"):
		detail.Code, detail.Message, detail.Retryable = "quota", "The AI provider has insufficient quota. Check the account balance or billing limit.", false
	case status == 401 || code == "invalid_api_key":
		detail.Code, detail.Message, detail.Retryable = "authentication", "The AI provider rejected the API key. Ask an administrator to check the AI settings.", false
	case status == 403:
		detail.Code, detail.Message, detail.Retryable = "permission", "The AI account does not have permission to use this model.", false
	case status == 429 || code == "rate_limit_exceeded":
		detail.Code, detail.Message = "rate_limit", "The AI provider is rate limited. Wait a moment before retrying."
	case code == "context_length_exceeded" || strings.Contains(message, "context length") || strings.Contains(message, "context window"):
		detail.Code, detail.Message, detail.Retryable = "context_limit", "The request exceeds this model's context limit. Start a new conversation or reduce the attached content.", false
	case strings.Contains(message, "reasoning") && (status == 400 || status == 422):
		detail.Code, detail.Message, detail.Retryable = "reasoning_unsupported", "This model does not support the selected reasoning setting. Choose the default setting or another model.", false
	case status == 404 || code == "model_not_found":
		detail.Code, detail.Message, detail.Retryable = "model_configuration", "The AI endpoint or model was not found (provider status 404). Check the Base URL, API mode and model.", false
	case strings.Contains(text, "truncated"):
		detail.Code, detail.Message, detail.Retryable = "output_limit", "The AI output was truncated. Increase the output token limit or reduce the reasoning effort before retrying.", false
	case status >= 500:
		detail.Code, detail.Message = "unavailable", "The AI provider is temporarily unavailable. Please retry later."
	case status >= 400:
		detail.Code, detail.Message, detail.Retryable = "invalid_request", "The AI provider rejected the request. Check the model, attachments and AI settings.", false
	case strings.Contains(text, "stream"):
		detail.Code, detail.Message = "stream_interrupted", "The AI response stream was interrupted. The partial reply has not been saved; please retry."
	case strings.Contains(text, "call ai provider"):
		detail.Code, detail.Message = "network", "Could not connect to the AI provider. Check connectivity and retry."
	}
	if status != 0 && !strings.Contains(detail.Message, "provider status") {
		detail.Message += " (provider status " + strconv.Itoa(status) + ")"
	}
	return detail
}
