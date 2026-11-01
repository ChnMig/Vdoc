package vdoc

import (
	"sort"
	"time"
	repo "vdoc/domain/documentrepo"
	"vdoc/utils/id"
)

// 旧版本按原文档分支、时间和稳定 ID 导入；原始版本、草稿及分享链接不变。
func (s *Store) repositoryImportLegacyDocument(storage repo.Storage, scoped documentRepositoryStorage, actor, project string, document *repo.Document, genesis *repo.Commit, uploads *[]string) error {
	ctx := s.requestContext()
	state := s.stateLocked()
	if s.persistence != nil {
		var err error
		state, err = s.persistence.repo.LoadState(ctx)
		if err != nil {
			return err
		}
	}
	type snapshot struct {
		id, branch, content, key, hash, author, path, kind string
		at                                                 time.Time
	}
	snapshots := []snapshot{}
	for _, v := range state.Versions {
		if v.ProjectID == project && v.ServiceID == document.ID {
			snapshots = append(snapshots, snapshot{v.ID, v.BranchID, v.RawSchema, v.RawSchemaObjectKey, v.RawSchemaHash, v.PublishedBy, v.RelativePath, "version", v.PublishedAt})
		}
	}
	for _, d := range state.Drafts {
		if d.ProjectID == project && d.ServiceID == document.ID && d.Status != DraftStatusPublished {
			snapshots = append(snapshots, snapshot{d.ID, d.BranchID, d.RawSchema, d.RawSchemaObjectKey, d.RawSchemaHash, d.CreatedBy, "", "draft", d.UpdatedAt})
		}
	}
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].at.Equal(snapshots[j].at) {
			return snapshots[i].id < snapshots[j].id
		}
		return snapshots[i].at.Before(snapshots[j].at)
	})
	branches := map[string]*repo.Branch{}
	usedNames := map[string]bool{}
	for _, snap := range snapshots {
		b := branches[snap.branch]
		if b == nil {
			name := "main"
			if old := state.Branches[snap.branch]; old != nil && validRepositoryBranchName(old.Name) {
				name = old.Name
			}
			for usedNames[name] {
				name += "-imported"
			}
			usedNames[name] = true
			b = &repo.Branch{ID: id.GenerateID(), ProjectID: project, DocumentID: document.ID, Name: name, HeadCommitID: genesis.ID, Generation: 1, CreatedBy: actor, CreatedAt: time.Now().UTC()}
			branches[snap.branch] = b
		}
		f := document.File
		f.ObjectKey, f.ContentHash = snap.key, snap.hash
		if snap.path != "" {
			f.RelativePath = snap.path
		}
		if f.ObjectKey == "" || s.persistence == nil && s.objects == nil {
			var err error
			f, err = s.repositoryStoreFile(project, f, snap.content)
			*uploads = append(*uploads, f.ObjectKey)
			if err != nil {
				return err
			}
		} else if _, err := s.repositoryFileContent(f); err != nil {
			return err
		}
		c := repo.NewCommit(project, snap.author, "Import existing "+snap.kind, []string{b.HeadCommitID}, []repo.File{f}, snap.at)
		c.Provenance = "legacy_" + snap.kind + ":" + snap.id + "; chronological import within original document branch"
		repo.NumberCommit(c, b)
		b.HeadCommitID, b.Revision = c.ID, c.Revision
		if err := repositoryPut(ctx, scoped, project, "commit", c.ID, c, 1, nil); err != nil {
			return err
		}
		if snap.kind == "version" {
			release := repo.Release{ID: id.GenerateID(), ProjectID: project, BranchID: b.ID, CommitID: c.ID, VersionName: c.VersionName, Message: "Imported legacy publication", PublishedBy: snap.author, PublishedAt: snap.at}
			if err := repositoryPut(ctx, scoped, project, "release", release.ID, release, 1, nil); err != nil {
				return err
			}
		}
	}
	ordered := []*repo.Branch{}
	for _, b := range branches {
		ordered = append(ordered, b)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Name == "main" {
			return true
		}
		if ordered[j].Name == "main" {
			return false
		}
		return ordered[i].Name < ordered[j].Name
	})
	if len(ordered) == 0 {
		ordered = append(ordered, &repo.Branch{ID: id.GenerateID(), ProjectID: project, DocumentID: document.ID, Name: "main", HeadCommitID: genesis.ID, Generation: 1, CreatedBy: actor, CreatedAt: time.Now().UTC()})
	}
	for _, b := range ordered {
		if err := repositoryPut(ctx, scoped, project, "branch", b.ID, b, 1, nil); err != nil {
			return err
		}
	}
	document.DefaultBranchID, document.DefaultBranchName = ordered[0].ID, ordered[0].Name
	return nil
}
