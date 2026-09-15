package vdoc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	domain "vdoc/domain/vdoc"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRepositoryTransactionClassifiesCommitOutcome(t *testing.T) {
	beginErr := errors.New("begin failed")
	callbackErr := errors.New("business write rejected")
	commitErr := errors.New("commit acknowledgement lost")
	for _, test := range []struct {
		name          string
		beginErr      error
		callbackErr   error
		commitErr     error
		wantErr       error
		wantCallback  int
		wantCommit    int
		wantRollback  int
		wantUncertain bool
	}{
		{name: "begin-failure", beginErr: beginErr, wantErr: beginErr},
		{name: "callback-failure", callbackErr: callbackErr, commitErr: commitErr, wantErr: callbackErr, wantCallback: 1, wantRollback: 1},
		{name: "commit-failure", commitErr: commitErr, wantErr: commitErr, wantCallback: 1, wantCommit: 1, wantRollback: 1, wantUncertain: true},
		{name: "success", wantCallback: 1, wantCommit: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			commands := &transactionOutcomeTestSQL{t: t}
			tx := &transactionOutcomeTestTx{transactionOutcomeTestSQL: commands, commitErr: test.commitErr}
			pool := &transactionOutcomeTestPool{transactionOutcomeTestSQL: commands, beginErr: test.beginErr, transaction: tx}
			database, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			repository := NewRepository(database)
			callbackCalls := 0
			err = repository.WithinTransaction(context.Background(), func(writer domain.Repository) error {
				callbackCalls++
				if err := writer.RecordObject(context.Background(), domain.ObjectRef{Key: "attempt/raw.json", Kind: "raw", OwnerType: "draft", Hash: "content-hash", SizeBytes: 2}); err != nil {
					return err
				}
				return test.callbackErr
			})
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("WithinTransaction() error = %v, want success", err)
				}
			} else if !errors.Is(err, test.wantErr) {
				t.Fatalf("WithinTransaction() error = %v, want original cause %v", err, test.wantErr)
			}
			if uncertain := errors.Is(err, domain.ErrCommitOutcomeUnknown); uncertain != test.wantUncertain {
				t.Fatalf("uncertain commit = %t, want %t; error=%v", uncertain, test.wantUncertain, err)
			}
			if pool.beginCalls != 1 || callbackCalls != test.wantCallback || commands.execCalls != test.wantCallback || tx.commitCalls != test.wantCommit || tx.rollbackCalls != test.wantRollback {
				t.Fatalf("begin=%d callback=%d write=%d commit=%d rollback=%d, want 1/%d/%d/%d/%d", pool.beginCalls, callbackCalls, commands.execCalls, tx.commitCalls, tx.rollbackCalls, test.wantCallback, test.wantCallback, test.wantCommit, test.wantRollback)
			}
		})
	}
}

type transactionOutcomeTestSQL struct {
	t         *testing.T
	execCalls int
}

func (s *transactionOutcomeTestSQL) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	s.t.Fatal("unexpected SQL prepare in transaction outcome test")
	return nil, errors.New("unexpected SQL prepare")
}

func (s *transactionOutcomeTestSQL) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	s.execCalls++
	return driver.RowsAffected(1), nil
}

func (s *transactionOutcomeTestSQL) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	s.t.Fatal("unexpected SQL query in transaction outcome test")
	return nil, errors.New("unexpected SQL query")
}

func (s *transactionOutcomeTestSQL) QueryRowContext(context.Context, string, ...any) *sql.Row {
	s.t.Fatal("unexpected SQL row query in transaction outcome test")
	return nil
}

type transactionOutcomeTestPool struct {
	*transactionOutcomeTestSQL
	beginErr    error
	beginCalls  int
	transaction *transactionOutcomeTestTx
}

func (p *transactionOutcomeTestPool) BeginTx(context.Context, *sql.TxOptions) (gorm.ConnPool, error) {
	p.beginCalls++
	if p.beginErr != nil {
		return nil, p.beginErr
	}
	return p.transaction, nil
}

type transactionOutcomeTestTx struct {
	*transactionOutcomeTestSQL
	commitErr     error
	commitCalls   int
	rollbackCalls int
}

func (tx *transactionOutcomeTestTx) Commit() error {
	tx.commitCalls++
	return tx.commitErr
}

func (tx *transactionOutcomeTestTx) Rollback() error {
	tx.rollbackCalls++
	return nil
}
