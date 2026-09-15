package db_test

import (
	"context"
	"errors"
	"testing"

	app "vdoc/services/vdoc"
)

func TestPostgresRegistrationDecidesInitialAdminInsideTransaction(t *testing.T) {
	for _, firstSeed := range []bool{false, true} {
		for _, secondSeed := range []bool{false, true} {
			name := "register-after-register"
			if firstSeed {
				name = "seed-after-register"
			}
			if secondSeed {
				name = "register-after-seed"
				if firstSeed {
					name = "seed-after-seed"
				}
			}
			t.Run(name, func(t *testing.T) {
				repo, cfg, _ := mutationTestRepository(t)
				barrier := &mutationTransactionBarrier{Repository: repo, reached: make(chan struct{}), resume: make(chan struct{})}
				firstCfg := cfg
				firstCfg.DatabaseRepository = barrier
				first := mutationTestStore(t, firstCfg)
				second := mutationTestStore(t, cfg)
				var registered *app.User
				resume := pauseMutation(t, barrier, first, func(_ context.Context, store *app.Store) error {
					if firstSeed {
						return store.SeedInitialAdmin("first@example.test", "First", "First-initial-admin-2026!")
					}
					var err error
					registered, err = store.Register("first@example.test", "First", "First-initial-admin-2026!")
					return err
				})
				if secondSeed {
					if err := second.SeedInitialAdmin("second@example.test", "Second", "Second-initial-admin-2026!"); err != nil {
						t.Fatal(err)
					}
				} else {
					user, err := second.Register("second@example.test", "Second", "Second-initial-admin-2026!")
					if err != nil || !user.IsSuperAdmin {
						t.Fatalf("first committed registration: user=%v err=%v", user, err)
					}
				}
				if err := resume(); err != nil {
					t.Fatal(err)
				}
				state, err := repo.LoadState(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				wantUsers := 2
				if firstSeed {
					wantUsers = 1
				}
				if len(state.Users) != wantUsers {
					t.Fatalf("users=%d, want %d", len(state.Users), wantUsers)
				}
				superAdmins, registrationAudits := 0, 0
				for _, user := range state.Users {
					if user.IsSuperAdmin {
						superAdmins++
					}
				}
				for _, audit := range state.AuditLogs {
					if audit.Action == "user.register" {
						registrationAudits++
					}
				}
				if superAdmins != 1 || registrationAudits != wantUsers {
					t.Fatalf("superadmins=%d registration audits=%d", superAdmins, registrationAudits)
				}
				if !firstSeed && (registered == nil || registered.IsSuperAdmin || state.Users[registered.ID].IsSuperAdmin) {
					t.Fatal("stale registration returned or persisted initial-admin privileges")
				}
			})
		}
	}
}

func TestPostgresManagementWritesRecheckActorsAndAdminTargets(t *testing.T) {
	repo, cfg, _ := mutationTestRepository(t)
	bootstrap := mutationTestStore(t, cfg)
	root, err := bootstrap.Register("root-management@example.test", "Root", "Management-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{"grant-superadmin-after-demotion", "archive-team-after-demotion", "login-after-disable", "add-member-after-demotion", "add-admin-after-target-disabled", "promote-admin-after-target-disabled", "create-project-after-target-disabled"}
	for _, scenario := range cases {
		t.Run(scenario, func(t *testing.T) {
			barrier := &mutationTransactionBarrier{Repository: repo, reached: make(chan struct{}), resume: make(chan struct{})}
			firstCfg := cfg
			firstCfg.DatabaseRepository = barrier
			first := mutationTestStore(t, firstCfg)
			actorSuper := scenario == "grant-superadmin-after-demotion" || scenario == "archive-team-after-demotion"
			actor, err := first.CreateUser(root.ID, scenario+"-actor@example.test", "Actor", "Management-password-2026!", actorSuper)
			if err != nil {
				t.Fatal(err)
			}
			target, err := first.CreateUser(root.ID, scenario+"-target@example.test", "Target", "Management-password-2026!", false)
			if err != nil {
				t.Fatal(err)
			}
			team, err := first.CreateTeam(root.ID, scenario, "")
			if err != nil {
				t.Fatal(err)
			}
			projectID := ""
			if scenario == "add-member-after-demotion" || scenario == "add-admin-after-target-disabled" || scenario == "promote-admin-after-target-disabled" {
				project, err := first.CreateProject(root.ID, team.ID, scenario, "", root.ID)
				if err != nil {
					t.Fatal(err)
				}
				projectID = project.ID
				if _, err := first.AddProjectMember(root.ID, project.ID, actor.ID, app.MemberRoleAdmin); err != nil {
					t.Fatal(err)
				}
				if scenario == "promote-admin-after-target-disabled" {
					if _, err := first.AddProjectMember(root.ID, project.ID, target.ID, app.MemberRoleReader); err != nil {
						t.Fatal(err)
					}
				}
			}
			second := mutationTestStore(t, cfg)
			resume := pauseMutation(t, barrier, first, func(_ context.Context, store *app.Store) error {
				var err error
				switch scenario {
				case "grant-superadmin-after-demotion":
					super := true
					_, err = store.PatchUser(actor.ID, target.ID, nil, &super)
				case "archive-team-after-demotion":
					_, err = store.ArchiveTeam(actor.ID, team.ID)
				case "login-after-disable":
					_, err = store.Login(actor.Email, "Management-password-2026!")
				case "add-member-after-demotion":
					_, err = store.AddProjectMember(actor.ID, projectID, target.ID, app.MemberRoleReader)
				case "add-admin-after-target-disabled":
					_, err = store.AddProjectMember(actor.ID, projectID, target.ID, app.MemberRoleAdmin)
				case "promote-admin-after-target-disabled":
					_, err = store.PatchProjectMemberRole(actor.ID, projectID, target.ID, app.MemberRoleAdmin)
				case "create-project-after-target-disabled":
					_, err = store.CreateProject(root.ID, team.ID, "late project", "", target.ID)
				}
				return err
			})
			want := app.ErrPermissionDenied
			switch scenario {
			case "grant-superadmin-after-demotion", "archive-team-after-demotion":
				super := false
				_, err = second.PatchUser(root.ID, actor.ID, nil, &super)
			case "login-after-disable":
				disabled := app.UserStatusDisabled
				_, err = second.PatchUser(root.ID, actor.ID, &disabled, nil)
				want = app.ErrUnauthenticated
			case "add-member-after-demotion":
				_, err = second.PatchProjectMemberRole(root.ID, projectID, actor.ID, app.MemberRoleWriter)
			default:
				disabled := app.UserStatusDisabled
				_, err = second.PatchUser(root.ID, target.ID, &disabled, nil)
				want = app.ErrFailedPrecondition
			}
			if err != nil {
				t.Fatalf("changing actor/target: %v", err)
			}
			assertMutationRejectedWithoutWrites(t, repo, resume, want)
		})
	}
}

func TestPostgresReciprocalAdminDemotionsUseConsistentLockOrder(t *testing.T) {
	repo, cfg, _ := mutationTestRepository(t)
	bootstrap := mutationTestStore(t, cfg)
	root, err := bootstrap.Register("lock-root@example.test", "Root", "Lock-order-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	firstUser, err := bootstrap.CreateUser(root.ID, "lock-first@example.test", "First", "Lock-order-password-2026!", false)
	if err != nil {
		t.Fatal(err)
	}
	secondUser, err := bootstrap.CreateUser(root.ID, "lock-second@example.test", "Second", "Lock-order-password-2026!", false)
	if err != nil {
		t.Fatal(err)
	}
	team, err := bootstrap.CreateTeam(root.ID, "Lock order", "")
	if err != nil {
		t.Fatal(err)
	}
	project, err := bootstrap.CreateProject(root.ID, team.ID, "Lock order", "", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []*app.User{firstUser, secondUser} {
		if _, err := bootstrap.AddProjectMember(root.ID, project.ID, actor.ID, app.MemberRoleAdmin); err != nil {
			t.Fatal(err)
		}
	}
	barriers := []*mutationTransactionBarrier{
		{Repository: repo, reached: make(chan struct{}), resume: make(chan struct{})},
		{Repository: repo, reached: make(chan struct{}), resume: make(chan struct{})},
	}
	firstCfg := cfg
	firstCfg.DatabaseRepository = barriers[0]
	first := mutationTestStore(t, firstCfg)
	secondCfg := cfg
	secondCfg.DatabaseRepository = barriers[1]
	second := mutationTestStore(t, secondCfg)
	resumeFirst := pauseMutation(t, barriers[0], first, func(_ context.Context, store *app.Store) error {
		_, err := store.PatchProjectMemberRole(firstUser.ID, project.ID, secondUser.ID, app.MemberRoleReader)
		return err
	})
	resumeSecond := pauseMutation(t, barriers[1], second, func(_ context.Context, store *app.Store) error {
		_, err := store.PatchProjectMemberRole(secondUser.ID, project.ID, firstUser.ID, app.MemberRoleReader)
		return err
	})
	// 同时开放两次事务，验证互相降权只允许先取得权限锁的一方提交。
	results := make(chan error, 2)
	go func() { results <- resumeFirst() }()
	go func() { results <- resumeSecond() }()
	succeeded, denied := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			succeeded++
		} else if errors.Is(err, app.ErrPermissionDenied) {
			denied++
		} else {
			t.Fatalf("reciprocal mutation failed: %v", err)
		}
	}
	if succeeded != 1 || denied != 1 {
		t.Fatalf("succeeded=%d denied=%d, want one of each", succeeded, denied)
	}
}
