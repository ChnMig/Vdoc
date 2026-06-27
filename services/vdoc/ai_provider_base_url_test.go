package vdoc

import (
	"errors"
	"net/http"
	"net/netip"
	"testing"
	"time"

	domainai "vdoc/domain/ai"
)

const (
	testAISuperUserID     = "super"
	testAIProviderBaseURL = "https://ai.example.test"
)

func TestAIProviderStore_UpsertSystemProviderRejectsUnsafeBaseURL_whenURLCanReachInternalNetwork(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "plain http", baseURL: "http://ai.example.test"},
		{name: "localhost", baseURL: "https://localhost:11434"},
		{name: "loopback address", baseURL: "https://127.0.0.1:8443"},
		{name: "link local metadata", baseURL: "https://169.254.169.254"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			store := newAISuperStore(t)

			// When
			_, err := store.UpsertSystemAIProvider(testAISuperUserID, testAIProviderInput(tt.baseURL))

			// Then
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("UpsertSystemAIProvider() error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestAIProviderStore_TestSystemProviderRejectsUnsafeBaseURL_whenInputOverridesProvider(t *testing.T) {
	// Given
	store := newAISuperStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("transport was called for unsafe base_url %q", r.URL.String())
		return nil, nil
	})})
	input := testAIProviderInput("http://127.0.0.1:11434")

	// When
	_, err := store.TestSystemAIProvider(testAISuperUserID, &input)

	// Then
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("TestSystemAIProvider() error = %v, want ErrInvalidArgument", err)
	}
}

func TestAIProviderURL_RejectsResolvedUnsafeAddress_whenDNSReturnsInternalAddress(t *testing.T) {
	// Given
	addrs := []netip.Addr{netip.MustParseAddr("10.0.0.10")}

	// When
	err := validateAIProviderResolvedAddrs("ai.example.test", addrs)

	// Then
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("validateAIProviderResolvedAddrs() error = %v, want ErrInvalidArgument", err)
	}
}

func TestAIProviderURL_AllowsResolvedPublicAddress_whenDNSReturnsPublicAddress(t *testing.T) {
	// Given
	addrs := []netip.Addr{netip.MustParseAddr("93.184.216.34")}

	// When
	err := validateAIProviderResolvedAddrs("ai.example.test", addrs)

	// Then
	if err != nil {
		t.Fatalf("validateAIProviderResolvedAddrs() error = %v, want nil", err)
	}
}

func newAISuperStore(t *testing.T) *Store {
	t.Helper()
	now := time.Now()
	store := NewStore()
	store.users[testAISuperUserID] = &User{ID: testAISuperUserID, Email: "super@example.com", IsSuperAdmin: true, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	return store
}

func testAIProviderInput(baseURL string) AIProviderInput {
	return AIProviderInput{Name: "test", BaseURL: baseURL, Model: "gpt-test", APIMode: domainai.ProviderModeChatCompletions, APIKey: "sk-test-1234", Enabled: true}
}
