package vdoc

import (
	"testing"
	"time"

	domainvdoc "vdoc/domain/vdoc"
)

func TestAIModelsMappingRoundTripsProviderTuningFields(t *testing.T) {
	// Given
	now := time.Now().UTC().Truncate(time.Second)
	provider := &domainvdoc.AIProviderConfig{
		ID:               "provider-id",
		Scope:            "system",
		Name:             "OpenAI-compatible",
		BaseURL:          "https://ai.example.test",
		Model:            "gpt-test",
		APIMode:          "chat_completions",
		APIKeyCiphertext: []byte{1, 2, 3},
		CipherKID:        "test-kid",
		APIKeyLast4:      "1234",
		Enabled:          true,
		Temperature:      0,
		TimeoutMS:        45000,
		MaxOutputTokens:  2048,
		CreatedBy:        "admin-id",
		UpdatedBy:        "admin-id",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	// When
	model := aiProviderModelFromDomain(provider)
	loaded := domainAIProviderFromModel(*model)

	// Then
	if model.Temperature != 0 || model.TimeoutMS != 45000 || model.MaxOutputTokens != 2048 {
		t.Fatalf("model tuning = temp %v timeout %d max %d, want 0/45000/2048", model.Temperature, model.TimeoutMS, model.MaxOutputTokens)
	}
	if loaded.Temperature != 0 || loaded.TimeoutMS != 45000 || loaded.MaxOutputTokens != 2048 {
		t.Fatalf("loaded tuning = temp %v timeout %d max %d, want 0/45000/2048", loaded.Temperature, loaded.TimeoutMS, loaded.MaxOutputTokens)
	}
}

func TestAIProviderStreamMappingPreservesExplicitFalse(t *testing.T) {
	for _, configured := range []*bool{nil, boolPointer(false), boolPointer(true)} {
		provider := &domainvdoc.AIProviderConfig{Stream: configured}
		model := aiProviderModelFromDomain(provider)
		loaded := domainAIProviderFromModel(*model)
		if model.Stream != provider.StreamingEnabled() || loaded.StreamingEnabled() != provider.StreamingEnabled() {
			t.Fatal("stream selection changed during persistence mapping")
		}
	}
}

func boolPointer(value bool) *bool { return &value }

func TestAIProviderTimeoutMappingPreservesUnlimited(t *testing.T) {
	zero := 0
	provider := &domainvdoc.AIProviderConfig{TimeoutMS: 0, FirstResponseTimeoutMS: &zero, IdleTimeoutMS: &zero}
	loaded := domainAIProviderFromModel(*aiProviderModelFromDomain(provider))
	if loaded.TimeoutMS != 0 || loaded.FirstResponseTimeout() != 0 || loaded.IdleTimeout() != 0 {
		t.Fatal("unlimited timeout changed during persistence mapping")
	}
}
