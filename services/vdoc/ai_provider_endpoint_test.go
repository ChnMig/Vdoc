package vdoc

import (
	"net/http"
	"strings"
	"testing"
)

func TestAIProviderModelDiscoveryAndTestUseSameVersionedBase(t *testing.T) {
	for _, scope := range []string{"system", "project"} {
		for _, saved := range []bool{false, true} {
			for _, mode := range []string{"responses", "chat_completions"} {
				for _, suffix := range []string{"", "/", "/v1", "/v1/", "/gateway", "/gateway/v1/"} {
					t.Run(scope+"/"+mode+"/"+suffix+map[bool]string{true: "/saved", false: "/unsaved"}[saved], func(t *testing.T) {
						store := newProjectRoleAuthorizationStore()
						input := testAIProviderInput(testAIProviderBaseURL + suffix)
						input.APIMode = mode
						if saved {
							var err error
							if scope == "project" {
								_, err = store.UpsertProjectAIProvider("super", "project-a", input)
							} else {
								_, err = store.UpsertSystemAIProvider("super", input)
							}
							if err != nil {
								t.Fatal(err)
							}
						}
						prefix := "/v1/"
						if strings.Contains(suffix, "gateway") {
							prefix = "/gateway/v1/"
						}
						calls := 0
						store.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
							calls++
							if r.Header.Get("Authorization") != "Bearer sk-test-1234" {
								t.Fatal("incorrect credential")
							}
							endpoint := "responses"
							if mode == "chat_completions" {
								endpoint = "chat/completions"
							}
							if r.Method == http.MethodGet {
								if r.URL.Path != prefix+"models" {
									t.Fatalf("model endpoint=%s", r.URL.Path)
								}
								return aiJSONResponse(`{"data":[{"id":"gpt-test"}]}`), nil
							}
							if r.URL.Path != prefix+endpoint {
								t.Fatalf("inference endpoint=%s", r.URL.Path)
							}
							if mode == "responses" {
								return aiJSONResponse(`{"status":"completed","output_text":"OK"}`), nil
							}
							return aiJSONResponse(`{"choices":[{"message":{"content":"OK"}}]}`), nil
						})})
						var arg *AIProviderInput
						if !saved {
							arg = &input
						}
						var err error
						var content string
						if scope == "project" {
							_, err = store.ListProjectAIProviderModels("super", "project-a", arg)
						} else {
							_, err = store.ListSystemAIProviderModels("super", arg)
						}
						if err != nil {
							t.Fatal(err)
						}
						if scope == "project" {
							content, err = store.TestProjectAIProvider("super", "project-a", arg)
						} else {
							content, err = store.TestSystemAIProvider("super", arg)
						}
						if err != nil || content != "OK" || calls != 2 {
							t.Fatalf("content=%q calls=%d err=%v", content, calls, err)
						}
					})
				}
			}
		}
	}
}
