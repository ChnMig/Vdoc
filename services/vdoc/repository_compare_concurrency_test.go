package vdoc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	repo "vdoc/domain/documentrepo"
)

type comparisonBlockingObjects struct {
	ObjectStorage
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (o *comparisonBlockingObjects) GetObject(ctx context.Context, key string) ([]byte, error) {
	o.once.Do(func() { close(o.entered) })
	select {
	case <-o.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return o.ObjectStorage.GetObject(ctx, key)
}

func TestRepositoryCompareReleasesStoreLockAndRechecksAccess(t *testing.T) {
	for _, scenario := range []string{"read", "permission", "token", "delete", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, branch := gitTestRepository(t)
			before := gitSave(t, s, &branch, "comparison-before", gitChange("one", "one.md", "old\n"))
			after := gitSave(t, s, &branch, "comparison-after", gitChange("one", "one.md", "new\n"))
			objects := newRecordingObjectStorage(nil)
			for key, body := range s.repoMemory.blobs {
				objects.objects[key] = append([]byte(nil), body...)
			}
			gate := &comparisonBlockingObjects{ObjectStorage: objects, entered: make(chan struct{}), release: make(chan struct{})}
			s.objects = gate
			var release sync.Once
			unblock := func() { release.Do(func() { close(gate.release) }) }
			t.Cleanup(unblock)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var tokenID string
			if scenario == "token" {
				token, err := s.CreateMCPToken("reader", "comparison-token", []int{ScopeDocRead}, nil)
				if err != nil {
					t.Fatal(err)
				}
				tokenID = token.ID
				ctx = WithMCPToken(ctx, token.ID, token.Scopes...)
			}
			type comparisonResult struct {
				out *repo.Comparison
				err error
			}
			done := make(chan comparisonResult, 1)
			go func() {
				out, err := s.WithContext(ctx).RepositoryCompare("reader", "project-a", repo.CompareInput{FromCommitID: before.ID, ToCommitID: after.ID})
				done <- comparisonResult{out, err}
			}()
			select {
			case <-gate.entered:
			case result := <-done:
				t.Fatalf("comparison exited before storage read: %v", result.err)
			case <-time.After(3 * time.Second):
				t.Fatal("comparison did not reach storage")
			}
			// 存储读取停住时，另一个仓库请求必须仍可完成。
			readDone := make(chan error, 1)
			go func() { _, err := s.RepositoryBranches("reader", "project-a"); readDone <- err }()
			select {
			case err := <-readDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				unblock()
				t.Fatal("object read retained the shared Store lock")
			}
			if scenario == "permission" {
				s.mu.Lock()
				s.users["reader"].Status = UserStatusDisabled
				s.mu.Unlock()
			}
			if scenario == "token" {
				if _, err := s.RevokeMCPToken("reader", tokenID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "delete" {
				if err := s.RepositoryDeleteDocument("admin", "project-a", "one"); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "cancel" {
				cancel()
			}
			unblock()
			select {
			case result := <-done:
				if scenario == "read" {
					if result.err != nil || result.out == nil || len(result.out.Files) != 1 || result.out.Files[0].NewContent != "new\n" {
						t.Fatalf("comparison=%+v error=%v", result.out, result.err)
					}
				} else if result.err == nil || result.out != nil {
					t.Fatalf("access changed during I/O but content returned: %+v %v", result.out, result.err)
				}
				if scenario == "cancel" && !errors.Is(result.err, context.Canceled) {
					t.Fatalf("cancellation=%v", result.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("comparison did not finish")
			}
		})
	}
}
