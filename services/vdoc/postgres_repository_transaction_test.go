package vdoc

import (
	"context"
	"errors"
	"testing"
	"time"

	domainvdoc "vdoc/domain/vdoc"
)

func TestSaveLockedRollsBackBusinessRowsWhenAuditFails(t *testing.T) {
	repo := newTransactionalFailureRepository()
	store := NewStore()
	store.shares["share-id"] = &domainvdoc.DocumentShare{ID: "share-id"}
	store.audits["audit-id"] = &domainvdoc.AuditLog{ID: "audit-id"}

	err := (&postgresPersistence{repo: repo}).saveLocked(context.Background(), store)
	if !errors.Is(err, errInjectedAuditFailure) {
		t.Fatalf("saveLocked() error = %v, want injected audit failure", err)
	}
	if repo.transactions != 1 {
		t.Fatalf("transactions = %d, want 1", repo.transactions)
	}
	if len(repo.shares) != 0 || len(repo.audits) != 0 {
		t.Fatalf("failed transaction committed shares=%v audits=%v", repo.shares, repo.audits)
	}
}

func TestSaveLockedWithObjectRefsRollsBackRefsAndDiffWhenAuditFails(t *testing.T) {
	repo := newTransactionalFailureRepository()
	store := NewStore()
	store.versions["from"] = &domainvdoc.ContractVersion{ID: "from"}
	store.versions["to"] = &domainvdoc.ContractVersion{ID: "to"}
	store.diffs["diff-id"] = &domainvdoc.Diff{ID: "diff-id", FromVersionID: "from", ToVersionID: "to"}
	store.audits["audit-id"] = &domainvdoc.AuditLog{ID: "audit-id"}
	ref := domainvdoc.ObjectRef{Key: "objects/diff.json", Kind: "full-diff", OwnerType: "diff", OwnerID: "diff-id"}

	err := (&postgresPersistence{repo: repo}).saveLockedWithObjectRefs(context.Background(), store, []domainvdoc.ObjectRef{ref})
	if !errors.Is(err, errInjectedAuditFailure) {
		t.Fatalf("saveLockedWithObjectRefs() error = %v, want injected audit failure", err)
	}
	if len(repo.objects) != 0 || len(repo.diffs) != 0 || len(repo.audits) != 0 {
		t.Fatalf("failed transaction committed objects=%v diffs=%v audits=%v", repo.objects, repo.diffs, repo.audits)
	}
}

func TestSaveLockedRollsBackAIProviderAndPromptWhenAuditFails(t *testing.T) {
	repo := newTransactionalFailureRepository()
	store := NewStore()
	store.aiProviders["system"] = &domainvdoc.AIProviderConfig{ID: "provider-id", Scope: "system"}
	store.aiPrompts["system:page_chat"] = &domainvdoc.AIPromptOverride{ID: "prompt-id", Scope: "system", PromptKey: "page_chat"}
	store.audits["audit-id"] = &domainvdoc.AuditLog{ID: "audit-id"}

	err := (&postgresPersistence{repo: repo}).saveLocked(context.Background(), store)
	if !errors.Is(err, errInjectedAuditFailure) {
		t.Fatalf("saveLocked() error = %v, want injected audit failure", err)
	}
	if repo.transactions != 1 {
		t.Fatalf("transactions = %d, want 1", repo.transactions)
	}
	if len(repo.providers) != 0 || len(repo.prompts) != 0 || len(repo.audits) != 0 {
		t.Fatalf("failed transaction committed providers=%v prompts=%v audits=%v", repo.providers, repo.prompts, repo.audits)
	}
}

func TestSaveLockedUsesOptimisticTokenMutationWithLoadedVersion(t *testing.T) {
	now := time.Now().UTC()
	previous := &domainvdoc.MCPToken{ID: "token-id", Status: domainvdoc.MCPTokenStatusActive, UpdatedAt: now}
	current := *previous
	current.Status = domainvdoc.MCPTokenStatusRevoked
	current.UpdatedAt = now.Add(time.Second)
	base := newRecordingRepository(domainvdoc.NewState())
	repo := &optimisticTokenFailureRepository{recordingRepository: base}
	store := NewStore()
	store.tokens[current.ID] = &current
	store.persisted = domainvdoc.NewState()
	store.persisted.Tokens[previous.ID] = previous

	err := (&postgresPersistence{repo: repo}).saveLocked(context.Background(), store)
	if !errors.Is(err, errInjectedOptimisticConflict) {
		t.Fatalf("saveLocked() error = %v, want optimistic conflict", err)
	}
	if repo.calls != 1 || repo.previous != previous {
		t.Fatalf("optimistic token calls=%d previous=%p, want one call with loaded version %p", repo.calls, repo.previous, previous)
	}
	if base.saves != 0 {
		t.Fatalf("legacy token upsert calls = %d, want none", base.saves)
	}
}

var errInjectedOptimisticConflict = errors.New("injected optimistic conflict")

type optimisticTokenFailureRepository struct {
	*recordingRepository
	calls    int
	previous *domainvdoc.MCPToken
}

func (r *optimisticTokenFailureRepository) UpsertMCPTokenIfUnchanged(_ context.Context, _ *domainvdoc.MCPToken, previous *domainvdoc.MCPToken) error {
	r.calls++
	r.previous = previous
	return errInjectedOptimisticConflict
}

var errInjectedAuditFailure = errors.New("injected audit failure")

type transactionalFailureRepository struct {
	shares       map[string]*domainvdoc.DocumentShare
	audits       map[string]*domainvdoc.AuditLog
	objects      map[string]domainvdoc.ObjectRef
	diffs        map[string]*domainvdoc.Diff
	providers    map[string]*domainvdoc.AIProviderConfig
	prompts      map[string]*domainvdoc.AIPromptOverride
	transactions int
	failAudit    bool
}

func newTransactionalFailureRepository() *transactionalFailureRepository {
	return &transactionalFailureRepository{
		shares:    map[string]*domainvdoc.DocumentShare{},
		audits:    map[string]*domainvdoc.AuditLog{},
		objects:   map[string]domainvdoc.ObjectRef{},
		diffs:     map[string]*domainvdoc.Diff{},
		providers: map[string]*domainvdoc.AIProviderConfig{},
		prompts:   map[string]*domainvdoc.AIPromptOverride{},
		failAudit: true,
	}
}

func (r *transactionalFailureRepository) WithinTransaction(ctx context.Context, fn func(domainvdoc.Repository) error) error {
	r.transactions++
	tx := &transactionalFailureRepository{
		shares:    cloneShareMap(r.shares),
		audits:    cloneAuditMap(r.audits),
		objects:   cloneObjectRefMap(r.objects),
		diffs:     cloneDiffMap(r.diffs),
		providers: cloneAIProviderMap(r.providers),
		prompts:   cloneAIPromptMap(r.prompts),
		failAudit: r.failAudit,
	}
	if err := fn(tx); err != nil {
		return err
	}
	r.shares = tx.shares
	r.audits = tx.audits
	r.objects = tx.objects
	r.diffs = tx.diffs
	r.providers = tx.providers
	r.prompts = tx.prompts
	return nil
}

func (r *transactionalFailureRepository) LoadState(context.Context) (*domainvdoc.State, error) {
	return domainvdoc.NewState(), nil
}

func (r *transactionalFailureRepository) LoadUser(context.Context, string) (*domainvdoc.User, error) {
	return nil, domainvdoc.ErrNotFound
}

func (r *transactionalFailureRepository) ArchiveTeam(context.Context, string, *domainvdoc.AuditLog) error {
	return nil
}

func (r *transactionalFailureRepository) RecordObject(_ context.Context, ref domainvdoc.ObjectRef) error {
	r.objects[ref.Key] = ref
	return nil
}

func (r *transactionalFailureRepository) RecordAudit(_ context.Context, audit *domainvdoc.AuditLog) error {
	if r.failAudit {
		return errInjectedAuditFailure
	}
	if audit != nil {
		copied := *audit
		r.audits[audit.ID] = &copied
	}
	return nil
}

func (r *transactionalFailureRepository) UpsertMCPToken(context.Context, *domainvdoc.MCPToken) error {
	return nil
}

func (r *transactionalFailureRepository) UpsertAIProvider(_ context.Context, provider *domainvdoc.AIProviderConfig) error {
	if provider != nil {
		copied := *provider
		copied.APIKeyCiphertext = append([]byte(nil), provider.APIKeyCiphertext...)
		r.providers[provider.ID] = &copied
	}
	return nil
}

func (r *transactionalFailureRepository) UpsertAIPrompt(_ context.Context, prompt *domainvdoc.AIPromptOverride) error {
	if prompt != nil {
		copied := *prompt
		r.prompts[prompt.ID] = &copied
	}
	return nil
}

func (r *transactionalFailureRepository) UpsertAISummary(context.Context, *domainvdoc.AISummary) error {
	return nil
}

func (r *transactionalFailureRepository) UpsertAIChatSession(context.Context, *domainvdoc.AIChatSession) error {
	return nil
}

func (r *transactionalFailureRepository) UpsertAIChatMessage(context.Context, *domainvdoc.AIChatMessage) error {
	return nil
}

func (r *transactionalFailureRepository) PublishState(context.Context, domainvdoc.PublishStateInput) error {
	return nil
}

func (r *transactionalFailureRepository) UpsertDocumentShare(_ context.Context, share *domainvdoc.DocumentShare) error {
	if share != nil {
		copied := *share
		r.shares[share.ID] = &copied
	}
	return nil
}

func (r *transactionalFailureRepository) UpsertDocumentDiff(_ context.Context, diff *domainvdoc.Diff, _, _ *domainvdoc.ContractVersion) error {
	if diff != nil {
		copied := *diff
		r.diffs[diff.ID] = &copied
	}
	return nil
}

func cloneShareMap(values map[string]*domainvdoc.DocumentShare) map[string]*domainvdoc.DocumentShare {
	cloned := make(map[string]*domainvdoc.DocumentShare, len(values))
	for key, value := range values {
		copied := *value
		cloned[key] = &copied
	}
	return cloned
}

func cloneAuditMap(values map[string]*domainvdoc.AuditLog) map[string]*domainvdoc.AuditLog {
	cloned := make(map[string]*domainvdoc.AuditLog, len(values))
	for key, value := range values {
		copied := *value
		cloned[key] = &copied
	}
	return cloned
}

func cloneObjectRefMap(values map[string]domainvdoc.ObjectRef) map[string]domainvdoc.ObjectRef {
	cloned := make(map[string]domainvdoc.ObjectRef, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneDiffMap(values map[string]*domainvdoc.Diff) map[string]*domainvdoc.Diff {
	cloned := make(map[string]*domainvdoc.Diff, len(values))
	for key, value := range values {
		copied := *value
		cloned[key] = &copied
	}
	return cloned
}

func cloneAIProviderMap(values map[string]*domainvdoc.AIProviderConfig) map[string]*domainvdoc.AIProviderConfig {
	cloned := make(map[string]*domainvdoc.AIProviderConfig, len(values))
	for key, value := range values {
		copied := *value
		copied.APIKeyCiphertext = append([]byte(nil), value.APIKeyCiphertext...)
		cloned[key] = &copied
	}
	return cloned
}

func cloneAIPromptMap(values map[string]*domainvdoc.AIPromptOverride) map[string]*domainvdoc.AIPromptOverride {
	cloned := make(map[string]*domainvdoc.AIPromptOverride, len(values))
	for key, value := range values {
		copied := *value
		cloned[key] = &copied
	}
	return cloned
}
