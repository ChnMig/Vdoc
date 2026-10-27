package vdoc

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAIProviderModelsUseSavedOrUnsavedCredentialsWithoutSaving(t *testing.T) {
	for _, saved := range []bool{false, true} {
		for _, suffix := range []string{"", "/v1"} {
			t.Run(suffix+map[bool]string{false: "unsaved", true: "saved"}[saved], func(t *testing.T) {
				store := newAISuperStore(t)
				baseURL := testAIProviderBaseURL + suffix
				if saved {
					if _, err := store.UpsertSystemAIProvider("super", testAIProviderInput(baseURL)); err != nil {
						t.Fatal(err)
					}
				}
				store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.Method != http.MethodGet || r.URL.String() != testAIProviderBaseURL+"/v1/models" || r.Header.Get("Authorization") != "Bearer sk-test-1234" {
						t.Fatal("model request did not use the intended endpoint and credential")
					}
					return aiJSONResponse(`{"data":[{"id":"z-model"},{"id":" a-model "},{"id":"z-model"}]}`), nil
				})})
				var input *AIProviderInput
				if !saved {
					input = &AIProviderInput{BaseURL: baseURL, APIKey: "sk-test-1234"}
				}
				models, err := store.ListSystemAIProviderModels("super", input)
				if err != nil || !reflect.DeepEqual(models, []string{"a-model", "z-model"}) {
					t.Fatalf("models=%v err=%v", models, err)
				}
				provider, err := store.SystemAIProvider("super")
				if err != nil || (provider != nil) != saved {
					t.Fatal("model discovery changed the saved provider")
				}
			})
		}
	}
}

func TestAIProviderModelsDoNotForwardSavedKeyToChangedURL(t *testing.T) {
	store := newAISuperStore(t)
	if _, err := store.UpsertSystemAIProvider("super", testAIProviderInput(testAIProviderBaseURL)); err != nil {
		t.Fatal(err)
	}
	calls := 0
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer replacement-key" {
			t.Fatal("saved credential was sent to another URL")
		}
		return aiJSONResponse(`{"data":[]}`), nil
	})})
	input := &AIProviderInput{BaseURL: "https://other.example.test"}
	if _, err := store.ListSystemAIProviderModels("super", input); !errors.Is(err, ErrInvalidArgument) || calls != 0 {
		t.Fatal("changed URL reused a stored key")
	}
	input.APIKey = "replacement-key"
	models, err := store.ListSystemAIProviderModels("super", input)
	if err != nil || models == nil || len(models) != 0 || calls != 1 {
		t.Fatalf("replacement credentials: models=%v calls=%d err=%v", models, calls, err)
	}
	provider, _ := store.SystemAIProvider("super")
	if provider.BaseURL != testAIProviderBaseURL || provider.APIKeyLast4 != "1234" {
		t.Fatal("model discovery saved unsaved connection changes")
	}
}

func TestAIProviderModelsEnforceConfigurationPermissionsAndArchive(t *testing.T) {
	store := newProjectRoleAuthorizationStore()
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return aiJSONResponse(`{"data":[{"id":"project-model"}]}`), nil
	})})
	input := &AIProviderInput{BaseURL: testAIProviderBaseURL, APIKey: "test-model-key"}
	for _, actor := range []string{"reader", "writer", "admin"} {
		if _, err := store.ListSystemAIProviderModels(actor, input); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("%s accessed system model discovery: %v", actor, err)
		}
	}
	for _, actor := range []string{"reader", "writer"} {
		if _, err := store.ListProjectAIProviderModels(actor, "project-a", input); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("%s accessed project model discovery: %v", actor, err)
		}
	}
	for _, actor := range []string{"admin", "super"} {
		if _, err := store.ListProjectAIProviderModels(actor, "project-a", input); err != nil {
			t.Fatalf("%s model discovery failed: %v", actor, err)
		}
	}
	store.projects["project-a"].Status = ProjectStatusArchived
	if _, err := store.ListProjectAIProviderModels("admin", "project-a", input); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("archived project model discovery: %v", err)
	}
}

func TestAIProviderModelsRecheckPermissionAfterRemoteCall(t *testing.T) {
	store := newProjectRoleAuthorizationStore()
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		store.mu.Lock()
		delete(store.members, memberKey("project-a", "admin"))
		store.mu.Unlock()
		return aiJSONResponse(`{"data":[{"id":"private-model"}]}`), nil
	})})
	input := &AIProviderInput{BaseURL: testAIProviderBaseURL, APIKey: "test-model-key"}
	models, err := store.ListProjectAIProviderModels("admin", "project-a", input)
	if !errors.Is(err, ErrPermissionDenied) || models != nil {
		t.Fatalf("revoked access returned model data: %v %v", models, err)
	}
}

func TestAIProviderModelsRejectUnsafeURLsAndInvalidTimeouts(t *testing.T) {
	store := newAISuperStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("invalid model connection reached the transport")
		return nil, nil
	})})
	for _, base := range []string{"http://example.test", "https://127.0.0.1", "https://169.254.169.254", "https://example.test?key=value"} {
		if _, err := store.ListSystemAIProviderModels("super", &AIProviderInput{BaseURL: base, APIKey: "key"}); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("unsafe URL accepted: %v", err)
		}
	}
	for _, timeout := range []int{0, 999, 120001} {
		if _, err := store.ListSystemAIProviderModels("super", &AIProviderInput{BaseURL: testAIProviderBaseURL, APIKey: "key", TimeoutMS: &timeout}); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid timeout accepted: %v", err)
		}
	}
}

func TestAIProviderModelsBoundResponsesAndRedactErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "secret-model-key"},
		{"redirect", 302, "secret-model-key"},
		{"malformed", 200, "secret-model-key"},
		{"missing data", 200, `{}`},
		{"null data", 200, `{"data":null}`},
		{"invalid ID", 200, `{"data":[{"id":123}]}`},
		{"blank ID", 200, `{"data":[{}]}`},
		{"control character", 200, `{"data":[{"id":"model\nname"}]}`},
		{"oversized", 200, strings.Repeat("x", aiProviderMaxResponseBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				response := aiJSONResponse(tc.body)
				response.StatusCode = tc.status
				response.Header = http.Header{"Location": {"https://other.example.test/models"}}
				return response, nil
			})}
			_, err := fetchAIProviderModels(context.Background(), client, &AIProviderConfig{BaseURL: testAIProviderBaseURL, TimeoutMS: 1000}, "secret-model-key")
			if !errors.Is(err, ErrFailedPrecondition) || strings.Contains(err.Error(), "secret-model-key") || calls != 1 {
				t.Fatalf("model response handling: calls=%d err=%v", calls, err)
			}
		})
	}
	t.Run("transport", func(t *testing.T) {
		client := &http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("invalid header: secret-model-key")
		})}
		_, err := fetchAIProviderModels(context.Background(), client, &AIProviderConfig{BaseURL: testAIProviderBaseURL, TimeoutMS: 1000}, "secret-model-key")
		if !errors.Is(err, ErrFailedPrecondition) || strings.Contains(err.Error(), "secret-model-key") {
			t.Fatal("transport error exposed provider credentials")
		}
	})
}

func TestAIProviderModelsHonorRequestCancellation(t *testing.T) {
	store := newAISuperStore(t)
	store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := store.WithContext(ctx).ListSystemAIProviderModels("super", &AIProviderInput{BaseURL: testAIProviderBaseURL, APIKey: "test-model-key"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request deadline was not honored: %v", err)
	}
}
