package vdoc

import (
	"context"
	"errors"
	"strings"
	"testing"

	domain "vdoc/domain/vdoc"
)

func TestAIProviderKeyRevealScopePermissionsAndAudit(t *testing.T) {
	store := newProjectRoleAuthorizationStore()
	input := testAIProviderInput(testAIProviderBaseURL)
	if _, err := store.UpsertSystemAIProvider("super", input); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"admin", "writer", "reader"} {
		if key, err := store.RevealSystemAIProviderKey(actor); key != "" || !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("system access: %s %v", actor, err)
		}
	}
	if key, err := store.RevealProjectAIProviderKey("admin", "project-a"); key != "" || !errors.Is(err, ErrNotFound) {
		t.Fatalf("project fallback must not reveal system key: %v", err)
	}
	input.APIKey = "synthetic-project-key"
	if _, err := store.UpsertProjectAIProvider("admin", "project-a", input); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"writer", "reader"} {
		if key, err := store.RevealProjectAIProviderKey(actor, "project-a"); key != "" || !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("project access: %s %v", actor, err)
		}
	}
	if key, err := store.RevealProjectAIProviderKey("admin", "project-a"); err != nil || key != input.APIKey {
		t.Fatalf("reveal project: %v", err)
	}
	if key, err := store.RevealSystemAIProviderKey("super"); err != nil || key != "sk-test-1234" {
		t.Fatalf("reveal system: %v", err)
	}
	count := 0
	for _, audit := range store.audits {
		if audit.Action != "ai.provider.key.reveal" {
			continue
		}
		count++
		for _, value := range audit.Metadata {
			if strings.Contains(value, "synthetic-project-key") || strings.Contains(value, "sk-test-1234") {
				t.Fatal("secret leaked into audit metadata")
			}
		}
	}
	if count != 2 {
		t.Fatalf("reveal audit count=%d", count)
	}
	store.projects["project-a"].Status = ProjectStatusArchived
	if key, err := store.RevealProjectAIProviderKey("admin", "project-a"); key != "" || !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("archived reveal: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if key, err := store.WithContext(ctx).RevealSystemAIProviderKey("super"); key != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reveal: %v", err)
	}
}

func TestAIProviderKeyRevealDoesNotReturnSecretAfterTransactionAuthorizationLoss(t *testing.T) {
	for _, system := range []bool{false, true} {
		fixture := newAICompletionTransactionFixture(t, false, false)
		actor, project := "admin", fixture.target.ProjectID
		if system {
			actor, project = "super", ""
		} else {
			provider := cloneAIProvider(fixture.repo.state.AIProviders[systemAIProviderKey()])
			provider.ProjectID, provider.Scope = project, "project"
			fixture.repo.state.AIProviders[projectAIProviderKey(project)] = provider
		}
		before := len(fixture.repo.state.AuditLogs)
		fixture.repo.beforeTransaction = func(state *domain.State) {
			if system {
				state.Users[actor].IsSuperAdmin = false
			} else {
				delete(state.Members, memberKey(project, actor))
			}
		}
		key, err := fixture.store.revealAIProviderKey(actor, project)
		if key != "" || !errors.Is(err, ErrPermissionDenied) || len(fixture.repo.state.AuditLogs) != before {
			t.Fatalf("reveal after access loss: err=%v audits=%d", err, len(fixture.repo.state.AuditLogs)-before)
		}
	}
}

func TestAIProviderKeyRevealRequiresSuccessfulAuditPersistence(t *testing.T) {
	store := newAISuperStore(t)
	if _, err := store.UpsertSystemAIProvider("super", testAIProviderInput(testAIProviderBaseURL)); err != nil {
		t.Fatal(err)
	}
	repo := &aiKeyAuditFailureRepository{recordingRepository: &recordingRepository{state: store.cloneStateLocked()}}
	store.persistence = &postgresPersistence{repo: repo}
	store.persisted = store.cloneStateLocked()
	key, err := store.RevealSystemAIProviderKey("super")
	if key != "" || !errors.Is(err, errInjectedAuditFailure) {
		t.Fatalf("audit failure must not return a key: %v", err)
	}
}

type aiKeyAuditFailureRepository struct{ *recordingRepository }

func (r *aiKeyAuditFailureRepository) LoadState(context.Context) (*domain.State, error) {
	return cloneAICompletionState(r.state), nil
}

func (r *aiKeyAuditFailureRepository) WithinTransaction(_ context.Context, fn func(domain.Repository) error) error {
	return fn(r)
}

func (r *aiKeyAuditFailureRepository) RecordAudit(context.Context, *domain.AuditLog) error {
	return errInjectedAuditFailure
}
