package vdoc

import (
	"context"
	"errors"

	domain "vdoc/domain/vdoc"
)

type registrationMutation struct {
	userID      string
	onlyIfEmpty bool
}

var errInitialAdminAlreadyExists = errors.New("initial admin already exists")

type registrationContextRepository interface {
	HasRegisteredUsers(context.Context) (bool, error)
}

// 调用前必须持有协作不变量锁；空库资格依照此时的数据库，而非进程先前的快照。
func prepareRegistrationMutation(ctx context.Context, repository domain.Repository, store *Store) error {
	if store.registration == nil {
		return nil
	}
	repo, ok := repository.(registrationContextRepository)
	if !ok {
		return nil
	}
	hasUsers, err := repo.HasRegisteredUsers(ctx)
	if err != nil {
		return err
	}
	if store.registration.onlyIfEmpty && hasUsers {
		return errInitialAdminAlreadyExists
	}
	store.users[store.registration.userID].IsSuperAdmin = !hasUsers
	return nil
}
