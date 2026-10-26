package db_test

import (
	"context"
	"os"
	"testing"

	databasepkg "vdoc/db"
	pgvdoc "vdoc/db/pgdb/vdoc"
	app "vdoc/services/vdoc"
)

func TestPostgresDefaultBranchSwitchWorksInBothIDOrders(t *testing.T) {
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
	const (
		userID     = "11111111111111111111111111111111"
		teamID     = "22222222222222222222222222222222"
		projectID  = "33333333333333333333333333333333"
		documentID = "44444444444444444444444444444444"
		lowID      = "00000000000000000000000000000001"
		highID     = "ffffffffffffffffffffffffffffffff"
	)
	for _, statement := range []string{
		`INSERT INTO users(id,email,password_hash,display_name,is_super_admin) VALUES('` + userID + `','default-branch@example.test','unused-test-hash','Admin',true)`,
		`INSERT INTO teams(id,name,slug,created_by) VALUES('` + teamID + `','Branches','branches','` + userID + `')`,
		`INSERT INTO projects(id,team_id,name,slug,created_by) VALUES('` + projectID + `','` + teamID + `','Branches','branches','` + userID + `')`,
		`INSERT INTO project_members(project_id,user_id,role,status,added_by) VALUES('` + projectID + `','` + userID + `',3,1,'` + userID + `')`,
		`INSERT INTO documents(id,project_id,name,document_type,relative_path,created_by) VALUES('` + documentID + `','` + projectID + `','Branch defaults',3,'notes.txt','` + userID + `')`,
		`INSERT INTO document_branches(id,document_id,name,kind,is_default,created_by) VALUES('` + lowID + `','` + documentID + `','low',2,false,'` + userID + `'),('` + highID + `','` + documentID + `','high',2,true,'` + userID + `')`,
	} {
		if err := database.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := pgvdoc.NewRepository(database)
	cfg := app.RuntimeConfig{DatabaseEnabled: true, DatabaseRepository: repo, ObjectStorage: &optimizationObjects{bodies: map[string][]byte{}}}
	if err := app.InitDefaultStore(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	defer app.CloseDefaultStore()
	store := app.DefaultStore()
	store.StopSummaryWorker()
	makeDefault := true
	// 从大 ID 到小 ID 是原先的失败路径；再反向切换证明两种排序都可持久化。
	for _, targetID := range []string{lowID, highID, lowID} {
		branch, err := store.UpdateBranch(userID, projectID, documentID, targetID, app.BranchPatchInput{IsDefault: &makeDefault})
		if err != nil {
			t.Fatalf("switch default to %s: %v", targetID, err)
		}
		if branch.ID != targetID || !branch.IsDefault {
			t.Fatalf("updated branch = %+v", branch)
		}
		state, err := repo.LoadState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defaultCount := 0
		for id, candidate := range state.Branches {
			if candidate.DocumentID == documentID && candidate.IsDefault {
				defaultCount++
				if id != targetID {
					t.Fatalf("wrong persisted default %s, want %s", id, targetID)
				}
			}
		}
		if defaultCount != 1 {
			t.Fatalf("persisted default count = %d, want 1", defaultCount)
		}
	}
}
