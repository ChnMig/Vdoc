package db_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
)

type mutationTransactionBarrier struct {
	*pgvdoc.Repository
	armed   atomic.Bool
	reached chan struct{}
	resume  chan struct{}
}

func (r *mutationTransactionBarrier) WithinTransaction(ctx context.Context, fn func(domain.Repository) error) error {
	if r.armed.CompareAndSwap(true, false) {
		close(r.reached)
		select {
		case <-r.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.Repository.WithinTransaction(ctx, fn)
}

func mutationTestRepository(t *testing.T) (*pgvdoc.Repository, app.RuntimeConfig, *optimizationObjects) {
	t.Helper()
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	t.Cleanup(func() { closeAIGenerationTestDB(t, database) })
	resetAIGenerationTestSchema(t, database)
	if err := databasepkg.RunMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := pgvdoc.NewRepository(database)
	objects := &optimizationObjects{bodies: map[string][]byte{}}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, ObjectStorage: objects, AllowRegistration: true}
	t.Cleanup(func() {
		if err := app.CloseDefaultStore(); err != nil {
			t.Error(err)
		}
	})
	return repo, cfg, objects
}

func mutationTestStore(t *testing.T, cfg app.RuntimeConfig) *app.Store {
	t.Helper()
	if err := app.InitDefaultStore(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	store := app.DefaultStore()
	store.StopSummaryWorker()
	return store
}

func pauseMutation(t *testing.T, barrier *mutationTransactionBarrier, store *app.Store, operation func(context.Context, *app.Store) error) func() error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	barrier.armed.Store(true)
	var once sync.Once
	resume := func() { once.Do(func() { close(barrier.resume) }) }
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() { defer close(done); result <- operation(ctx, store.WithContext(ctx)) }()
	t.Cleanup(func() {
		cancel()
		resume()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("mutation goroutine did not stop")
		}
	})
	select {
	case <-barrier.reached:
	case err := <-result:
		t.Fatalf("operation stopped before transaction: %v", err)
	case <-ctx.Done():
		t.Fatal("operation did not reach transaction barrier")
	}
	return func() error {
		resume()
		select {
		case err := <-result:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func assertMutationRejectedWithoutWrites(t *testing.T, repo *pgvdoc.Repository, resume func() error, want error) {
	t.Helper()
	before, err := repo.LoadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := resume(); !errors.Is(err, want) || errors.Is(err, domain.ErrCommitOutcomeUnknown) {
		t.Fatalf("mutation error = %v, want definite %v", err, want)
	}
	after, err := repo.LoadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejected operation changed persistent rows or audit records")
	}
}

func TestPostgresOrdinaryWritesRecheckMutationContext(t *testing.T) {
	repo, cfg, objects := mutationTestRepository(t)
	bootstrap := mutationTestStore(t, cfg)
	admin, err := bootstrap.Register("guard-admin@example.test", "Admin", "Mutation-guard-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	actor, err := bootstrap.CreateUser(admin.ID, "guard-actor@example.test", "Actor", "Mutation-guard-password-2026!", false)
	if err != nil {
		t.Fatal(err)
	}
	team, err := bootstrap.CreateTeam(admin.ID, "Mutation guards", "")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ operation, change string }{}
	for _, operation := range []string{"create", "update", "submit"} {
		for _, change := range []string{"project-archived", "document-archived", "branch-archived", "actor-demoted", "token-revoked"} {
			cases = append(cases, struct{ operation, change string }{operation, change})
		}
	}
	for _, operation := range []string{"reject", "request-changes", "promote"} {
		cases = append(cases, struct{ operation, change string }{operation, "actor-demoted"})
	}
	cases = append(cases, struct{ operation, change string }{"promote", "source-archived"})
	for _, markdown := range []bool{false, true} {
		name, kind := "openapi", app.DocumentTypeOpenAPI
		body := `{"openapi":"3.1.0","info":{"title":"Guard","version":"1"},"paths":{"/guard":{"get":{"responses":{"200":{"description":"ok"}}}}}}`
		if markdown {
			name, kind, body = "markdown", app.DocumentTypeMarkdown, "# Guard\n"
		}
		for _, tc := range cases {
			t.Run(name+"/"+tc.operation+"/"+tc.change, func(t *testing.T) {
				barrier := &mutationTransactionBarrier{Repository: repo, reached: make(chan struct{}), resume: make(chan struct{})}
				firstCfg := cfg
				firstCfg.DatabaseRepository = barrier
				first := mutationTestStore(t, firstCfg)
				project, err := first.CreateProject(admin.ID, team.ID, t.Name(), "", admin.ID)
				if err != nil {
					t.Fatal(err)
				}
				role := app.MemberRoleWriter
				if tc.operation == "reject" || tc.operation == "request-changes" || tc.operation == "promote" {
					role = app.MemberRoleAdmin
				}
				if _, err := first.AddProjectMember(admin.ID, project.ID, actor.ID, role); err != nil {
					t.Fatal(err)
				}
				document, err := first.CreateDocument(admin.ID, project.ID, "Guard", kind, "guard/document.md", "")
				if err != nil {
					t.Fatal(err)
				}
				branches, err := first.ListBranches(admin.ID, project.ID, document.ID)
				if err != nil {
					t.Fatal(err)
				}
				branchID, sourceID := "", ""
				for _, branch := range branches {
					if branch.Name == "test" {
						branchID = branch.ID
					}
					if branch.Name == "staging" {
						sourceID = branch.ID
					}
				}
				if branchID == "" {
					t.Fatal("test branch missing")
				}
				if sourceID == "" {
					branch, err := first.CreateBranch(admin.ID, project.ID, document.ID, "feature/guard-source", "")
					if err != nil {
						t.Fatal(err)
					}
					sourceID = branch.ID
				}
				create, submit, review := first.CreateDraft, first.SubmitDraft, first.ReviewDraft
				if markdown {
					create, submit, review = first.CreateMarkdownDraft, first.SubmitMarkdownDraft, first.ReviewMarkdownDraft
				}
				draftBranch := branchID
				if tc.operation == "promote" {
					draftBranch = sourceID
				}
				draft, err := create(actor.ID, project.ID, document.ID, app.DraftInput{BranchID: draftBranch, VersionName: "1.0.0", SchemaContent: body})
				if err != nil {
					t.Fatal(err)
				}
				if tc.operation == "reject" || tc.operation == "request-changes" || tc.operation == "promote" {
					if _, err := submit(actor.ID, project.ID, document.ID, draft.ID); err != nil {
						t.Fatal(err)
					}
					first.StopSummaryWorker()
					draft, _, err = first.ReadDraftContent(actor.ID, project.ID, document.ID, draft.ID, "raw")
					if err != nil {
						t.Fatal(err)
					}
					if tc.operation == "promote" {
						if _, err := review(actor.ID, project.ID, document.ID, draft.ID, "approve", app.DraftReviewInput{ExpectedReviewRevision: draft.ReviewRevision()}); err != nil {
							t.Fatal(err)
						}
						first.StopSummaryWorker()
					}
				}
				tokenID := ""
				if tc.change == "token-revoked" {
					scope := app.ScopeAPIDraft
					if markdown {
						scope = app.ScopeDocDraft
					}
					token, err := first.CreateMCPToken(actor.ID, "in-flight", []int{scope}, nil)
					if err != nil {
						t.Fatal(err)
					}
					tokenID = token.ID
				}
				second := mutationTestStore(t, cfg)
				beforeObjects := objectRaceObjectKeys(objects)
				resume := pauseMutation(t, barrier, first, func(ctx context.Context, store *app.Store) error {
					if tokenID != "" {
						store = store.WithContext(app.WithMCPToken(ctx, tokenID))
					}
					create, update, submit, review := store.CreateDraft, store.UpdateDraft, store.SubmitDraft, store.ReviewDraft
					if markdown {
						create, update, submit, review = store.CreateMarkdownDraft, store.UpdateMarkdownDraft, store.SubmitMarkdownDraft, store.ReviewMarkdownDraft
					}
					var operationErr error
					switch tc.operation {
					case "create":
						_, operationErr = create(actor.ID, project.ID, document.ID, app.DraftInput{BranchID: branchID, VersionName: "2.0.0", SchemaContent: body})
					case "update":
						note := "new note"
						_, operationErr = update(actor.ID, project.ID, document.ID, draft.ID, app.DraftPatchInput{ExpectedRevision: draft.Revision(), SchemaContent: body, Changelog: &note})
					case "submit":
						_, operationErr = submit(actor.ID, project.ID, document.ID, draft.ID)
					case "reject", "request-changes":
						_, operationErr = review(actor.ID, project.ID, document.ID, draft.ID, tc.operation, app.DraftReviewInput{ExpectedReviewRevision: draft.ReviewRevision()})
					case "promote":
						_, operationErr = store.PromoteDraft(actor.ID, project.ID, document.ID, app.PromoteInput{SourceBranchID: sourceID, TargetBranchID: branchID, VersionName: "2.0.0"})
					}
					return operationErr
				})
				want := app.ErrFailedPrecondition
				switch tc.change {
				case "project-archived":
					_, err = second.ArchiveProject(admin.ID, project.ID)
				case "document-archived":
					_, err = second.ArchiveDocument(admin.ID, project.ID, document.ID)
				case "branch-archived":
					_, err = second.ArchiveBranch(admin.ID, project.ID, document.ID, branchID)
				case "source-archived":
					_, err = second.ArchiveBranch(admin.ID, project.ID, document.ID, sourceID)
				case "actor-demoted":
					_, err = second.PatchProjectMemberRole(admin.ID, project.ID, actor.ID, role-1)
					want = app.ErrPermissionDenied
				case "token-revoked":
					_, err = second.RevokeMCPToken(actor.ID, tokenID)
					want = app.ErrUnauthenticated
				}
				if err != nil {
					t.Fatalf("changing context: %v", err)
				}
				assertMutationRejectedWithoutWrites(t, repo, resume, want)
				if !reflect.DeepEqual(beforeObjects, objectRaceObjectKeys(objects)) {
					t.Fatal("failed mutation did not clean only its own objects")
				}
			})
		}
	}
}
