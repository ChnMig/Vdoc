package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	repo "vdoc/domain/documentrepo"
	domain "vdoc/domain/vdoc"
	app "vdoc/services/vdoc"
)

func TestPostgresCapabilityScopeMigrationKeepsLegacyGrants(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	migrations, err := databasepkg.EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Exec(`CREATE TABLE schema_migrations (version text PRIMARY KEY, name text NOT NULL, checksum text, applied_at timestamptz NOT NULL DEFAULT now())`).Error; err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if migration.Version >= "026" {
			continue
		}
		if err = database.Exec(migration.SQL).Error; err != nil {
			t.Fatal(err)
		}
		if err = database.Exec(`INSERT INTO schema_migrations(version,name,checksum) VALUES(?,?,?)`, migration.Version, migration.Name, migration.Checksum).Error; err != nil {
			t.Fatal(err)
		}
	}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: pgvdoc.NewRepository(database), ObjectStorage: &optimizationObjects{bodies: map[string][]byte{}}, AllowRegistration: true}
	s := mutationTestStore(t, cfg)
	defer app.CloseDefaultStore()
	owner, err := s.Register("legacy-scopes@example.test", "Owner", "Capability-migration-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]int{{1}, {2}, {3}, {4}, {1, 3}, {2, 4}, {1, 2, 3, 4}} {
		if _, err = s.CreateMCPToken(owner.ID, fmt.Sprint(scopes), scopes, nil); err != nil {
			t.Fatal(err)
		}
	}
	type rawGrant struct{ ID, Scopes string }
	var before, after []rawGrant
	if err = database.Raw(`SELECT id, scopes::text AS scopes FROM mcp_tokens ORDER BY id`).Scan(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err = databasepkg.RunMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if err = database.Raw(`SELECT id, scopes::text AS scopes FROM mcp_tokens ORDER BY id`).Scan(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("capability migration changed legacy grants: before=%+v after=%+v err=%v", before, after, err)
	}
	for _, scopes := range [][]int{{5}, {6}, {5, 6}, {1, 6}} {
		if _, err = s.CreateMCPToken(owner.ID, fmt.Sprint(scopes), scopes, nil); err != nil {
			t.Fatalf("new scopes %v: %v", scopes, err)
		}
	}
	for _, invalid := range []int{0, 7, 99} {
		if err = database.Exec(`UPDATE mcp_tokens SET scopes=ARRAY[?]::smallint[] WHERE id=?`, invalid, before[0].ID).Error; err == nil {
			t.Fatalf("database accepted unknown scope %d", invalid)
		}
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	s = mutationTestStore(t, cfg)
	listed, err := s.ListMCPTokens(owner.ID)
	if err != nil || len(listed) != 11 {
		t.Fatalf("scope grants after restart: %+v, %v", listed, err)
	}
	if err = database.Raw(`SELECT id, scopes::text AS scopes FROM mcp_tokens WHERE id IN ? ORDER BY id`, []string{before[0].ID, before[1].ID, before[2].ID, before[3].ID, before[4].ID, before[5].ID, before[6].ID}).Scan(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("legacy grants changed after restart: %+v %v", after, err)
	}
}

func TestPostgresPrivateMCPTokenIsolationSurvivesReload(t *testing.T) {
	_, cfg, _ := mutationTestRepository(t)
	s := mutationTestStore(t, cfg)
	super, err := s.Register("private-super@example.test", "Super", "Private-token-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	owners := []string{super.ID}
	for _, name := range []string{"A", "B"} {
		user, err := s.CreateUser(super.ID, "private-"+name+"@example.test", name, "Private-token-password-2026!", false)
		if err != nil {
			t.Fatal(err)
		}
		owners = append(owners, user.ID)
	}
	tokens := map[string]*app.MCPToken{}
	for _, owner := range owners {
		tokens[owner], err = s.CreateMCPToken(owner, "Private", []int{app.ScopeRead, app.ScopeWrite}, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = app.CloseDefaultStore(); err != nil {
		t.Fatal(err)
	}
	s = mutationTestStore(t, cfg)
	for _, actor := range owners {
		list, err := s.ListMCPTokens(actor)
		if err != nil || len(list) != 1 || list[0].ID != tokens[actor].ID || list[0].Token != "" {
			t.Fatalf("private owner %s list=%+v err=%v", actor, list, err)
		}
		for _, owner := range owners {
			if actor == owner {
				continue
			}
			for name, call := range map[string]func() error{
				"user list":   func() error { _, e := s.ListUserMCPTokens(actor, owner); return e },
				"detail":      func() error { _, e := s.MCPToken(actor, tokens[owner].ID); return e },
				"revoke":      func() error { _, e := s.RevokeMCPToken(actor, tokens[owner].ID); return e },
				"user revoke": func() error { _, e := s.RevokeUserMCPToken(actor, owner, tokens[owner].ID); return e },
				"usage":       func() error { _, e := s.QueryMCPUsage(actor, app.MCPUsageQuery{TokenID: tokens[owner].ID}); return e },
			} {
				if err := call(); !errors.Is(err, app.ErrPermissionDenied) {
					t.Fatalf("%s %s for %s: %v", actor, name, owner, err)
				}
			}
		}
	}
	for _, owner := range owners {
		if token, _, err := s.AuthenticateMCPToken(tokens[owner].Token); err != nil || token.UserID != owner {
			t.Fatalf("denied calls altered owner token: %+v %v", token, err)
		}
	}
}

func TestPostgresLastDocumentMutationKeepsCompleteFolderChainAfterRestart(t *testing.T) {
	dsn := os.Getenv("VDOC_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("VDOC_TEST_DATABASE_DSN not set")
	}
	database := openAIGenerationTestDB(t, dsn)
	defer closeAIGenerationTestDB(t, database)
	resetAIGenerationTestSchema(t, database)
	if err := databasepkg.RunMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: pgvdoc.NewRepository(database), ObjectStorage: &optimizationObjects{bodies: map[string][]byte{}}, AllowRegistration: true}
	s := mutationTestStore(t, cfg)
	defer app.CloseDefaultStore()
	owner, err := s.Register("folder-chain@example.test", "Owner", "Folder-chain-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := s.CreateTeam(owner.ID, "Folder parent chains", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, legacy := range []bool{false, true} {
		for _, operation := range []string{"delete", "move", "move_folder"} {
			t.Run(fmt.Sprintf("%s/inferred=%t", operation, legacy), func(t *testing.T) {
				project, err := s.CreateProject(owner.ID, team.ID, t.Name(), "", owner.ID)
				if err != nil {
					t.Fatal(err)
				}
				created, err := s.RepositoryCreateDocument(owner.ID, project.ID, repo.DocumentInput{Name: "Last document", RelativePath: "Source/Nested/Leaf/file.md", DocumentType: app.DocumentTypeMarkdown, Content: "exact\r\n", IdempotencyKey: "persistent-folder-chain"})
				if err != nil {
					t.Fatal(err)
				}
				before, err := s.ListDocumentFolders(owner.ID, project.ID)
				if err != nil || len(before) != 3 {
					t.Fatalf("created folders=%+v, %v", before, err)
				}
				if legacy {
					if err = database.Exec(`DELETE FROM document_folders WHERE project_id=?`, project.ID).Error; err != nil {
						t.Fatal(err)
					}
					if err = app.CloseDefaultStore(); err != nil {
						t.Fatal(err)
					}
					s = mutationTestStore(t, cfg)
				}
				if operation == "delete" {
					err = s.RepositoryDeleteDocument(owner.ID, project.ID, created.Document.ID)
				} else if operation == "move" {
					target := "root.md"
					_, err = s.RepositoryUpdateDocument(owner.ID, project.ID, created.Document.ID, app.RepositoryDocumentPatchInput{RelativePath: &target})
				} else {
					root := ""
					_, err = s.UpdateDocumentFolderLocation(owner.ID, project.ID, "Source/Nested/Leaf", nil, &root)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = app.CloseDefaultStore(); err != nil {
					t.Fatal(err)
				}
				s = mutationTestStore(t, cfg)
				folders, err := s.ListDocumentFolders(owner.ID, project.ID)
				if err != nil || len(folders) != 3 {
					t.Fatalf("folder chain after restart=%+v, %v", folders, err)
				}
				byPath := map[string]*app.DocumentFolder{}
				for _, folder := range folders {
					byPath[folder.Path] = folder
				}
				for _, relation := range [][2]string{{"Source", ""}, {"Source/Nested", "Source"}, {"Source/Nested/Leaf", "Source/Nested"}} {
					if operation == "move_folder" && relation[0] == "Source/Nested/Leaf" {
						relation = [2]string{"Leaf", ""}
					}
					folder := byPath[relation[0]]
					parentID := ""
					if relation[1] != "" && byPath[relation[1]] != nil {
						parentID = byPath[relation[1]].ID
					}
					if folder == nil || folder.ID == "" || folder.ParentID != parentID {
						t.Fatalf("broken persisted chain %v folder=%+v all=%+v", relation, folder, byPath)
					}
				}
				if !legacy {
					for _, old := range before {
						path := old.Path
						if operation == "move_folder" && path == "Source/Nested/Leaf" {
							path = "Leaf"
						}
						if byPath[path] == nil || byPath[path].ID != old.ID {
							t.Fatalf("persisted folder identity changed: %+v", old)
						}
					}
				}
				if err = s.DeleteDocumentFolder(owner.ID, project.ID, "Source"); !errors.Is(err, app.ErrFailedPrecondition) {
					t.Fatalf("deleted parent containing child: %v", err)
				}
				if operation != "move_folder" {
					if err = s.DeleteDocumentFolder(owner.ID, project.ID, "Source/Nested/Leaf"); err != nil {
						t.Fatalf("empty leaf cannot be deleted after restart: %v", err)
					}
				}
			})
		}
	}
}

func TestPostgresPrivateTokenManagementRechecksOwnershipAtCommit(t *testing.T) {
	port, cfg, _ := mutationTestRepository(t)
	bootstrap := mutationTestStore(t, cfg)
	owner, err := bootstrap.Register("token-race-owner@example.test", "Owner", "Token-race-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	other, err := bootstrap.CreateUser(owner.ID, "token-race-other@example.test", "Other", "Token-race-password-2026!", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"reveal", "inactive_detail", "revoke", "user_revoke"} {
		t.Run(operation, func(t *testing.T) {
			barrier := &mutationTransactionBarrier{Repository: port, reached: make(chan struct{}), resume: make(chan struct{})}
			firstCfg := cfg
			firstCfg.DatabaseRepository = barrier
			first := mutationTestStore(t, firstCfg)
			token, err := first.CreateMCPToken(owner.ID, operation, []int{app.ScopeRead, app.ScopeWrite}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if operation == "inactive_detail" {
				if _, err = first.RevokeMCPToken(owner.ID, token.ID); err != nil {
					t.Fatal(err)
				}
			}
			resume := pauseMutation(t, barrier, first, func(_ context.Context, store *app.Store) error {
				var result *app.MCPToken
				var callErr error
				switch operation {
				case "reveal", "inactive_detail":
					result, callErr = store.MCPToken(owner.ID, token.ID)
				case "revoke":
					result, callErr = store.RevokeMCPToken(owner.ID, token.ID)
				case "user_revoke":
					result, callErr = store.RevokeUserMCPToken(owner.ID, owner.ID, token.ID)
				}
				if result != nil {
					return fmt.Errorf("stale ownership returned token metadata or plaintext")
				}
				return callErr
			})
			state, err := port.LoadState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			target := state.Tokens[token.ID]
			target.UserID = other.ID
			if err = port.UpsertMCPToken(context.Background(), target); err != nil {
				t.Fatal(err)
			}
			assertMutationRejectedWithoutWrites(t, port, resume, app.ErrPermissionDenied)
		})
	}
}

func TestPostgresCapabilityScopeSnapshotRechecksPartialRevocationAtCommit(t *testing.T) {
	port, cfg, _ := mutationTestRepository(t)
	firstBarrier := &mutationTransactionBarrier{Repository: port, reached: make(chan struct{}), resume: make(chan struct{})}
	firstCfg := cfg
	firstCfg.DatabaseRepository = firstBarrier
	first := mutationTestStore(t, firstCfg)
	owner, err := first.Register("capability-race@example.test", "Owner", "Capability-race-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	team, err := first.CreateTeam(owner.ID, "Capability race", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := first.CreateProject(owner.ID, team.ID, "Capability race", "", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	created, err := first.RepositoryCreateDocument(owner.ID, project.ID, repo.DocumentInput{Name: "Original", RelativePath: "original.txt", DocumentType: app.DocumentTypeText, Content: "original", IdempotencyKey: "capability-snapshot-create"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := first.CreateMCPToken(owner.ID, "Snapshot", []int{app.ScopeRead, app.ScopeWrite}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 写 scope 尚在，但认证时的 read grant 已撤销，原请求仍须拒绝。
	resume := pauseMutation(t, firstBarrier, first, func(ctx context.Context, store *app.Store) error {
		agent := store.WithContext(app.WithMCPToken(ctx, token.ID, app.ScopeRead, app.ScopeWrite)).WithRepositoryDocument(created.Document.ID)
		_, e := agent.RepositorySave(owner.ID, project.ID, created.Branch.ID, repo.SaveInput{ExpectedHeadCommitID: created.Commit.ID, ExpectedGeneration: 1, IdempotencyKey: "capability-stale-snapshot", Changes: []repo.Change{{Operation: "upsert", DocumentID: created.Document.ID, RelativePath: "original.txt", Name: "Original", DocumentType: app.DocumentTypeText, Content: "denied"}}})
		return e
	})
	state, err := port.LoadState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changed := *state.Tokens[token.ID]
	changed.Scopes = []int{app.ScopeWrite}
	if err = port.UpsertMCPToken(context.Background(), (*domain.MCPToken)(&changed)); err != nil {
		t.Fatal(err)
	}
	assertMutationRejectedWithoutWrites(t, port, resume, app.ErrPermissionDenied)
}
