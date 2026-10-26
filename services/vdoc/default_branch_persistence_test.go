package vdoc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	domainbranch "vdoc/domain/documentbranch"
	domain "vdoc/domain/vdoc"
)

func TestSaveDocumentWorkflowSwitchesDefaultWithoutTransientDuplicate(t *testing.T) {
	for _, optimistic := range []bool{false, true} {
		for _, targetID := range []string{"a-target", "z-target"} {
			name := targetID + "/legacy"
			if optimistic {
				name = targetID + "/optimistic"
			}
			t.Run(name, func(t *testing.T) {
				now := time.Now().UTC()
				store := NewStore()
				store.branches["m-current"] = &ContractBranch{ID: "m-current", DocumentID: "document", ServiceID: "document", IsDefault: true, Status: BranchStatusActive, UpdatedAt: now}
				store.branches[targetID] = &ContractBranch{ID: targetID, DocumentID: "document", ServiceID: "document", Status: BranchStatusActive, UpdatedAt: now}
				store.persisted = store.cloneStateLocked()
				writer := &defaultBranchConstraintWriter{branches: map[string]*ContractBranch{}}
				for id, branch := range store.persisted.Branches {
					writer.branches[id] = cloneBranch(branch)
				}
				makeDefault := true
				if err := domainbranch.Update(domainbranch.UpdateParams{Branch: store.branches[targetID], Branches: store.branchesForServiceLocked("document"), IsDefault: &makeDefault, Now: now.Add(time.Second)}); err != nil {
					t.Fatal(err)
				}
				var repository documentMutationRepository = writer
				var optimisticWriter *optimisticDefaultBranchWriter
				if optimistic {
					optimisticWriter = &optimisticDefaultBranchWriter{defaultBranchConstraintWriter: writer, previous: map[string]*ContractBranch{}}
					repository = optimisticWriter
				}
				if err := (&postgresPersistence{}).saveDocumentWorkflowLocked(context.Background(), store, repository); err != nil {
					t.Fatalf("switch default: %v", err)
				}
				if writer.branches["m-current"].IsDefault || !writer.branches[targetID].IsDefault {
					t.Fatalf("persisted defaults = %+v", writer.branches)
				}
				if !reflect.DeepEqual(writer.calls, []string{"m-current", targetID}) {
					t.Fatalf("write order = %v, want old default cleared before new default", writer.calls)
				}
				if optimistic {
					for id, previous := range store.persisted.Branches {
						if optimisticWriter.previous[id] != previous {
							t.Fatalf("optimistic write for %s lost loaded previous row", id)
						}
					}
				}
			})
		}
	}
}

// 模拟 PostgreSQL 立即执行的默认分支唯一索引，不能等事务结束再检查。
type defaultBranchConstraintWriter struct {
	branches map[string]*ContractBranch
	calls    []string
}

func (*defaultBranchConstraintWriter) UpsertDocument(context.Context, *domain.APIService) error {
	return nil
}

func (*defaultBranchConstraintWriter) UpsertDocumentDraft(context.Context, *domain.ContractDraft, *domain.APIService) error {
	return nil
}

func (r *defaultBranchConstraintWriter) UpsertDocumentBranch(_ context.Context, branch *domain.ContractBranch) error {
	r.calls = append(r.calls, branch.ID)
	if branch.IsDefault {
		for id, existing := range r.branches {
			if id != branch.ID && existing.DocumentID == branch.DocumentID && existing.IsDefault {
				return errors.New("document_branches_default_uidx: duplicate default")
			}
		}
	}
	r.branches[branch.ID] = cloneBranch(branch)
	return nil
}

type optimisticDefaultBranchWriter struct {
	*defaultBranchConstraintWriter
	previous map[string]*ContractBranch
}

func (r *optimisticDefaultBranchWriter) UpsertDocumentIfUnchanged(ctx context.Context, current, _ *domain.APIService) error {
	return r.UpsertDocument(ctx, current)
}

func (r *optimisticDefaultBranchWriter) UpsertDocumentDraftIfUnchanged(ctx context.Context, current, _ *domain.ContractDraft, document *domain.APIService) error {
	return r.UpsertDocumentDraft(ctx, current, document)
}

func (r *optimisticDefaultBranchWriter) UpsertDocumentBranchIfUnchanged(ctx context.Context, current, previous *domain.ContractBranch) error {
	r.previous[current.ID] = previous
	return r.UpsertDocumentBranch(ctx, current)
}
