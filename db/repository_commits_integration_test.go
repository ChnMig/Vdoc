package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
)

func TestPostgresRepositoryHistoryIsImmutableAndReferencesUseCAS(t *testing.T) {
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
	port := pgvdoc.NewRepository(database)
	objects := &optimizationObjects{bodies: map[string][]byte{}}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: port, ObjectStorage: objects, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	s := app.DefaultStore()
	user, err := s.Register("repository-history@example.test", "Owner", "Repository-history-test-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.CreateTeam(user.ID, "Immutable history", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(user.ID, team.ID, "Immutable history", "", user.ID)
	if err != nil {
		t.Fatal(err)
	}
	branches, err := s.RepositoryBranches(user.ID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.RepositoryCreateBranch(user.ID, project.ID, repo.BranchInput{Name: "work", FromCommitID: branches[0].HeadCommitID})
	if err != nil {
		t.Fatal(err)
	}
	input := repo.SaveInput{ExpectedHeadCommitID: b.HeadCommitID, ExpectedGeneration: b.Generation, IdempotencyKey: "persistent-first", Changes: []repo.Change{{Operation: "upsert", Name: "Exact text", RelativePath: "one.txt", DocumentType: app.DocumentTypeText, Content: "Original\r\nbytes\r\n"}}}
	first, err := s.RepositorySave(user.ID, project.ID, b.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	doc := first.Files[0].DocumentID
	second, err := s.RepositorySave(user.ID, project.ID, b.ID, repo.SaveInput{ExpectedHeadCommitID: first.ID, ExpectedGeneration: 2, IdempotencyKey: "persistent-second", Changes: []repo.Change{{Operation: "upsert", DocumentID: doc, Name: "Exact text", RelativePath: "one.txt", DocumentType: app.DocumentTypeText, Content: "Changed\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.RepositoryPublish(user.ID, project.ID, repo.PublishInput{BranchID: b.ID, CommitID: second.ID, ExpectedGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"commit", "event", "release", "idempotency"} {
		if err = database.Exec("UPDATE repository_records SET generation = generation + 1 WHERE project_id = ? AND kind = ?", project.ID, kind).Error; err == nil {
			t.Fatalf("SQL mutated immutable %s", kind)
		}
		if err = database.Exec("DELETE FROM repository_records WHERE project_id = ? AND kind = ?", project.ID, kind).Error; err == nil {
			t.Fatalf("SQL deleted immutable %s", kind)
		}
	}
	// 两个独立数据库事务使用相同 generation；只有一个移动分支引用成功。
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- port.WithinTransaction(ctx, func(tx domain.Repository) error {
				storage := tx.(repo.Storage)
				if e := storage.LockRepository(ctx, project.ID); e != nil {
					return e
				}
				row, e := storage.RepositoryRecord(ctx, project.ID, "branch", b.ID)
				if e != nil {
					return e
				}
				var branch repo.Branch
				if e = json.Unmarshal(row.Payload, &branch); e != nil {
					return e
				}
				branch.Generation = 4
				raw, _ := json.Marshal(branch)
				expected := int64(3)
				return storage.PutRepositoryRecord(ctx, repo.Record{ProjectID: project.ID, Kind: "branch", ID: b.ID, Generation: 4, Payload: raw}, &expected)
			})
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for e := range results {
		if e == nil {
			winners++
		} else if !errors.Is(e, domain.ErrFailedPrecondition) {
			t.Fatal(e)
		}
	}
	if winners != 1 {
		t.Fatalf("CAS transaction winners=%d", winners)
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	if err = app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	s = app.DefaultStore()
	old, err := s.RepositoryContent(user.ID, project.ID, first.ID, doc)
	if err != nil || old.Content != "Original\r\nbytes\r\n" {
		t.Fatalf("old snapshot reload=%+v %v", old, err)
	}
	releases, err := s.RepositoryReleases(user.ID, project.ID)
	if err != nil || len(releases) != 1 || releases[0].ID != release.ID || releases[0].CommitID != second.ID {
		t.Fatalf("release reload=%+v %v", releases, err)
	}
	retry, err := s.RepositorySave(user.ID, project.ID, b.ID, input)
	if err != nil || retry.ID != first.ID {
		t.Fatalf("persistent idempotent retry=%+v %v", retry, err)
	}
}

func TestPostgresDocumentRepositoriesSurviveRestartAndIsolateNames(t *testing.T) {
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
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: pgvdoc.NewRepository(database), ObjectStorage: &optimizationObjects{bodies: map[string][]byte{}}, AllowRegistration: true}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	s := app.DefaultStore()
	u, err := s.Register("document-branches@example.test", "Owner", "Document-branches-test-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.CreateTeam(u.ID, "Documents", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(u.ID, team.ID, "Documents", "", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	create := func(name string) *repo.CreatedDocument {
		t.Helper()
		d, e := s.RepositoryCreateDocument(u.ID, p.ID, repo.DocumentInput{Name: name, RelativePath: name + ".txt", DocumentType: 3, Content: "original\r\n", IdempotencyKey: "new-document-" + name})
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	a, b := create("A"), create("B")
	if a.Commit.VersionName != "main.1" || b.Commit.VersionName != "main.1" {
		t.Fatal("independent initial numbers")
	}
	scoped := s.WithRepositoryDocument(a.Document.ID)
	input := repo.SaveInput{ExpectedHeadCommitID: a.Commit.ID, ExpectedGeneration: 1, IdempotencyKey: "persistent-doc-next", Changes: []repo.Change{{Operation: "upsert", DocumentID: a.Document.ID, Name: "A", RelativePath: "A.txt", DocumentType: 3, Content: "second"}}}
	saved, err := scoped.RepositorySave(u.ID, p.ID, a.Branch.ID, input)
	if err != nil || saved.VersionName != "main.2" {
		t.Fatalf("save=%+v %v", saved, err)
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	if err = app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	s = app.DefaultStore()
	scoped = s.WithRepositoryDocument(a.Document.ID)
	history, err := scoped.RepositoryHistory(u.ID, p.ID, a.Branch.ID, "", "")
	if err != nil || len(history.Commits) != 2 || history.Commits[0].VersionName != "main.2" {
		t.Fatalf("restart history=%+v %v", history, err)
	}
	retry, err := scoped.RepositorySave(u.ID, p.ID, a.Branch.ID, input)
	if err != nil || retry.ID != saved.ID {
		t.Fatal("restart retry", err)
	}
	branches, err := s.WithRepositoryDocument(b.Document.ID).RepositoryBranches(u.ID, p.ID)
	if err != nil || len(branches) != 1 || branches[0].Revision != 1 {
		t.Fatalf("second document=%+v %v", branches, err)
	}
	catalog, err := s.RepositoryDocuments(u.ID, p.ID)
	if err != nil || len(catalog) != 2 {
		t.Fatalf("restart catalog=%+v %v", catalog, err)
	}
	if _, err = scoped.RepositoryContent(u.ID, p.ID, b.Commit.ID, b.Document.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatal("cross-document content", err)
	}
}
