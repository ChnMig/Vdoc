package vdoc

import (
	"errors"
	"fmt"
	"sort"
	"time"

	repo "vdoc/domain/documentrepo"
	"vdoc/utils/id"
)

func (s *Store) RepositoryReleases(actor, project string) (out []repo.Release, err error) {
	err = s.WithRepositoryRead(actor, project, func(storage repo.Storage) error {
		out, err = repositoryList[repo.Release](s.requestContext(), storage, project, "release")
		sort.Slice(out, func(i, j int) bool { return out[i].PublishedAt.After(out[j].PublishedAt) })
		return err
	})
	return
}

func (s *Store) RepositoryPublish(actor, project string, input repo.PublishInput) (out *repo.Release, err error) {
	if input.BranchID == "" || input.CommitID == "" || input.ExpectedGeneration < 1 {
		return nil, ErrInvalidArgument
	}
	if mcpMutationTokenID(s.requestContext()) != "" {
		return nil, ErrPermissionDenied
	}
	err = s.WithRepositoryWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		if e := s.repositoryRequireImported(storage, project); e != nil {
			return e
		}
		b, e := repositoryLoad[repo.Branch](s.requestContext(), storage, project, "branch", input.BranchID)
		if e != nil {
			return e
		}
		if b.HeadCommitID != input.CommitID || b.Generation != input.ExpectedGeneration {
			return fmt.Errorf("%w: REVIEW_STALE: branch changed before publication", ErrFailedPrecondition)
		}
		commit, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", input.CommitID)
		if e != nil {
			return e
		}
		if e = s.repositoryValidatePublished(commit); e != nil {
			return e
		}
		now := time.Now().UTC()
		releaseID := id.GenerateID()
		out = &repo.Release{ID: releaseID, ProjectID: project, BranchID: b.ID, CommitID: commit.ID, VersionName: now.Format("20060102.150405") + "-" + releaseID[:8], Message: input.Message, PublishedBy: actor, PublishedAt: now}
		if e = repositoryPut(s.requestContext(), storage, project, "release", out.ID, out, 1, nil); e != nil {
			return e
		}
		return s.repositoryEvent(storage, actor, project, "release.published", b, "", out.ID)
	})
	return
}

// RepositoryBootstrap 显式保存每一份仍可证明的旧快照，再建立有来源标记的项目初始树。
func (s *Store) RepositoryBootstrap(actor, project string) (out []repo.Branch, err error) {
	newKeys := []string{}
	err = s.WithRepositoryWrite(actor, project, MemberRoleAdmin, func(storage repo.Storage) error {
		if _, e := storage.RepositoryRecord(s.requestContext(), project, "metadata", "converted"); e == nil {
			return fmt.Errorf("%w: repository has already been converted", ErrFailedPrecondition)
		} else if !errors.Is(e, ErrNotFound) {
			return e
		}
		branches, e := s.repositoryEnsure(storage, actor, project)
		if e != nil {
			return e
		}
		var main *repo.Branch
		for i := range branches {
			if branches[i].Name == "main" {
				main = &branches[i]
				break
			}
		}
		if main == nil {
			return ErrFailedPrecondition
		}
		genesis, e := repositoryLoad[repo.Commit](s.requestContext(), storage, project, "commit", main.HeadCommitID)
		if e != nil {
			return e
		}
		if len(genesis.Files) > 0 || len(genesis.ParentIDs) > 0 {
			return fmt.Errorf("%w: import requires the empty initial main branch", ErrFailedPrecondition)
		}
		state := s.stateLocked()
		if s.persistence != nil {
			state, e = s.persistence.repo.LoadState(s.requestContext())
			if e != nil {
				return e
			}
		}
		latest := map[string]*ContractVersion{}
		latestFiles := map[string]repo.File{}
		add := func(kind, snapshotID, documentID, branchID, relativePath, content, key, hash, author string, at time.Time) (*repo.Commit, error) {
			document := state.APIServices[documentID]
			if document == nil || document.ProjectID != project {
				return nil, ErrNotFound
			}
			if key != "" {
				if s.persistence == nil && s.objects == nil {
					if hash != "" && repo.Hash([]byte(content)) != hash {
						return nil, fmt.Errorf("legacy inline content failed SHA-256 verification")
					}
					key = ""
				} else {
					body, e := s.readVerifiedObject(s.requestContext(), key, hash)
					if e != nil {
						return nil, e
					}
					content = string(body)
				}
			}
			f := repo.File{DocumentID: documentID, Name: document.Name, RelativePath: document.RelativePath, DocumentType: document.DocumentType, Description: document.Description, ContentHash: repo.Hash([]byte(content)), ObjectKey: key}
			if relativePath != "" {
				f.RelativePath = relativePath
			}
			if key == "" {
				var e error
				f, e = s.repositoryStoreFile(project, f, content)
				newKeys = append(newKeys, f.ObjectKey)
				if e != nil {
					return nil, e
				}
			}
			if e := repo.ValidateFiles([]repo.File{f}); e != nil {
				return nil, e
			}
			c := repo.NewCommit(project, author, "Import "+kind+" "+snapshotID, []string{genesis.ID}, []repo.File{f}, at)
			c.Provenance = "legacy_" + kind + ":" + snapshotID + "; original branch=" + branchID + "; historical project tree is not inferred"
			if e := repositoryPut(s.requestContext(), storage, project, "commit", c.ID, c, 1, nil); e != nil {
				return nil, e
			}
			b := repo.Branch{ID: id.GenerateID(), ProjectID: project, Name: "legacy/" + documentID + "/" + kind + "/" + snapshotID, HeadCommitID: c.ID, Generation: 1, IsProtected: true, CreatedBy: actor, CreatedAt: time.Now().UTC()}
			if e := repositoryPut(s.requestContext(), storage, project, "branch", b.ID, b, 1, nil); e != nil {
				return nil, e
			}
			if e := repositoryPut(s.requestContext(), storage, project, "legacy_map", kind+":"+snapshotID, map[string]string{"commit_id": c.ID, "document_id": documentID}, 1, nil); e != nil {
				return nil, e
			}
			return c, nil
		}
		versions := make([]*ContractVersion, 0)
		for _, v := range state.Versions {
			if v.ProjectID == project {
				versions = append(versions, v)
			}
		}
		sort.Slice(versions, func(i, j int) bool { return versions[i].PublishedAt.Before(versions[j].PublishedAt) })
		for _, v := range versions {
			c, e := add("version", v.ID, v.ServiceID, v.BranchID, v.RelativePath, v.RawSchema, v.RawSchemaObjectKey, v.RawSchemaHash, v.PublishedBy, v.PublishedAt)
			if e != nil {
				return e
			}
			r := repo.Release{ID: id.GenerateID(), ProjectID: project, BranchID: main.ID, CommitID: c.ID, VersionName: "import-" + v.ID[:min(8, len(v.ID))], Message: "Imported legacy version " + v.VersionName, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt}
			if e = repositoryPut(s.requestContext(), storage, project, "release", r.ID, r, 1, nil); e != nil {
				return e
			}
			if previous := latest[v.ServiceID]; previous == nil || v.PublishedAt.After(previous.PublishedAt) {
				latest[v.ServiceID] = v
				latestFiles[v.ServiceID] = c.Files[0]
			}
		}
		for _, d := range state.Drafts {
			if d.ProjectID != project || d.Status == DraftStatusPublished {
				continue
			}
			if _, e = add("draft", d.ID, d.ServiceID, d.BranchID, "", d.RawSchema, d.RawSchemaObjectKey, d.RawSchemaHash, d.CreatedBy, d.UpdatedAt); e != nil {
				return e
			}
		}
		files := repositorySortedFiles(latestFiles)
		if e = repo.ValidateFiles(files); e != nil {
			return e
		}
		synthetic := repo.NewCommit(project, actor, "Import current published documents into project repository", []string{genesis.ID}, files, time.Now())
		synthetic.Provenance = "synthetic project initialization: each file is its latest observed legacy publication; original publications were not atomic project commits"
		if e = s.repositoryAdvance(storage, actor, project, main, synthetic); e != nil {
			return e
		}
		if e = repositoryPut(s.requestContext(), storage, project, "metadata", "converted", map[string]any{"converted_at": time.Now().UTC(), "actor_id": actor, "imported_versions": len(versions)}, 1, nil); e != nil {
			return e
		}
		out, e = repositoryList[repo.Branch](s.requestContext(), storage, project, "branch")
		return e
	})
	if err != nil {
		err = s.repositoryCleanupUploads(err, newKeys)
	}
	return
}
