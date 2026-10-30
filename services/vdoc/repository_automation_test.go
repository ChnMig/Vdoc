package vdoc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	domainai "vdoc/domain/ai"
	repo "vdoc/domain/documentrepo"
)

func newRepositoryAutomationFixture(t *testing.T) (*Store, *repo.Branch, *repo.Commit, *repo.Commit) {
	t.Helper()
	s := newTask5Store()
	t.Cleanup(s.StopRepositoryAutomation)
	t.Cleanup(s.StopSummaryWorker)
	b, err := s.RepositoryCreateBranch("writer", "project-a", repo.BranchInput{Name: "automation"})
	if err != nil {
		t.Fatal(err)
	}
	first := saveRepositoryAutomationContent(t, s, b, "", "# Before", "automation-first")
	second := saveRepositoryAutomationContent(t, s, b, first.Files[0].DocumentID, "# After", "automation-second")
	return s, b, first, second
}

func saveRepositoryAutomationContent(t *testing.T, s *Store, b *repo.Branch, documentID, content, key string) *repo.Commit {
	t.Helper()
	c, err := s.RepositorySave("writer", b.ProjectID, b.ID, repo.SaveInput{ExpectedHeadCommitID: b.HeadCommitID, ExpectedGeneration: b.Generation, IdempotencyKey: key, Changes: []repo.Change{{Operation: "upsert", DocumentID: documentID, RelativePath: "guide.md", Name: "Guide", DocumentType: DocumentTypeMarkdown, Content: content}}})
	if err != nil {
		t.Fatal(err)
	}
	b.HeadCommitID, b.Generation = c.ID, b.Generation+1
	return c
}

func TestRepositoryAnalysisPinsInputsWhenBranchAdvances(t *testing.T) {
	s, branch, before, target := newRepositoryAutomationFixture(t)
	upsertAuditProvider(t, s, domainai.ProviderModeChatCompletions, "analysis-secret")
	var advanced *repo.Commit
	s.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		_ = request.Body.Close()
		for _, required := range []string{before.ID, target.ID, "# Before", "# After", "guide.md"} {
			if !strings.Contains(string(body), required) {
				t.Errorf("analysis evidence missing %q", required)
			}
		}
		if strings.Contains(string(body), "object_key") {
			t.Error("AI context contains storage keys")
		}
		advanced = saveRepositoryAutomationContent(t, s, branch, target.Files[0].DocumentID, "# Later", "automation-advance")
		return aiJSONResponse(`{"choices":[{"message":{"content":"Fixed commit explanation"}}]}`), nil
	})})
	analysis, err := s.RepositoryAnalyze("reader", branch.ProjectID, repo.AnalysisInput{ToCommitID: target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Status != "succeeded" || analysis.FromCommitID != before.ID || analysis.ToCommitID != target.ID || analysis.Sequence < 1 || analysis.Content != "Fixed commit explanation" || analysis.CompletedAt == nil {
		t.Fatalf("analysis = %+v", analysis)
	}
	content, err := s.RepositoryContent("reader", branch.ProjectID, target.ID, target.Files[0].DocumentID)
	if err != nil || content.Content != "# After" || advanced.ID == target.ID {
		t.Fatalf("saved target changed: content=%+v error=%v", content, err)
	}
	list, err := s.RepositoryAnalyses("reader", branch.ProjectID, 0)
	if err != nil || len(list) != 1 || list[0].ID != analysis.ID {
		t.Fatalf("analysis history = %+v, %v", list, err)
	}
	next, err := s.RepositoryAnalyses("reader", branch.ProjectID, analysis.Sequence)
	if err != nil || len(next) != 0 {
		t.Fatalf("analysis cursor = %+v, %v", next, err)
	}
}

func TestRepositoryAnalysisCompletionRecordsFailureAfterContextChanges(t *testing.T) {
	for _, change := range []string{"provider", "prompt", "permission", "disabled-user", "archived-project", "canceled", "provider-error"} {
		t.Run(change, func(t *testing.T) {
			s, branch, _, target := newRepositoryAutomationFixture(t)
			upsertAuditProvider(t, s, domainai.ProviderModeChatCompletions, "must-not-leak")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.SetAIHTTPClient(&http.Client{Transport: aiRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				_ = request.Body.Close()
				s.mu.Lock()
				switch change {
				case "provider":
					s.aiProviders[systemAIProviderKey()].Model = "changed-model"
				case "prompt":
					s.aiPrompts[aiPromptKey(branch.ProjectID, domainai.PromptDiffChangeSummary)] = &AIPromptOverride{ID: "changed", ProjectID: branch.ProjectID, Scope: "project", PromptKey: domainai.PromptDiffChangeSummary, Enabled: true, SystemPrompt: "Changed", UserPromptTemplate: "{{context}}"}
				case "permission":
					delete(s.members, memberKey(branch.ProjectID, "reader"))
				case "disabled-user":
					s.users["reader"].Status = UserStatusDisabled
				case "archived-project":
					s.projects[branch.ProjectID].Status = ProjectStatusArchived
				}
				s.mu.Unlock()
				if change == "canceled" {
					cancel()
				}
				if change == "provider-error" {
					return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("must-not-leak"))}, nil
				}
				return aiJSONResponse(`{"choices":[{"message":{"content":"Discard this response"}}]}`), nil
			})})
			_, callErr := s.WithContext(ctx).RepositoryAnalyze("reader", branch.ProjectID, repo.AnalysisInput{ToCommitID: target.ID})
			if change == "permission" || change == "disabled-user" {
				if !errors.Is(callErr, ErrPermissionDenied) && !errors.Is(callErr, ErrUnauthenticated) {
					t.Fatalf("revoked response error=%v", callErr)
				}
			} else if callErr != nil {
				t.Fatal(callErr)
			}
			values, err := s.RepositoryAnalyses("admin", branch.ProjectID, 0)
			if err != nil || len(values) != 1 || values[0].Status != "failed" || values[0].Content != "" || values[0].CompletedAt == nil || strings.Contains(values[0].ErrorMessage, "must-not-leak") {
				t.Fatalf("failure history=%+v err=%v", values, err)
			}
			stored, err := s.RepositoryContent("admin", branch.ProjectID, target.ID, target.Files[0].DocumentID)
			if err != nil || stored.Content != "# After" {
				t.Fatalf("AI failure affected commit: %+v, %v", stored, err)
			}
		})
	}
}

func TestRepositoryAnalysisEvidenceIsBoundedAndValid(t *testing.T) {
	s, branch, before, target := newRepositoryAutomationFixture(t)
	target = saveRepositoryAutomationContent(t, s, branch, target.Files[0].DocumentID, strings.Repeat("\x01文", 100000), "automation-large")
	evidence, truncated, err := s.repositoryAnalysisEvidence("reader", branch.ProjectID, target, repo.AnalysisInput{FromCommitID: before.ID, ToCommitID: target.ID})
	if err != nil || !truncated || len(evidence) > repositoryAnalysisContextBytes || !utf8.ValidString(evidence) {
		t.Fatalf("evidence length=%d truncated=%v error=%v", len(evidence), truncated, err)
	}
	parts := strings.SplitN(evidence, "\n", 3)
	if len(parts) != 3 || !json.Valid([]byte(parts[2])) {
		t.Fatal("bounded evidence is not valid JSON")
	}
	_, _, err = s.repositoryAnalysisEvidence("reader", branch.ProjectID, target, repo.AnalysisInput{FromCommitID: before.ID, ToCommitID: target.ID, DocumentID: "missing"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing document error=%v", err)
	}
}

func TestRepositoryInterruptedAnalysisExpiresWithoutChangingFreshRun(t *testing.T) {
	s, branch, before, target := newRepositoryAutomationFixture(t)
	err := s.WithRepositoryRead("reader", branch.ProjectID, func(storage repo.Storage) error {
		for _, entry := range []struct {
			id  string
			age time.Duration
		}{{"old", 11 * time.Minute}, {"fresh", time.Minute}} {
			value := repo.Analysis{ID: entry.id, ProjectID: branch.ProjectID, FromCommitID: before.ID, ToCommitID: target.ID, Status: "running", CreatedBy: "reader", CreatedAt: time.Now().Add(-entry.age)}
			if err := putRepositoryAutomation(context.Background(), storage, branch.ProjectID, "analysis", entry.id, value, 1, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repositoryExpireAnalyses(branch.ProjectID); err != nil {
		t.Fatal(err)
	}
	old, _ := s.RepositoryAnalysis("admin", branch.ProjectID, "old")
	fresh, _ := s.RepositoryAnalysis("admin", branch.ProjectID, "fresh")
	if old.Status != "failed" || old.CompletedAt == nil || fresh.Status != "running" || fresh.CompletedAt != nil {
		t.Fatalf("old=%+v fresh=%+v", old, fresh)
	}
}

func appendRepositoryAutomationEvent(t *testing.T, s *Store, branch *repo.Branch, before, target *repo.Commit, eventType, eventID string) {
	t.Helper()
	err := s.WithRepositoryWrite("admin", branch.ProjectID, MemberRoleAdmin, func(storage repo.Storage) error {
		event := repo.Event{ID: eventID, ProjectID: branch.ProjectID, BranchID: branch.ID, EventType: eventType, BeforeCommitID: before.ID, AfterCommitID: target.ID, ActorID: "writer", CreatedAt: time.Now().UTC()}
		return putRepositoryAutomation(context.Background(), storage, branch.ProjectID, "event", eventID, event, 1, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRepositorySubscriptionsDeliverOnceAndSkipCompletedPages(t *testing.T) {
	s, branch, before, target := newRepositoryAutomationFixture(t)
	subscription, err := s.RepositoryCreateSubscription("admin", branch.ProjectID, repo.SubscriptionInput{Name: "Updates", Kind: "in_app", EventTypes: []string{"commit.created"}, BranchID: branch.ID, DocumentID: target.Files[0].DocumentID})
	if err != nil {
		t.Fatal(err)
	}
	// 已完成的第一页不能让后续待投递事件永久饥饿。
	err = s.WithRepositoryWrite("admin", branch.ProjectID, MemberRoleAdmin, func(storage repo.Storage) error {
		for i := 0; i < 100; i++ {
			value := repositoryStoredDelivery{Delivery: repo.Delivery{ID: fmt.Sprintf("completed-%d", i), SubscriptionID: subscription.ID, Status: "succeeded", CreatedAt: time.Now()}}
			if err := putRepositoryAutomation(context.Background(), storage, branch.ProjectID, "delivery", value.ID, value, 1, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	appendRepositoryAutomationEvent(t, s, branch, before, target, "commit.created", "deliver-once")
	for i := 0; i < 2; i++ {
		if err := s.RepositoryDrainDeliveries("admin", branch.ProjectID); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.RepositoryDeliveries("admin", branch.ProjectID, 0)
	if err != nil || len(first) != 100 {
		t.Fatalf("first delivery page=%d error=%v", len(first), err)
	}
	next, err := s.RepositoryDeliveries("admin", branch.ProjectID, first[99].Sequence)
	if err != nil || len(next) != 1 || next[0].Status != "succeeded" || next[0].AttemptCount != 1 || next[0].EventID != "deliver-once" {
		t.Fatalf("next delivery page=%+v error=%v", next, err)
	}
	if _, err := s.RepositoryDeliveries("reader", branch.ProjectID, 0); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("non-admin delivery error=%v", err)
	}
}

func TestRepositorySubscriptionPauseResumeFiltersAndPermissionRevocation(t *testing.T) {
	s, branch, before, target := newRepositoryAutomationFixture(t)
	sub, err := s.RepositoryCreateSubscription("admin", branch.ProjectID, repo.SubscriptionInput{Name: "Releases", Kind: "in_app"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RepositoryPauseSubscription("admin", branch.ProjectID, sub.ID, true); err != nil {
		t.Fatal(err)
	}
	appendRepositoryAutomationEvent(t, s, branch, before, target, "release.published", "paused-release")
	if err := s.RepositoryDrainDeliveries("admin", branch.ProjectID); err != nil {
		t.Fatal(err)
	}
	values, _ := s.RepositoryDeliveries("admin", branch.ProjectID, 0)
	if len(values) != 0 {
		t.Fatalf("paused deliveries=%+v", values)
	}
	if _, err := s.RepositoryPauseSubscription("admin", branch.ProjectID, sub.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.RepositoryDrainDeliveries("admin", branch.ProjectID); err != nil {
		t.Fatal(err)
	}
	values, _ = s.RepositoryDeliveries("admin", branch.ProjectID, 0)
	if len(values) != 1 || values[0].EventID != "paused-release" {
		t.Fatalf("resumed deliveries=%+v", values)
	}
	appendRepositoryAutomationEvent(t, s, branch, before, target, "release.published", "revoked-release")
	delete(s.members, memberKey(branch.ProjectID, "admin"))
	if err := s.RepositoryDrainDeliveries("super", branch.ProjectID); err != nil {
		t.Fatal(err)
	}
	values, _ = s.RepositoryDeliveries("super", branch.ProjectID, 0)
	subs, _ := s.RepositorySubscriptions("super", branch.ProjectID)
	if len(values) != 2 || values[1].Status != "failed" || !subs[0].Paused {
		t.Fatalf("revoked delivery=%+v subscription=%+v", values, subs)
	}
}

func TestRepositoryPausedDeliveriesDoNotStarveActiveSubscriptions(t *testing.T) {
	s, branch, before, target := newRepositoryAutomationFixture(t)
	paused, err := s.RepositoryCreateSubscription("admin", branch.ProjectID, repo.SubscriptionInput{Name: "Paused", Kind: "in_app"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WithRepositoryWrite("admin", branch.ProjectID, MemberRoleAdmin, func(storage repo.Storage) error {
		for i := 0; i < 40; i++ {
			value := repositoryStoredDelivery{Delivery: repo.Delivery{ID: fmt.Sprintf("paused-%d", i), SubscriptionID: paused.ID, Status: "pending", CreatedAt: time.Now()}}
			if err := putRepositoryAutomation(context.Background(), storage, branch.ProjectID, "delivery", value.ID, value, 1, nil); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RepositoryPauseSubscription("admin", branch.ProjectID, paused.ID, true); err != nil {
		t.Fatal(err)
	}
	active, err := s.RepositoryCreateSubscription("admin", branch.ProjectID, repo.SubscriptionInput{Name: "Active", Kind: "in_app"})
	if err != nil {
		t.Fatal(err)
	}
	appendRepositoryAutomationEvent(t, s, branch, before, target, "release.published", "active-release")
	if err := s.RepositoryDrainDeliveries("admin", branch.ProjectID); err != nil {
		t.Fatal(err)
	}
	values, err := s.RepositoryDeliveries("admin", branch.ProjectID, 0)
	if err != nil || len(values) != 41 || values[40].SubscriptionID != active.ID || values[40].Status != "succeeded" {
		t.Fatalf("active delivery was starved: values=%+v error=%v", values, err)
	}
}

func TestRepositoryDeliveryHonorsLeaseAndManualRetryKeepsEventIdentity(t *testing.T) {
	s, branch, before, target := newRepositoryAutomationFixture(t)
	sub, err := s.RepositoryCreateSubscription("admin", branch.ProjectID, repo.SubscriptionInput{Name: "Changes", Kind: "in_app", EventTypes: []string{"commit.created"}, DocumentID: target.Files[0].DocumentID})
	if err != nil {
		t.Fatal(err)
	}
	appendRepositoryAutomationEvent(t, s, branch, target, target, "commit.created", "unchanged-document")
	appendRepositoryAutomationEvent(t, s, branch, before, target, "commit.created", "leased-event")
	if err := s.repositoryEnqueueDeliveries("admin", branch.ProjectID); err != nil {
		t.Fatal(err)
	}
	values, _ := s.RepositoryDeliveries("admin", branch.ProjectID, 0)
	if len(values) != 1 || values[0].SubscriptionID != sub.ID || values[0].EventID != "leased-event" {
		t.Fatalf("filtered deliveries=%+v", values)
	}
	deliveryID := values[0].ID
	update := func(status string, lease time.Time) {
		t.Helper()
		err := s.WithRepositoryWrite("admin", branch.ProjectID, MemberRoleAdmin, func(storage repo.Storage) error {
			var value repositoryStoredDelivery
			record, err := getRepositoryAutomation(context.Background(), storage, branch.ProjectID, "delivery", deliveryID, &value)
			if err != nil {
				return err
			}
			value.Status, value.LeaseID, value.LeaseExpiresAt = status, "other-worker-lease", &lease
			return putRepositoryAutomation(context.Background(), storage, branch.ProjectID, "delivery", deliveryID, value, record.Generation+1, &record.Generation)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	update("processing", time.Now().Add(time.Minute))
	if err := s.RepositoryDrainDeliveries("admin", branch.ProjectID); err != nil {
		t.Fatal(err)
	}
	values, _ = s.RepositoryDeliveries("admin", branch.ProjectID, 0)
	if values[0].Status != "processing" || values[0].AttemptCount != 0 {
		t.Fatalf("active lease was stolen: %+v", values)
	}
	update("processing", time.Now().Add(-time.Minute))
	if err := s.RepositoryDrainDeliveries("admin", branch.ProjectID); err != nil {
		t.Fatal(err)
	}
	values, _ = s.RepositoryDeliveries("admin", branch.ProjectID, 0)
	if values[0].Status != "succeeded" || values[0].AttemptCount != 1 {
		t.Fatalf("expired lease did not recover: %+v", values)
	}
	if _, err := s.RepositoryRetryDelivery("admin", branch.ProjectID, deliveryID); !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("retry succeeded delivery error=%v", err)
	}
	update("failed", time.Now())
	retried, err := s.RepositoryRetryDelivery("admin", branch.ProjectID, deliveryID)
	if err != nil || retried.Status != "pending" || retried.EventID != "leased-event" || retried.AttemptCount != 0 || retried.CompletedAt != nil {
		t.Fatalf("retry=%+v err=%v", retried, err)
	}
	if err := s.RepositoryDrainDeliveries("admin", branch.ProjectID); err != nil {
		t.Fatal(err)
	}
	values, _ = s.RepositoryDeliveries("admin", branch.ProjectID, 0)
	if len(values) != 1 || values[0].Status != "succeeded" || values[0].EventID != "leased-event" {
		t.Fatalf("retry duplicated or replaced event: %+v", values)
	}
}

func TestRepositoryWebhookSecretIsEncryptedBoundAndOnlyReturnedOnce(t *testing.T) {
	s, branch, _, _ := newRepositoryAutomationFixture(t)
	created, err := s.RepositoryCreateSubscription("admin", branch.ProjectID, repo.SubscriptionInput{Name: "Webhook", Kind: "webhook", URL: "https://8.8.8.8/vdoc"})
	if err != nil || len(created.Secret) != 64 {
		t.Fatalf("created=%+v error=%v", created, err)
	}
	values, err := s.RepositorySubscriptions("admin", branch.ProjectID)
	encoded, _ := json.Marshal(values)
	if err != nil || strings.Contains(string(encoded), created.Secret) || strings.Contains(string(encoded), "cipher") {
		t.Fatalf("subscription list leaked secret; error=%v", err)
	}
	var stored repositoryStoredSubscription
	err = s.WithRepositoryRead("admin", branch.ProjectID, func(storage repo.Storage) error {
		record, err := getRepositoryAutomation(context.Background(), storage, branch.ProjectID, "subscription", created.ID, &stored)
		if err == nil && strings.Contains(string(record.Payload), created.Secret) {
			t.Error("persisted webhook secret is plaintext")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.repositorySubscriptionSecret(branch.ProjectID, stored)
	if err != nil || secret != created.Secret {
		t.Fatal("stored secret cannot be decrypted")
	}
	if _, err := s.repositorySubscriptionSecret("other-project", stored); err == nil {
		t.Fatal("signing secret accepted under another project")
	}
	for _, url := range []string{"http://8.8.8.8/hook", "https://127.0.0.1/hook", "https://169.254.169.254/latest", "https://user:password@8.8.8.8/hook", "https://8.8.8.8/#fragment", "https://[::1]/hook"} {
		if _, err := normalizeRepositoryWebhookURL(context.Background(), url); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("unsafe URL accepted: %s err=%v", url, err)
		}
	}
}

func TestRepositoryWebhookSigningRetryAndRedirectPolicy(t *testing.T) {
	const secret = "synthetic-signing-key"
	event := repo.Event{ID: "stable-event-id", ProjectID: "project", EventType: "commit.created", AfterCommitID: "exact-commit", CreatedAt: time.Now().UTC()}
	status, received, redirected := 200, 0, 0
	sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) { redirected++; w.WriteHeader(204) }))
	defer sink.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		received++
		body, _ := io.ReadAll(request.Body)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(request.Header.Get("X-Vdoc-Timestamp") + "."))
		_, _ = mac.Write(body)
		if request.Header.Get("X-Vdoc-Event-ID") != event.ID || request.Header.Get("X-Vdoc-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) || !strings.Contains(string(body), event.AfterCommitID) {
			t.Error("webhook signature or immutable payload did not verify")
		}
		if status == 302 {
			w.Header().Set("Location", sink.URL)
		}
		w.WriteHeader(status)
	}))
	defer server.Close()
	for _, entry := range []struct {
		status int
		retry  bool
	}{{200, false}, {204, false}, {400, false}, {408, true}, {429, true}, {500, true}, {302, false}} {
		status = entry.status
		actual, retry, err := sendRepositoryWebhook(context.Background(), server.Client(), server.URL, secret, event)
		if status < 300 {
			if err != nil || actual != status || retry {
				t.Fatalf("successful webhook: status=%d retry=%v err=%v", actual, retry, err)
			}
		} else if err == nil || retry != entry.retry {
			t.Fatalf("failed webhook HTTP%d: retry=%v err=%v", status, retry, err)
		}
	}
	if received != 7 || redirected != 0 {
		t.Fatalf("received=%d redirected=%d", received, redirected)
	}
}
