package db_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
	"vdoc/utils/jsonvalue"
)

func TestPostgresRepairAcceptedNumberCanBePublished(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx := context.Background()
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	objects := &optimizationObjects{bodies: map[string][]byte{}}
	if err := app.InitDefaultStore(ctx, app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: pgvdoc.NewRepository(database), ObjectStorage: objects, AllowRegistration: true}); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	store := app.DefaultStore()
	user, err := store.Register("reaudit@example.test", "Reaudit", "Reaudit-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := store.CreateTeam(user.ID, "Reaudit", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(user.ID, team.ID, "Reaudit", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := store.CreateDocument(user.ID, project.ID, "Numeric range", app.DocumentTypeOpenAPI, "api.json", "")
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.ListBranches(user.ID, project.ID, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"openapi":"3.1.0","info":{"title":"Number","version":"1"},"paths":{"/values":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"number","enum":[1e131071,1e-16383]}}}}}}}}}`
	for _, replacement := range []string{`1e200000`, `"\u0000"`} {
		invalid := strings.Replace(raw, "1e131071,1e-16383", replacement, 1)
		if _, err := store.CreateDocumentDraft(user.ID, project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "invalid", SchemaContent: invalid}); !app.Is(err, app.ErrInvalidArgument) {
			t.Fatalf("invalid input was not rejected before persistence: %v", err)
		}
	}

	if _, err := app.ParseOpenAPI(raw); err != nil {
		t.Fatalf("parser: %v", err)
	}
	t.Log("1e131071,1e-16383 passes parser limits")
	draft, err := store.CreateDocumentDraft(user.ID, project.ID, document.ID, app.DraftInput{BranchID: branches[0].ID, VersionName: "v1", SchemaContent: raw})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Log("draft was created successfully")
	if _, err := store.SubmitDocumentDraft(user.ID, project.ID, document.ID, draft.ID); err != nil {
		t.Fatalf("submit: %v", err)
	}
	_, err = store.ReviewDocumentDraft(user.ID, project.ID, document.ID, draft.ID, "approve", reviewInputForTest(t, store, user.ID, project.ID, document.ID, draft.ID))
	t.Logf("publish: %v", err)
	if err != nil {
		t.Errorf("parser accepted a value that cannot be persisted during publication: %v", err)
	}
	store.StopSummaryWorker()
	persisted, err := pgvdoc.NewRepository(database).LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := app.ParseOpenAPI(raw)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(jsonvalue.Normalize(parsed.Endpoints[0].Responses))
	if len(persisted.Endpoints) != 1 {
		t.Fatal("missing published endpoint")
	}
	for _, endpoint := range persisted.Endpoints {
		actual, _ := json.Marshal(jsonvalue.Normalize(endpoint.Responses))
		if string(actual) != string(expected) {
			t.Fatal("JSONB expansion changed canonical numeric identity")
		}
	}

}

type repairPausedRepository struct {
	*pgvdoc.Repository
	pause            atomic.Bool
	captured, resume chan struct{}
}

func (r *repairPausedRepository) StateRevision(ctx context.Context) (string, error) {
	if r.pause.Load() {
		return "force-reaudit-snapshot", nil
	}
	return r.Repository.StateRevision(ctx)
}

func (r *repairPausedRepository) LoadWorkingStateWithRevision(ctx context.Context) (*domain.State, string, error) {
	state, revision, err := r.Repository.LoadWorkingStateWithRevision(ctx)
	if err == nil && r.pause.CompareAndSwap(true, false) {
		close(r.captured)
		select {
		case <-r.resume:
		case <-time.After(5 * time.Second):
		}
	}
	return state, revision, err
}

func TestPostgresRepairRevokeDuringAuthentication(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	ctx := context.Background()
	if err := databasepkg.RunMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	repo := &repairPausedRepository{Repository: pgvdoc.NewRepository(database), captured: make(chan struct{}), resume: make(chan struct{})}
	if err := app.InitDefaultStore(ctx, app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, AllowRegistration: true}); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	store := app.DefaultStore()
	user, err := store.Register("reaudit-auth@example.test", "Reaudit", "Reaudit-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateMCPToken(user.ID, "Concurrent use", []int{app.ScopeAPIRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.StopSummaryWorker()
	for _, admin := range []bool{false, true} {
		repo.captured, repo.resume = make(chan struct{}), make(chan struct{})
		if admin {
			token, err = store.CreateMCPToken(user.ID, "Admin revocation", []int{app.ScopeAPIRead}, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		beforeUse, err := repo.LoadState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		repo.pause.Store(true)
		finished := make(chan error, 1)
		go func() {
			var err error
			if admin {
				_, err = store.RevokeUserMCPToken(user.ID, user.ID, token.ID)
			} else {
				_, err = store.RevokeMCPToken(user.ID, token.ID)
			}
			finished <- err
		}()
		select {
		case <-repo.captured:
		case <-time.After(5 * time.Second):
			t.Fatal("revoke did not load its snapshot")
		}
		used, _, authErr := store.AuthenticateMCPToken(token.Token)
		close(repo.resume)
		revokeErr := <-finished
		t.Logf("concurrent authentication: %v; revoke: %v", authErr, revokeErr)
		if authErr != nil {
			t.Fatal(authErr)
		}
		_, _, after := store.AuthenticateMCPToken(token.Token)
		t.Logf("authentication after revoke request finished: %v", after)
		if revokeErr != nil || !app.Is(after, app.ErrUnauthenticated) {
			t.Error("last-use update prevented revocation; token remains active")
		}
		state, err := repo.LoadState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		persisted := state.Tokens[token.ID]
		if persisted.LastUsedAt == nil || persisted.LastUsedAt.Before(used.LastUsedAt.Truncate(time.Microsecond)) {
			t.Fatal("revocation rolled back last_used_at")
		}
		if !used.UpdatedAt.Equal(beforeUse.Tokens[token.ID].UpdatedAt) {
			t.Fatal("usage changed lifecycle timestamp")
		}
		// 持有旧生命周期版本的写入仍必须失败，不能复活已撤销的令牌。
		if err := repo.UpsertMCPTokenIfUnchanged(ctx, used, used); !app.Is(err, app.ErrFailedPrecondition) {
			t.Fatalf("stale lifecycle overwrite: %v", err)
		}
	}
}
