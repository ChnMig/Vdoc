package vdoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	repo "vdoc/domain/documentrepo"
	"vdoc/utils/id"
)

// RepositoryDocuments 读取稳定项目目录，不使用当前文档的分支树代替整个目录。
func (s *Store) RepositoryDocuments(actor, project string) ([]repo.Document, error) {
	return s.repositoryDocuments(actor, project, MemberRoleReader)
}

// RepositoryWriteDocuments 为仅有写 scope 的 MCP 请求预检类型，不附加读取授权。
func (s *Store) RepositoryWriteDocuments(actor, project string) ([]repo.Document, error) {
	return s.repositoryDocuments(actor, project, MemberRoleWriter)
}

func (s *Store) repositoryDocuments(actor, project string, permission int) (out []repo.Document, err error) {
	err = s.WithRepositoryDocument("").WithRepositoryWrite(actor, project, permission, func(storage repo.Storage) error {
		out, err = s.repositoryDocumentsLocked(storage, project)
		out = activeRepositoryDocuments(out)
		return err
	})
	return
}

func activeRepositoryDocuments(documents []repo.Document) []repo.Document {
	out := make([]repo.Document, 0, len(documents))
	for _, document := range documents {
		if document.Status == DocumentStatusActive {
			out = append(out, document)
		}
	}
	return out
}

func (s *Store) repositoryDocumentsLocked(storage repo.Storage, project string) ([]repo.Document, error) {
	ctx := s.requestContext()
	documents := map[string]repo.Document{}
	for _, d := range s.apiServices {
		if d.ProjectID != project {
			continue
		}
		documents[d.ID] = repo.Document{ID: d.ID, ProjectID: project, File: repo.File{DocumentID: d.ID, Name: d.Name, RelativePath: d.RelativePath, DocumentType: d.DocumentType, Description: d.Description}, Status: d.Status, CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, DefaultBranchName: "main"}
	}
	branches, err := repositoryList[repo.Branch](ctx, storage, project, "branch")
	if err != nil {
		return nil, err
	}
	for _, b := range branches {
		if b.DocumentID != "" || strings.HasPrefix(b.Name, "legacy/") {
			continue
		}
		c, err := repositoryLoad[repo.Commit](ctx, storage, project, "commit", b.HeadCommitID)
		if err != nil {
			return nil, err
		}
		for _, f := range c.Files {
			d, exists := documents[f.DocumentID]
			if exists && (d.Status != 1 || !c.CreatedAt.After(d.UpdatedAt)) {
				continue
			}
			f.ObjectKey = ""
			d = repo.Document{ID: f.DocumentID, ProjectID: project, File: f, Status: 1, CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.CreatedAt, DefaultBranchName: "main"}
			documents[d.ID] = d
		}
	}
	registered, err := repositoryList[repo.Document](ctx, storage, project, "document")
	if err != nil {
		return nil, err
	}
	for _, d := range registered {
		documents[d.ID] = d
	}
	// 保留旧版本与草稿入口；迁移后的目录仍能找到原始历史。
	latestVersions := map[string]*ContractVersion{}
	for _, version := range s.versions {
		if version.ProjectID != project {
			continue
		}
		previous := latestVersions[version.ServiceID]
		if previous == nil || version.PublishedAt.After(previous.PublishedAt) || (version.PublishedAt.Equal(previous.PublishedAt) && version.ID > previous.ID) {
			latestVersions[version.ServiceID] = version
		}
	}
	latestDrafts := map[string]*ContractDraft{}
	for _, draft := range s.drafts {
		if draft.ProjectID != project || draft.Status == DraftStatusPublished {
			continue
		}
		previous := latestDrafts[draft.ServiceID]
		if previous == nil || draft.UpdatedAt.After(previous.UpdatedAt) || (draft.UpdatedAt.Equal(previous.UpdatedAt) && draft.ID > previous.ID) {
			latestDrafts[draft.ServiceID] = draft
		}
	}
	for id, d := range documents {
		if version := latestVersions[id]; version != nil {
			d.LatestVersionID = version.ID
		}
		if draft := latestDrafts[id]; draft != nil {
			d.LatestDraftID = draft.ID
		}
		documents[id] = d
	}
	out := make([]repo.Document, 0, len(documents))
	for _, d := range documents {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

type repositoryCreatedDocumentRecord struct {
	Fingerprint string               `json:"fingerprint"`
	DocumentID  string               `json:"document_id"`
	BranchID    string               `json:"branch_id"`
	CommitID    string               `json:"commit_id"`
	Result      repo.CreatedDocument `json:"result"`
}

// RepositoryCreateDocument 在一个事务中建立文档、独立初始分支及 .1 版本。
func (s *Store) RepositoryCreateDocument(actor, project string, input repo.DocumentInput) (out *repo.CreatedDocument, err error) {
	s = s.WithRepositoryDocument("")
	input.InitialBranch = strings.TrimSpace(input.InitialBranch)
	if input.InitialBranch == "" {
		input.InitialBranch = "main"
	}
	if !validRepositoryBranchName(input.InitialBranch) || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 200 {
		return nil, ErrInvalidArgument
	}
	encoded, _ := json.Marshal([]any{actor, input})
	fingerprint, key := repo.Hash(encoded), repo.Hash([]byte("create_document:"+actor+":"+input.IdempotencyKey))
	keys := []string{}
	err = s.withRepositoryStateWrite(actor, project, MemberRoleWriter, func(storage repo.Storage) error {
		ctx := s.requestContext()
		previous, e := repositoryLoad[repositoryCreatedDocumentRecord](ctx, storage, project, "idempotency", key)
		if e == nil {
			if previous.Fingerprint != fingerprint {
				return fmt.Errorf("%w: idempotency key reused for another document", ErrInvalidArgument)
			}
			if e = s.repositoryRequireActiveDocument(storage, project, previous.DocumentID); e != nil {
				return e
			}
			out = &previous.Result
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		f := repo.File{DocumentID: id.GenerateID(), Name: strings.TrimSpace(input.Name), RelativePath: input.RelativePath, DocumentType: input.DocumentType, Description: input.Description}
		if e = repo.ValidateFiles([]repo.File{f}); e != nil {
			return e
		}
		catalog, e := s.repositoryDocumentsLocked(storage, project)
		if e != nil {
			return e
		}
		for _, d := range catalog {
			if d.Status == 1 && (folderPathContains(d.RelativePath, f.RelativePath) || folderPathContains(f.RelativePath, d.RelativePath)) {
				return fmt.Errorf("%w: document path already exists", ErrAlreadyExists)
			}
		}
		for _, folder := range s.documentFolders {
			if folder.ProjectID == project && folderPathContains(folder.Path, f.RelativePath) {
				return ErrAlreadyExists
			}
		}
		now := time.Now().UTC()
		if _, e = s.ensureDocumentParentFoldersLocked(actor, project, f.RelativePath, now); e != nil {
			return e
		}
		f, e = s.repositoryStoreFile(project, f, input.Content)
		keys = append(keys, f.ObjectKey)
		if e != nil {
			return e
		}
		b := repo.Branch{ID: id.GenerateID(), ProjectID: project, DocumentID: f.DocumentID, Name: input.InitialBranch, Generation: 1, CreatedBy: actor, CreatedAt: now}
		c := repo.NewCommit(project, actor, "Create "+f.Name, nil, []repo.File{f}, now)
		repo.NumberCommit(c, &b)
		b.Revision, b.HeadCommitID = c.Revision, c.ID
		d := repo.Document{ID: f.DocumentID, ProjectID: project, File: f, DefaultBranchID: b.ID, DefaultBranchName: b.Name, HeadVersionName: c.VersionName, Status: 1, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
		d.ObjectKey = ""
		scoped := documentRepositoryStorage{Storage: storage, documentID: d.ID}
		if e = repositoryPut(ctx, scoped, project, "commit", c.ID, c, 1, nil); e != nil {
			return e
		}
		if e = repositoryPut(ctx, scoped, project, "branch", b.ID, b, 1, nil); e != nil {
			return e
		}
		if e = repositoryPut(ctx, scoped, project, "document", d.ID, d, 1, nil); e != nil {
			return e
		}
		if e = repositoryPut(ctx, scoped, project, "metadata", "converted", map[string]any{"converted_at": now}, 1, nil); e != nil {
			return e
		}
		if e = s.repositoryEvent(scoped, actor, project, "commit.created", &b, "", c.ID); e != nil {
			return e
		}
		if e = repositoryPut(ctx, storage, project, "idempotency", key, repositoryCreatedDocumentRecord{fingerprint, d.ID, b.ID, c.ID, repo.CreatedDocument{Document: d, Branch: b, Commit: *c}}, 1, nil); e != nil {
			return e
		}
		out = &repo.CreatedDocument{Document: d, Branch: b, Commit: *c}
		return nil
	})
	if err != nil {
		err = s.repositoryCleanupUploads(err, keys)
	}
	return
}

// repositoryPrepareDocument 为旧项目创建文档级投影。旧提交、旧分享及原始内容始终保留。
func (s *Store) repositoryPrepareDocument(storage repo.Storage, actor, project, documentID string, uploads *[]string) error {
	ctx := s.requestContext()
	if err := s.repositoryRequireActiveDocument(storage, project, documentID); err != nil {
		return err
	}
	scoped := documentRepositoryStorage{Storage: storage, documentID: documentID}
	if _, err := scoped.RepositoryRecord(ctx, project, "metadata", "converted"); err == nil {
		if d := s.apiServices[documentID]; d != nil && d.Status != 1 {
			return ErrFailedPrecondition
		}
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	catalog, err := s.repositoryDocumentsLocked(storage, project)
	if err != nil {
		return err
	}
	var document *repo.Document
	for i := range catalog {
		if catalog[i].ID == documentID {
			document = &catalog[i]
			break
		}
	}
	if document == nil {
		return ErrNotFound
	}
	if document.Status != 1 {
		return ErrFailedPrecondition
	}
	branches, err := repositoryList[repo.Branch](ctx, storage, project, "branch")
	if err != nil {
		return err
	}
	sources := []repo.Branch{}
	for _, b := range branches {
		if b.DocumentID != "" || strings.HasPrefix(b.Name, "legacy/") {
			continue
		}
		c, err := repositoryLoad[repo.Commit](ctx, storage, project, "commit", b.HeadCommitID)
		if err != nil {
			return err
		}
		if _, ok := fileByID(c.Files, documentID); ok {
			sources = append(sources, b)
		}
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Name == "main" {
			return true
		}
		if sources[j].Name == "main" {
			return false
		}
		return sources[i].Name < sources[j].Name
	})
	genesis := repo.NewCommit(project, actor, "Initialize document "+documentID, nil, nil, document.CreatedAt)
	genesis.DocumentID = documentID
	if err = repositoryPut(ctx, scoped, project, "commit", genesis.ID, genesis, 1, nil); err != nil {
		return err
	}
	memo, visiting := map[string]*repo.Commit{}, map[string]bool{}
	var projectCommit func(string, *repo.Branch) (*repo.Commit, error)
	projectCommit = func(oldID string, branch *repo.Branch) (*repo.Commit, error) {
		if c, ok := memo[oldID]; ok {
			return c, nil
		}
		if visiting[oldID] || len(memo)+len(visiting) >= 10000 {
			return nil, fmt.Errorf("%w: document history migration limit or cycle", ErrFailedPrecondition)
		}
		visiting[oldID] = true
		defer delete(visiting, oldID)
		old, err := repositoryLoad[repo.Commit](ctx, storage, project, "commit", oldID)
		if err != nil {
			return nil, err
		}
		parents := []*repo.Commit{}
		for _, parentID := range old.ParentIDs {
			p, err := projectCommit(parentID, branch)
			if err != nil {
				return nil, err
			}
			if len(parents) == 0 || parents[0].ID != p.ID {
				parents = append(parents, p)
			}
		}
		if len(parents) == 0 {
			parents = append(parents, genesis)
		}
		files := []repo.File{}
		if f, ok := fileByID(old.Files, documentID); ok {
			files = append(files, f)
		}
		if repo.TreeHash(files) == parents[0].TreeHash && len(parents) == 1 {
			memo[oldID] = parents[0]
			return parents[0], nil
		}
		parentIDs := []string{}
		for _, p := range parents {
			parentIDs = append(parentIDs, p.ID)
		}
		c := repo.NewCommit(project, old.CreatedBy, old.Message, parentIDs, files, old.CreatedAt)
		c.Provenance = "project_commit:" + old.ID
		repo.NumberCommit(c, branch)
		branch.Revision = c.Revision
		if err = repositoryPut(ctx, scoped, project, "commit", c.ID, c, 1, nil); err != nil {
			return nil, err
		}
		memo[oldID] = c
		return c, nil
	}
	usedNames := map[string]bool{}
	branchMapping := map[string]string{}
	for i, source := range sources {
		name := source.Name
		if i == 0 && name != "main" {
			name = "main"
		} else if strings.HasPrefix(name, "work/") {
			name = fmt.Sprintf("imported-%d", i+1)
		}
		for usedNames[name] {
			name += "-imported"
		}
		usedNames[name] = true
		b := repo.Branch{ID: id.GenerateID(), ProjectID: project, DocumentID: documentID, Name: name, Generation: 1, CreatedBy: actor, CreatedAt: time.Now().UTC()}
		c, err := projectCommit(source.HeadCommitID, &b)
		if err != nil {
			return err
		}
		b.HeadCommitID = c.ID
		branchMapping[source.ID] = b.ID
		if err = repositoryPut(ctx, scoped, project, "branch", b.ID, b, 1, nil); err != nil {
			return err
		}
		if i == 0 {
			document.DefaultBranchID, document.DefaultBranchName, document.HeadVersionName = b.ID, b.Name, c.VersionName
		}
	}
	if len(sources) == 0 {
		if err := s.repositoryImportLegacyDocument(storage, scoped, actor, project, document, genesis, uploads); err != nil {
			return err
		}
	}

	// 保留旧项目发布的可发现性；原发布 ID 继续指向既有分享，当前入口读取单文档投影。
	releases, err := repositoryList[repo.Release](ctx, storage, project, "release")
	if err != nil {
		return err
	}
	for _, release := range releases {
		original, err := repositoryLoad[repo.Commit](ctx, storage, project, "commit", release.CommitID)
		if err != nil {
			return err
		}
		file, exists := fileByID(original.Files, documentID)
		if !exists || original.DocumentID != "" {
			continue
		}
		projected := memo[original.ID]
		if projected == nil {
			projected = repo.NewCommit(project, original.CreatedBy, "Imported publication "+release.ID+" for "+documentID, nil, []repo.File{file}, original.CreatedAt)
			projected.DocumentID = documentID
			projected.VersionName = release.VersionName
			projected.Provenance = "project_commit:" + original.ID
			if err := repositoryPut(ctx, scoped, project, "commit", projected.ID, projected, 1, nil); err != nil {
				return err
			}
		}
		release.CommitID = projected.ID
		if mapped := branchMapping[release.BranchID]; mapped != "" {
			release.BranchID = mapped
		} else {
			release.BranchID = document.DefaultBranchID
		}
		if err := repositoryPut(ctx, scoped, project, "imported_release", documentID+":"+release.ID, release, 1, nil); err != nil {
			return err
		}
	}

	document.DocumentID = documentID
	if err = s.repositoryPutDocument(scoped, project, document); err != nil {
		return err
	}
	return repositoryPut(ctx, scoped, project, "metadata", "converted", map[string]any{"converted_at": time.Now().UTC(), "source": "document_projection"}, 1, nil)
}
