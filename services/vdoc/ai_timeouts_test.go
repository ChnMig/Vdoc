package vdoc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAIResponsePhaseTimeoutsOverRealHTTP(t *testing.T) {
	for _, test := range []struct {
		name                                   string
		total, first, idle                     int
		beforeHeaders, beforeData, betweenData time.Duration
		chunks                                 int
		want                                   string
	}{
		{name: "first response includes headers", first: 80, idle: 500, beforeHeaders: 200 * time.Millisecond, chunks: 1, want: "first response"},
		{name: "headers do not satisfy first response", first: 80, idle: 500, beforeData: 200 * time.Millisecond, chunks: 1, want: "first response"},
		{name: "stalled stream", first: 500, idle: 80, betweenData: 200 * time.Millisecond, chunks: 2, want: "response idle"},
		{name: "heartbeats renew idle beyond first deadline", first: 100, idle: 100, betweenData: 20 * time.Millisecond, chunks: 10},
		{name: "total still bounds an active stream", total: 100, first: 500, idle: 500, betweenData: 20 * time.Millisecond, chunks: 15, want: "total generation"},
		{name: "all limits disabled", beforeData: 100 * time.Millisecond, betweenData: 30 * time.Millisecond, chunks: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !waitAITestContext(r.Context(), test.beforeHeaders) {
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.(http.Flusher).Flush()
				if !waitAITestContext(r.Context(), test.beforeData) {
					return
				}
				for i := 0; i < test.chunks; i++ {
					if i > 0 && !waitAITestContext(r.Context(), test.betweenData) {
						return
					}
					_, _ = io.WriteString(w, ": heartbeat\n\n")
					w.(http.Flusher).Flush()
				}
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			client := &http.Client{Transport: aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				copy := r.Clone(r.Context())
				copy.URL.Scheme, copy.URL.Host = "http", server.Listener.Addr().String()
				return server.Client().Do(copy)
			})}
			provider := &AIProviderConfig{BaseURL: testAIProviderBaseURL, Model: "test", TimeoutMS: test.total, FirstResponseTimeoutMS: &test.first, IdleTimeoutMS: &test.idle}
			result, err := callChatCompletions(context.Background(), client, aiCompletionRequest{Provider: provider})
			if test.want != "" {
				if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("wrong phase error: %v", err)
				}
				if result.Content != "" {
					t.Fatal("timed-out generation returned committed content")
				}
			} else if err != nil || result.Content != "complete" {
				t.Fatalf("content=%q error=%v", result.Content, err)
			}
		})
	}
}

func waitAITestContext(ctx context.Context, duration time.Duration) bool {
	if duration == 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func TestUnlimitedAITimeoutsStillHonorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	zero := 0
	watch := newAIResponseWatchdog(ctx, &AIProviderConfig{FirstResponseTimeoutMS: &zero, IdleTimeoutMS: &zero})
	defer watch.close()
	if _, ok := watch.ctx.Deadline(); ok {
		t.Fatal("unlimited request has a deadline")
	}
	cancel()
	if !errors.Is(watch.err(nil), context.Canceled) {
		t.Fatal("client cancellation was lost")
	}
}

func TestAIProviderTimeoutConfigurationPreservesUnlimitedAndOmittedUpdates(t *testing.T) {
	store := newAISuperStore(t)
	input := testAIProviderInput(testAIProviderBaseURL)
	input.TimeoutMS, input.FirstResponseTimeoutMS, input.IdleTimeoutMS = intPtr(0), intPtr(0), intPtr(0)
	provider, err := store.UpsertSystemAIProvider("super", input)
	if err != nil {
		t.Fatal(err)
	}
	if provider.TimeoutMS != 0 || provider.FirstResponseTimeout() != 0 || provider.IdleTimeout() != 0 {
		t.Fatal("unlimited configuration changed")
	}
	input.TimeoutMS, input.FirstResponseTimeoutMS, input.IdleTimeoutMS = nil, nil, nil
	provider, err = store.UpsertSystemAIProvider("super", input)
	if err != nil || provider.TimeoutMS != 0 || provider.FirstResponseTimeout() != 0 || provider.IdleTimeout() != 0 {
		t.Fatalf("omitted update reset unlimited: %+v %v", provider, err)
	}
	input.TimeoutMS = intPtr(900000)
	provider, err = store.UpsertSystemAIProvider("super", input)
	if err != nil || provider.TimeoutMS != 900000 {
		t.Fatalf("long total rejected: %+v %v", provider, err)
	}
	for _, invalid := range []int{-1, 1, 999, 2147483648} {
		input.IdleTimeoutMS = intPtr(invalid)
		if _, err = store.UpsertSystemAIProvider("super", input); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid idle timeout accepted: %d %v", invalid, err)
		}
	}
}
