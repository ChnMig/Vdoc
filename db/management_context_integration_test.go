package db_test

import (
	"context"
	"testing"

	domainshare "vdoc/domain/documentshare"
	app "vdoc/services/vdoc"
)

func TestPostgresManagementRechecksCurrentContext(t *testing.T) {
	repo, cfg, _ := mutationTestRepository(t)
	bootstrap := mutationTestStore(t, cfg)
	admin, err := bootstrap.Register("management-guard-admin@example.test", "Admin", "Management-guard-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	actor, err := bootstrap.CreateUser(admin.ID, "management-guard-actor@example.test", "Actor", "Management-guard-password-2026!", false)
	if err != nil {
		t.Fatal(err)
	}
	team, err := bootstrap.CreateTeam(admin.ID, "Management guards", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		operation, change string
		want              error
	}{
		{"token_reveal", "token_revoked", app.ErrFailedPrecondition},
		{"share_reveal", "share_revoked", app.ErrFailedPrecondition},
		{"share_create", "document_archived", app.ErrFailedPrecondition},
		{"share_create", "actor_demoted", app.ErrPermissionDenied},
		{"share_revoke", "document_archived", nil},
	} {
		t.Run(test.operation+"/"+test.change, func(t *testing.T) {
			barrier := &mutationTransactionBarrier{Repository: repo, reached: make(chan struct{}), resume: make(chan struct{})}
			firstCfg := cfg
			firstCfg.DatabaseRepository = barrier
			first := mutationTestStore(t, firstCfg)
			project, err := first.CreateProject(admin.ID, team.ID, t.Name(), "", admin.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := first.AddProjectMember(admin.ID, project.ID, actor.ID, app.MemberRoleAdmin); err != nil {
				t.Fatal(err)
			}
			document, err := first.CreateDocument(admin.ID, project.ID, "Management guard", app.DocumentTypeMarkdown, "guard/management.md", "")
			if err != nil {
				t.Fatal(err)
			}
			branches, err := first.ListBranches(admin.ID, project.ID, document.ID)
			if err != nil {
				t.Fatal(err)
			}
			branchID := ""
			for _, branch := range branches {
				if branch.Name == "test" {
					branchID = branch.ID
				}
			}
			if branchID == "" {
				t.Fatal("test branch missing")
			}
			draft, err := first.CreateMarkdownDraft(actor.ID, project.ID, document.ID, app.DraftInput{BranchID: branchID, VersionName: "1.0.0", SchemaContent: "# Management guard"})
			if err != nil {
				t.Fatal(err)
			}
			draft, err = first.SubmitMarkdownDraft(actor.ID, project.ID, document.ID, draft.ID)
			if err != nil {
				t.Fatal(err)
			}
			first.StopSummaryWorker()
			draft, _, err = first.ReadDraftContent(admin.ID, project.ID, document.ID, draft.ID, "raw")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := first.ReviewMarkdownDraft(admin.ID, project.ID, document.ID, draft.ID, "approve", app.DraftReviewInput{ExpectedReviewRevision: draft.ReviewRevision()}); err != nil {
				t.Fatal(err)
			}
			first.StopSummaryWorker()
			shareInput := app.DocumentShareInput{BranchID: branchID, VersionScope: app.DocumentShareScopeLatest, ExpiryPreset: domainshare.ExpiryPresetPermanent}
			share, err := first.CreateDocumentShare(actor.ID, project.ID, document.ID, shareInput)
			if err != nil {
				t.Fatal(err)
			}
			token, err := first.CreateMCPToken(actor.ID, "management-reveal", []int{app.ScopeDocRead}, nil)
			if err != nil {
				t.Fatal(err)
			}
			second := mutationTestStore(t, cfg)
			returnedResult := false
			resume := pauseMutation(t, barrier, first, func(_ context.Context, store *app.Store) error {
				switch test.operation {
				case "token_reveal":
					result, err := store.MCPToken(actor.ID, token.ID)
					returnedResult = result != nil
					return err
				case "share_reveal":
					result, err := store.RevealDocumentShare(actor.ID, project.ID, document.ID, share.Share.ID)
					returnedResult = result != nil
					return err
				case "share_create":
					result, err := store.CreateDocumentShare(actor.ID, project.ID, document.ID, shareInput)
					returnedResult = result != nil
					return err
				default:
					result, err := store.RevokeDocumentShare(actor.ID, project.ID, document.ID, share.Share.ID)
					returnedResult = result != nil
					return err
				}
			})
			switch test.change {
			case "token_revoked":
				_, err = second.RevokeMCPToken(actor.ID, token.ID)
			case "share_revoked":
				_, err = second.RevokeDocumentShare(admin.ID, project.ID, document.ID, share.Share.ID)
			case "document_archived":
				_, err = second.ArchiveDocument(admin.ID, project.ID, document.ID)
			case "actor_demoted":
				_, err = second.PatchProjectMemberRole(admin.ID, project.ID, actor.ID, app.MemberRoleWriter)
			}
			if err != nil {
				t.Fatalf("concurrent context change: %v", err)
			}
			if test.want != nil {
				assertMutationRejectedWithoutWrites(t, repo, resume, test.want)
				if returnedResult {
					t.Fatal("rejected management operation returned a result or secret")
				}
				return
			}
			if err := resume(); err != nil || !returnedResult {
				t.Fatalf("revocation after archive returned=%t err=%v", returnedResult, err)
			}
			state, err := repo.LoadState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if state.Shares[share.Share.ID].Status != app.DocumentShareStatusRevoked || state.APIServices[document.ID].Status != app.DocumentStatusArchived {
				t.Fatal("archive or allowed revocation was not persisted")
			}
		})
	}
}
