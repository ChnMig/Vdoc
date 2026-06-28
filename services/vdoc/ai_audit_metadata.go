package vdoc

import "strconv"

func addTokenUsageMetadata(metadata map[string]string, usage aiTokenUsage) {
	if usage.PromptTokens > 0 {
		metadata["prompt_tokens"] = strconv.Itoa(usage.PromptTokens)
	}
	if usage.CompletionTokens > 0 {
		metadata["completion_tokens"] = strconv.Itoa(usage.CompletionTokens)
	}
	if usage.InputTokens > 0 {
		metadata["input_tokens"] = strconv.Itoa(usage.InputTokens)
	}
	if usage.OutputTokens > 0 {
		metadata["output_tokens"] = strconv.Itoa(usage.OutputTokens)
	}
	if usage.TotalTokens > 0 {
		metadata["total_tokens"] = strconv.Itoa(usage.TotalTokens)
	}
}
