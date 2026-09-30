package vdoc

import (
	"context"
	"reflect"
	"testing"

	domain "vdoc/domain/vdoc"
)

// 两个独立 Store 只共享仓库，在提交前交错真实操作，模拟不同后端实例的陈旧快照。
func TestTeamArchiveAndProjectCreationRecheckCommittedState(t *testing.T) {
	for _, first := range []string{"archive", "create"} {
		t.Run(first+"-commits-first", func(t *testing.T) {
			state := domain.NewState()
			state.Users["super"] = &User{ID: "super", Status: UserStatusActive, IsSuperAdmin: true}
			state.Teams["team"] = &Team{ID: "team", Name: "Platform"}
			repo := &teamArchiveRaceRepository{recordingRepository: newRecordingRepository(state)}
			stale, other := NewStore(), NewStore()
			stale.persistence = &postgresPersistence{repo: repo}
			other.persistence = &postgresPersistence{repo: repo}
			var committed *domain.State
			repo.beforeTransaction = func() error {
				var err error
				if first == "archive" {
					_, err = other.ArchiveTeam("super", "team")
				} else {
					_, err = other.CreateProject("super", "team", "Platform API", "", "super")
				}
				committed = cloneRepositoryStateWithoutBodies(repo.state)
				return err
			}
			var err error
			want := ErrFailedPrecondition
			if first == "archive" {
				_, err = stale.CreateProject("super", "team", "Stale API", "", "super")
				want = ErrNotFound
			} else {
				_, err = stale.ArchiveTeam("super", "team")
			}
			if !Is(err, want) {
				t.Fatalf("stale operation error = %v, want %v", err, want)
			}
			if committed == nil || !reflect.DeepEqual(committed, cloneRepositoryStateWithoutBodies(repo.state)) {
				t.Fatal("rejected operation changed persistent rows or audits")
			}
			t.Logf("%s committed first; stale operation rejected with %v and no writes", first, err)
		})
	}
}

type teamArchiveRaceRepository struct {
	*recordingRepository
	beforeTransaction func() error
}

func (r *teamArchiveRaceRepository) WithinTransaction(ctx context.Context, apply func(domain.Repository) error) error {
	if r.beforeTransaction != nil {
		hook := r.beforeTransaction
		r.beforeTransaction = nil
		if err := hook(); err != nil {
			return err
		}
	}
	tx := &teamArchiveRaceRepository{recordingRepository: newRecordingRepository(r.state)}
	if err := apply(tx); err != nil {
		return err
	}
	r.recordingRepository = tx.recordingRepository
	return nil
}

func (r *teamArchiveRaceRepository) LockMutationContext(_ context.Context, guard domain.MutationGuard) (*domain.State, error) {
	state := domain.NewState()
	state.Users[guard.ActorID] = r.state.Users[guard.ActorID]
	for _, userID := range guard.ActiveUserIDs {
		state.Users[userID] = r.state.Users[userID]
	}
	// 团队查询必须由提交 guard 显式请求，避免 fake 掩盖数据库锁范围遗漏。
	state.Teams[guard.TeamID] = r.state.Teams[guard.TeamID]
	return state, nil
}
