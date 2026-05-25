package vdoc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"vdoc/config"
	domainvdoc "vdoc/domain/vdoc"
	"vdoc/utils/encryption"
	"vdoc/utils/id"
	"vdoc/utils/random"
)

var (
	ErrInvalidArgument    = domainvdoc.ErrInvalidArgument
	ErrUnauthenticated    = domainvdoc.ErrUnauthenticated
	ErrPermissionDenied   = domainvdoc.ErrPermissionDenied
	ErrNotFound           = domainvdoc.ErrNotFound
	ErrAlreadyExists      = domainvdoc.ErrAlreadyExists
	ErrFailedPrecondition = domainvdoc.ErrFailedPrecondition
)

const minUserPasswordLength = 12

func Is(err, target error) bool { return domainvdoc.Is(err, target) }

type Store struct {
	mu          sync.RWMutex
	users       map[string]*User
	teams       map[string]*Team
	projects    map[string]*Project
	members     map[string]*ProjectMember
	services    map[string]*APIService
	branches    map[string]*ContractBranch
	drafts      map[string]*ContractDraft
	versions    map[string]*ContractVersion
	endpoints   map[string]*Endpoint
	diffs       map[string]*Diff
	tokens      map[string]*MCPToken
	audits      map[string]*AuditLog
	persistence *postgresPersistence
	objects     ObjectStorage
}

var defaultStore = NewStore()

func DefaultStore() *Store { return defaultStore }

func NewStore() *Store {
	return &Store{
		users: map[string]*User{}, teams: map[string]*Team{}, projects: map[string]*Project{}, members: map[string]*ProjectMember{},
		services: map[string]*APIService{}, branches: map[string]*ContractBranch{}, drafts: map[string]*ContractDraft{},
		versions: map[string]*ContractVersion{}, endpoints: map[string]*Endpoint{}, diffs: map[string]*Diff{}, tokens: map[string]*MCPToken{}, audits: map[string]*AuditLog{},
	}
}

func ResetDefaultStoreForTest() { defaultStore = NewStore() }

func (s *Store) refreshLocked() error {
	if s.persistence == nil {
		return nil
	}
	return s.persistence.load(context.Background(), s)
}

func (s *Store) persistLocked() error {
	if s.persistence == nil {
		return nil
	}
	ctx := context.Background()
	if err := s.persistence.saveLocked(ctx, s); err != nil {
		return err
	}
	return s.persistence.load(ctx, s)
}

func (s *Store) persistSchemaObjectLocked(projectID, serviceID, branchID, ownerType, ownerID, kind, hash, content string) (string, domainvdoc.ObjectRef, error) {
	ownerCollection := ownerType + "s"
	key := fmt.Sprintf("projects/%s/services/%s/branches/%s/%s/%s/%s-%s.json", projectID, serviceID, branchID, ownerCollection, ownerID, kind, hash)
	metadata := map[string]string{
		"project_id":       projectID,
		"service_id":       serviceID,
		"branch_id":        branchID,
		"owner_type":       ownerType,
		"owner_id":         ownerID,
		"owner_collection": ownerCollection,
		"kind":             kind,
		"sha256":           hash,
	}
	contentType := "application/json"
	info := ObjectInfo{SizeBytes: int64(len(content)), Metadata: metadata}
	if s.objects != nil {
		var err error
		info, err = s.objects.PutObject(context.Background(), ObjectWrite{Key: key, ContentType: contentType, Body: []byte(content), Metadata: metadata})
		if err != nil {
			return "", domainvdoc.ObjectRef{}, err
		}
		if info.SizeBytes == 0 {
			info.SizeBytes = int64(len(content))
		}
		if len(info.Metadata) == 0 {
			info.Metadata = metadata
		}
	} else if s.persistence != nil {
		return "", domainvdoc.ObjectRef{}, fmt.Errorf("object storage is required for persistent schema objects")
	}
	ref := domainvdoc.ObjectRef{Key: key, Kind: kind, OwnerType: ownerType, OwnerID: ownerID, Hash: hash, ContentType: contentType, SizeBytes: info.SizeBytes, ETag: info.ETag, Metadata: copyStringMap(info.Metadata)}
	return key, ref, nil
}

func (s *Store) persistDiffSnapshotLocked(projectID, serviceID, branchID string, diff *Diff) (domainvdoc.ObjectRef, error) {
	if diff == nil {
		return domainvdoc.ObjectRef{}, nil
	}
	snapshot := struct {
		ID            string      `json:"id"`
		ServiceID     string      `json:"service_id"`
		FromVersionID string      `json:"from_version_id"`
		ToVersionID   string      `json:"to_version_id"`
		DiffStatus    int         `json:"diff_status"`
		Summary       DiffSummary `json:"summary"`
		Items         []DiffItem  `json:"items"`
		CreatedAt     time.Time   `json:"created_at"`
		UpdatedAt     time.Time   `json:"updated_at"`
	}{ID: diff.ID, ServiceID: diff.ServiceID, FromVersionID: diff.FromVersionID, ToVersionID: diff.ToVersionID, DiffStatus: diff.DiffStatus, Summary: diff.Summary, Items: diff.Items, CreatedAt: diff.CreatedAt, UpdatedAt: diff.UpdatedAt}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return domainvdoc.ObjectRef{}, err
	}
	hash := sha(string(body))
	key := fmt.Sprintf("projects/%s/services/%s/branches/%s/diffs/%s/full-%s.json", projectID, serviceID, branchID, diff.ID, hash)
	metadata := map[string]string{
		"project_id":      projectID,
		"service_id":      serviceID,
		"branch_id":       branchID,
		"owner_type":      "diff",
		"owner_id":        diff.ID,
		"kind":            "full-diff",
		"sha256":          hash,
		"from_version_id": diff.FromVersionID,
		"to_version_id":   diff.ToVersionID,
	}
	contentType := "application/json"
	info := ObjectInfo{SizeBytes: int64(len(body)), Metadata: metadata}
	if s.objects != nil {
		info, err = s.objects.PutObject(context.Background(), ObjectWrite{Key: key, ContentType: contentType, Body: body, Metadata: metadata})
		if err != nil {
			return domainvdoc.ObjectRef{}, err
		}
		if info.SizeBytes == 0 {
			info.SizeBytes = int64(len(body))
		}
		if len(info.Metadata) == 0 {
			info.Metadata = metadata
		}
	} else if s.persistence != nil {
		return domainvdoc.ObjectRef{}, fmt.Errorf("object storage is required for persistent diff snapshots")
	}
	diff.ObjectKey = key
	diff.Hash = hash
	ref := domainvdoc.ObjectRef{Key: key, Kind: "full-diff", OwnerType: "diff", OwnerID: diff.ID, Hash: hash, ContentType: contentType, SizeBytes: info.SizeBytes, ETag: info.ETag, Metadata: copyStringMap(info.Metadata)}
	return ref, nil
}

func (s *Store) recordObjectRefsLocked(refs ...domainvdoc.ObjectRef) error {
	if s.persistence == nil {
		return nil
	}
	ctx := context.Background()
	for _, ref := range refs {
		if ref.Key == "" {
			continue
		}
		if err := s.persistence.recordObject(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RecordAudit(audit AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return err
	}
	s.appendAuditLocked(&audit)
	return s.persistLocked()
}

func (s *Store) ListAuditLogs() ([]*AuditLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	return s.sortedAuditLogsLocked(), nil
}

func (s *Store) AuditLogsForTest() []*AuditLog {
	logs, _ := s.ListAuditLogs()
	return logs
}

func (s *Store) sortedAuditLogsLocked() []*AuditLog {
	logs := make([]*AuditLog, 0, len(s.audits))
	for _, audit := range s.audits {
		logs = append(logs, cloneAuditLog(audit))
	}
	sort.Slice(logs, func(first, second int) bool {
		if logs[first].CreatedAt.Equal(logs[second].CreatedAt) {
			return logs[first].ID < logs[second].ID
		}
		return logs[first].CreatedAt.Before(logs[second].CreatedAt)
	})
	return logs
}

func (s *Store) appendAuditLocked(audit *AuditLog) {
	if audit == nil {
		return
	}
	if s.audits == nil {
		s.audits = map[string]*AuditLog{}
	}
	if audit.ID == "" {
		audit.ID = id.GenerateID()
	}
	if audit.ActorType == 0 {
		audit.ActorType = AuditActorSystem
	}
	if audit.Metadata == nil {
		audit.Metadata = map[string]string{}
	}
	if _, ok := audit.Metadata["result"]; !ok {
		audit.Metadata["result"] = "success"
	}
	now := time.Now()
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = now
	}
	if audit.UpdatedAt.IsZero() {
		audit.UpdatedAt = audit.CreatedAt
	}
	s.audits[audit.ID] = cloneAuditLog(audit)
}

func (s *Store) auditLocked(ctx AuditContext, actorType int, actorUserID, action, resourceType, resourceID, projectID, serviceID string, metadata map[string]string) {
	if s.audits == nil {
		s.audits = map[string]*AuditLog{}
	}
	appendAuditToState(s.audits, ctx, actorType, actorUserID, action, resourceType, resourceID, projectID, serviceID, metadata)
}

func appendAuditToState(audits map[string]*AuditLog, ctx AuditContext, actorType int, actorUserID, action, resourceType, resourceID, projectID, serviceID string, metadata map[string]string) *AuditLog {
	if audits == nil {
		return nil
	}
	if ctx.ActorType != 0 {
		actorType = ctx.ActorType
	}
	if actorType == 0 {
		actorType = AuditActorSystem
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	if _, ok := metadata["result"]; !ok {
		metadata["result"] = "success"
	}
	now := time.Now()
	audit := &AuditLog{ID: id.GenerateID(), ActorType: actorType, ActorUserID: actorUserID, ActorTokenID: ctx.ActorTokenID, Action: action, ResourceType: resourceType, ResourceID: resourceID, ProjectID: projectID, ServiceID: serviceID, Metadata: copyStringMap(metadata), IPAddress: ctx.IPAddress, UserAgent: ctx.UserAgent, RequestID: ctx.RequestID, CreatedAt: now, UpdatedAt: now}
	audits[audit.ID] = audit
	return audit
}

func auditContext(values []AuditContext) AuditContext {
	if len(values) == 0 {
		return AuditContext{}
	}
	return values[0]
}

func auditMetadata(pairs ...string) map[string]string {
	metadata := map[string]string{}
	for index := 0; index+1 < len(pairs); index += 2 {
		key := strings.TrimSpace(pairs[index])
		if key == "" {
			continue
		}
		metadata[key] = pairs[index+1]
	}
	return metadata
}

func (s *Store) Register(email, name, password string, auditCtx ...AuditContext) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	email = normalizeUserEmail(email)
	name = strings.TrimSpace(name)
	if email == "" {
		return nil, fmt.Errorf("%w: email is required", ErrInvalidArgument)
	}
	if err := validateUserPassword(password); err != nil {
		return nil, err
	}
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			return nil, fmt.Errorf("%w: email already exists", ErrAlreadyExists)
		}
	}
	hash, err := encryption.HashPasswordWithBcrypt(password)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	user := &User{ID: id.GenerateID(), Email: email, Name: name, PasswordHash: hash, IsSuperAdmin: len(s.users) == 0, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	s.users[user.ID] = user
	s.auditLocked(ctx, AuditActorUser, user.ID, "user.register", "user", user.ID, "", "", auditMetadata("result", "success", "email", user.Email))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneUser(user), nil
}

func (s *Store) Login(email, password string, auditCtx ...AuditContext) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	email = normalizeUserEmail(email)
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) && u.Status == UserStatusActive && encryption.VerifyBcryptPassword(password, u.PasswordHash) {
			s.auditLocked(ctx, AuditActorUser, u.ID, "auth.login", "user", u.ID, "", "", auditMetadata("result", "success", "email", email))
			if err := s.persistLocked(); err != nil {
				return nil, err
			}
			return cloneUser(u), nil
		}
	}
	actorType := AuditActorSystem
	actorUserID := ""
	resourceID := ""
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			actorType = AuditActorUser
			actorUserID = u.ID
			resourceID = u.ID
			break
		}
	}
	s.auditLocked(ctx, actorType, actorUserID, "auth.login", "user", resourceID, "", "", auditMetadata("result", "failure", "email", email))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return nil, ErrUnauthenticated
}

func (s *Store) User(id string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	u, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneUser(u), nil
}

func (s *Store) ActiveUser(id string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	u, ok := s.users[id]
	if !ok || u.Status != UserStatusActive {
		return nil, ErrUnauthenticated
	}
	return cloneUser(u), nil
}

func (s *Store) CreateUser(actorID, email, name, password string, super bool, auditCtx ...AuditContext) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive || !actor.IsSuperAdmin {
		return nil, ErrPermissionDenied
	}
	email = normalizeUserEmail(email)
	if email == "" {
		return nil, fmt.Errorf("%w: email is required", ErrInvalidArgument)
	}
	if err := validateUserPassword(password); err != nil {
		return nil, err
	}
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			return nil, ErrAlreadyExists
		}
	}
	hash, err := encryption.HashPasswordWithBcrypt(password)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	u := &User{ID: id.GenerateID(), Email: email, Name: strings.TrimSpace(name), PasswordHash: hash, IsSuperAdmin: super, Status: UserStatusActive, CreatedAt: now, UpdatedAt: now}
	s.users[u.ID] = u
	s.auditLocked(ctx, AuditActorUser, actorID, "user.create", "user", u.ID, "", "", auditMetadata("result", "success", "email", u.Email, "is_super_admin", fmt.Sprint(super)))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneUser(u), nil
}

func (s *Store) ListUsers(actorID string) ([]*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive || !actor.IsSuperAdmin {
		return nil, ErrPermissionDenied
	}
	out := []*User{}
	for _, u := range s.users {
		out = append(out, cloneUser(u))
	}
	sortUsers(out)
	return out, nil
}

func (s *Store) PatchUser(actorID, userID string, status *int, super *bool, auditCtx ...AuditContext) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive || !actor.IsSuperAdmin {
		return nil, ErrPermissionDenied
	}
	u, ok := s.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	if status != nil {
		if !validUserStatus(*status) {
			return nil, fmt.Errorf("%w: invalid user status", ErrInvalidArgument)
		}
		u.Status = *status
	}
	if super != nil {
		u.IsSuperAdmin = *super
	}
	u.UpdatedAt = time.Now()
	metadata := auditMetadata("result", "success")
	if status != nil {
		metadata["status"] = fmt.Sprint(*status)
	}
	if super != nil {
		metadata["is_super_admin"] = fmt.Sprint(*super)
	}
	s.auditLocked(ctx, AuditActorUser, actorID, "user.patch", "user", userID, "", "", metadata)
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneUser(u), nil
}

func (s *Store) CreateTeam(actorID, name, description string, auditCtx ...AuditContext) (*Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || !actor.IsSuperAdmin {
		return nil, ErrPermissionDenied
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	now := time.Now()
	t := &Team{ID: id.GenerateID(), Name: name, Description: description, CreatedBy: actor.ID, CreatedAt: now, UpdatedAt: now}
	s.teams[t.ID] = t
	s.auditLocked(ctx, AuditActorUser, actorID, "team.create", "team", t.ID, "", "", auditMetadata("result", "success", "name", t.Name))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneTeam(t), nil
}

func (s *Store) ListTeams(actorID string) ([]*Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if _, ok := s.users[actorID]; !ok {
		return nil, ErrUnauthenticated
	}
	out := []*Team{}
	for _, t := range s.teams {
		out = append(out, cloneTeam(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (s *Store) Team(actorID, teamID string) (*Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if _, ok := s.users[actorID]; !ok {
		return nil, ErrUnauthenticated
	}
	t, ok := s.teams[teamID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneTeam(t), nil
}

func (s *Store) UpdateTeam(actorID, teamID, name, description string, auditCtx ...AuditContext) (*Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive || !actor.IsSuperAdmin {
		return nil, ErrPermissionDenied
	}
	t, ok := s.teams[teamID]
	if !ok {
		return nil, ErrNotFound
	}
	if strings.TrimSpace(name) != "" {
		t.Name = strings.TrimSpace(name)
	}
	t.Description = description
	t.UpdatedAt = time.Now()
	s.auditLocked(ctx, AuditActorUser, actorID, "team.update", "team", teamID, "", "", auditMetadata("result", "success", "name", t.Name))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneTeam(t), nil
}

func (s *Store) ArchiveTeam(actorID, teamID string, auditCtx ...AuditContext) (*Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive || !actor.IsSuperAdmin {
		return nil, ErrPermissionDenied
	}
	t, ok := s.teams[teamID]
	if !ok {
		return nil, ErrNotFound
	}
	for _, project := range s.projects {
		if project.TeamID == teamID && project.Status != ProjectStatusArchived {
			return nil, fmt.Errorf("%w: team has active projects", ErrFailedPrecondition)
		}
	}
	archived := cloneTeam(t)
	delete(s.teams, teamID)
	s.auditLocked(ctx, AuditActorUser, actorID, "team.archive", "team", teamID, "", "", auditMetadata("result", "success", "name", archived.Name))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return archived, nil
}

func (s *Store) CreateProject(actorID, teamID, name, description, adminUserID string, auditCtx ...AuditContext) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || !actor.IsSuperAdmin {
		return nil, ErrPermissionDenied
	}
	if _, ok := s.teams[teamID]; !ok {
		return nil, ErrNotFound
	}
	if adminUserID == "" {
		adminUserID = actorID
	}
	if _, ok := s.users[adminUserID]; !ok {
		return nil, ErrNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	now := time.Now()
	p := &Project{ID: id.GenerateID(), TeamID: teamID, Name: name, Description: description, Status: ProjectStatusActive, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	s.projects[p.ID] = p
	s.members[memberKey(p.ID, adminUserID)] = &ProjectMember{ProjectID: p.ID, UserID: adminUserID, Role: MemberRoleAdmin, Status: MemberStatusActive, AddedBy: actorID, CreatedAt: now, UpdatedAt: now}
	s.auditLocked(ctx, AuditActorUser, actorID, "project.create", "project", p.ID, p.ID, "", auditMetadata("result", "success", "team_id", teamID, "admin_user_id", adminUserID, "name", p.Name))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneProject(p), nil
}

func (s *Store) ListProjects(actorID string) ([]*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive {
		return nil, ErrUnauthenticated
	}
	out := []*Project{}
	for _, p := range s.projects {
		if actor.IsSuperAdmin || s.canReadLocked(actorID, p.ID) {
			out = append(out, cloneProject(p))
		}
	}
	sortProjects(out)
	return out, nil
}
func (s *Store) Project(actorID, projectID string) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	p, ok := s.projects[projectID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneProject(p), nil
}

func (s *Store) UpdateProject(actorID, projectID, name, description string, auditCtx ...AuditContext) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	p, ok := s.projects[projectID]
	if !ok {
		return nil, ErrNotFound
	}
	if p.Status == ProjectStatusArchived {
		return nil, ErrFailedPrecondition
	}
	if strings.TrimSpace(name) != "" {
		p.Name = strings.TrimSpace(name)
	}
	p.Description = description
	p.UpdatedAt = time.Now()
	s.auditLocked(ctx, AuditActorUser, actorID, "project.update", "project", projectID, projectID, "", auditMetadata("result", "success", "name", p.Name))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneProject(p), nil
}

func (s *Store) ArchiveProject(actorID, projectID string, auditCtx ...AuditContext) (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	p, ok := s.projects[projectID]
	if !ok {
		return nil, ErrNotFound
	}
	p.Status = ProjectStatusArchived
	p.UpdatedAt = time.Now()
	s.auditLocked(ctx, AuditActorUser, actorID, "project.archive", "project", projectID, projectID, "", auditMetadata("result", "success"))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneProject(p), nil
}

func (s *Store) AddProjectMember(actorID, projectID, userID string, role int, auditCtx ...AuditContext) (*ProjectMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageMembersLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if _, ok := s.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	if _, ok := s.users[userID]; !ok {
		return nil, ErrNotFound
	}
	if role < MemberRoleReader || role > MemberRoleAdmin {
		return nil, ErrInvalidArgument
	}
	now := time.Now()
	m := &ProjectMember{ProjectID: projectID, UserID: userID, Role: role, Status: MemberStatusActive, AddedBy: actorID, CreatedAt: now, UpdatedAt: now}
	s.members[memberKey(projectID, userID)] = m
	s.auditLocked(ctx, AuditActorUser, actorID, "project_member.add", "project_member", userID, projectID, "", auditMetadata("result", "success", "target_user_id", userID, "role", fmt.Sprint(role)))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneMember(m), nil
}

func (s *Store) ListProjectMembers(actorID, projectID string) ([]*ProjectMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if _, ok := s.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	out := []*ProjectMember{}
	for _, member := range s.members {
		if member.ProjectID == projectID && member.Status == MemberStatusActive {
			out = append(out, cloneMember(member))
		}
	}
	sortMembers(out)
	return out, nil
}

func (s *Store) PatchProjectMemberRole(actorID, projectID, userID string, role int, auditCtx ...AuditContext) (*ProjectMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageMembersLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	m, ok := s.members[memberKey(projectID, userID)]
	if !ok || m.Status != MemberStatusActive {
		return nil, ErrNotFound
	}
	if role < MemberRoleReader || role > MemberRoleAdmin {
		return nil, ErrInvalidArgument
	}
	m.Role = role
	m.UpdatedAt = time.Now()
	s.auditLocked(ctx, AuditActorUser, actorID, "project_member.update_role", "project_member", userID, projectID, "", auditMetadata("result", "success", "target_user_id", userID, "role", fmt.Sprint(role)))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneMember(m), nil
}

func (s *Store) RemoveProjectMember(actorID, projectID, userID string, auditCtx ...AuditContext) (*ProjectMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageMembersLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	m, ok := s.members[memberKey(projectID, userID)]
	if !ok || m.Status != MemberStatusActive {
		return nil, ErrNotFound
	}
	m.Status = MemberStatusDisabled
	m.UpdatedAt = time.Now()
	s.auditLocked(ctx, AuditActorUser, actorID, "project_member.remove", "project_member", userID, projectID, "", auditMetadata("result", "success", "target_user_id", userID))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneMember(m), nil
}

func (s *Store) CreateService(actorID, projectID, name, displayName, description, basePath string, auditCtx ...AuditContext) (*APIService, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	project, ok := s.projects[projectID]
	if !ok {
		return nil, ErrNotFound
	}
	if project.Status == ProjectStatusArchived {
		return nil, ErrFailedPrecondition
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidArgument
	}
	for _, svc := range s.services {
		if svc.ProjectID == projectID && strings.EqualFold(svc.Name, name) {
			return nil, ErrAlreadyExists
		}
	}
	now := time.Now()
	svc := &APIService{ID: id.GenerateID(), ProjectID: projectID, Name: name, DisplayName: displayName, Description: description, BasePath: basePath, Status: ServiceStatusActive, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	s.services[svc.ID] = svc
	for _, spec := range []struct {
		name      string
		def, prot bool
	}{{"dev", true, false}, {"test", false, false}, {"prod", false, true}} {
		b := &ContractBranch{ID: id.GenerateID(), ServiceID: svc.ID, Name: spec.name, Kind: BranchKindEnvironment, IsDefault: spec.def, IsProtected: spec.prot, Status: BranchStatusActive, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
		s.branches[b.ID] = b
	}
	s.auditLocked(ctx, AuditActorUser, actorID, "api_service.create", "api_service", svc.ID, projectID, svc.ID, auditMetadata("result", "success", "name", svc.Name, "base_path", svc.BasePath))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneService(svc), nil
}
func (s *Store) ListServices(actorID, projectID string) ([]*APIService, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if _, ok := s.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	out := []*APIService{}
	for _, svc := range s.services {
		if svc.ProjectID == projectID {
			out = append(out, cloneService(svc))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *Store) Service(actorID, projectID, serviceID string) (*APIService, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	svc, ok := s.services[serviceID]
	if !ok || svc.ProjectID != projectID {
		return nil, ErrNotFound
	}
	return cloneService(svc), nil
}

func (s *Store) UpdateService(actorID, projectID, serviceID, name, displayName, description, basePath string, auditCtx ...AuditContext) (*APIService, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	project, ok := s.projects[projectID]
	if !ok {
		return nil, ErrNotFound
	}
	if project.Status == ProjectStatusArchived {
		return nil, ErrFailedPrecondition
	}
	svc, ok := s.services[serviceID]
	if !ok || svc.ProjectID != projectID {
		return nil, ErrNotFound
	}
	if svc.Status == ServiceStatusArchived {
		return nil, ErrFailedPrecondition
	}
	if strings.TrimSpace(name) != "" {
		name = strings.TrimSpace(name)
		for _, other := range s.services {
			if other.ID != serviceID && other.ProjectID == projectID && strings.EqualFold(other.Name, name) {
				return nil, ErrAlreadyExists
			}
		}
		svc.Name = name
	}
	svc.DisplayName = displayName
	svc.Description = description
	svc.BasePath = basePath
	svc.UpdatedAt = time.Now()
	s.auditLocked(ctx, AuditActorUser, actorID, "api_service.update", "api_service", serviceID, projectID, serviceID, auditMetadata("result", "success", "name", svc.Name, "base_path", svc.BasePath))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneService(svc), nil
}

func (s *Store) ArchiveService(actorID, projectID, serviceID string, auditCtx ...AuditContext) (*APIService, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canManageProjectLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	svc := s.services[serviceID]
	svc.Status = ServiceStatusArchived
	svc.UpdatedAt = time.Now()
	s.auditLocked(ctx, AuditActorUser, actorID, "api_service.archive", "api_service", serviceID, projectID, serviceID, auditMetadata("result", "success"))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneService(svc), nil
}
func (s *Store) ListBranches(actorID, projectID, serviceID string) ([]*ContractBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	out := []*ContractBranch{}
	for _, b := range s.branches {
		if b.ServiceID == serviceID {
			out = append(out, cloneBranch(b))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *Store) Branch(actorID, projectID, serviceID, branchID string) (*ContractBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	branch := s.branches[branchID]
	if branch == nil || branch.ServiceID != serviceID {
		return nil, ErrNotFound
	}
	return cloneBranch(branch), nil
}
func (s *Store) CreateBranch(actorID, projectID, serviceID, name, description string, auditCtx ...AuditContext) (*ContractBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canPublishLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceActiveInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	name = strings.TrimSpace(name)
	kind, err := branchKindForName(name)
	if err != nil {
		return nil, err
	}
	for _, b := range s.branches {
		if b.ServiceID == serviceID && b.Name == name {
			return nil, ErrAlreadyExists
		}
	}
	now := time.Now()
	b := &ContractBranch{ID: id.GenerateID(), ServiceID: serviceID, Name: name, Kind: kind, Description: description, Status: BranchStatusActive, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	s.branches[b.ID] = b
	s.auditLocked(ctx, AuditActorUser, actorID, "contract_branch.create", "contract_branch", b.ID, projectID, serviceID, auditMetadata("result", "success", "name", b.Name, "kind", fmt.Sprint(b.Kind)))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneBranch(b), nil
}

func (s *Store) UpdateBranch(actorID, projectID, serviceID, branchID, name, description string, isDefault, isProtected *bool, auditCtx ...AuditContext) (*ContractBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canPublishLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceActiveInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	branch := s.branches[branchID]
	if branch == nil || branch.ServiceID != serviceID {
		return nil, ErrNotFound
	}
	if branch.Status == BranchStatusArchived {
		return nil, ErrFailedPrecondition
	}
	if strings.TrimSpace(name) != "" {
		name = strings.TrimSpace(name)
		kind, err := branchKindForName(name)
		if err != nil {
			return nil, err
		}
		for _, other := range s.branches {
			if other.ID != branchID && other.ServiceID == serviceID && other.Name == name {
				return nil, ErrAlreadyExists
			}
		}
		branch.Name = name
		branch.Kind = kind
	}
	branch.Description = description
	if isDefault != nil {
		if *isDefault {
			for _, other := range s.branches {
				if other.ServiceID == serviceID {
					other.IsDefault = other.ID == branchID
					other.UpdatedAt = time.Now()
				}
			}
		} else {
			branch.IsDefault = false
		}
	}
	if isProtected != nil {
		branch.IsProtected = *isProtected
	}
	branch.UpdatedAt = time.Now()
	metadata := auditMetadata("result", "success", "name", branch.Name, "kind", fmt.Sprint(branch.Kind))
	if isDefault != nil {
		metadata["is_default"] = fmt.Sprint(*isDefault)
	}
	if isProtected != nil {
		metadata["is_protected"] = fmt.Sprint(*isProtected)
	}
	s.auditLocked(ctx, AuditActorUser, actorID, "contract_branch.update", "contract_branch", branchID, projectID, serviceID, metadata)
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneBranch(branch), nil
}

func (s *Store) ArchiveBranch(actorID, projectID, serviceID, branchID string, auditCtx ...AuditContext) (*ContractBranch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canPublishLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	branch := s.branches[branchID]
	if branch == nil || branch.ServiceID != serviceID {
		return nil, ErrNotFound
	}
	branch.Status = BranchStatusArchived
	branch.UpdatedAt = time.Now()
	s.auditLocked(ctx, AuditActorUser, actorID, "contract_branch.archive", "contract_branch", branchID, projectID, serviceID, auditMetadata("result", "success", "name", branch.Name))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneBranch(branch), nil
}

func (s *Store) CreateDraft(actorID, projectID, serviceID string, input DraftInput, auditCtx ...AuditContext) (*ContractDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	return s.createDraftLocked(actorID, projectID, serviceID, input, SourceTypeWebUpload, ctx)
}
func (s *Store) CreateMCPDraft(actorID, projectID, serviceID string, input DraftInput, auditCtx ...AuditContext) (*ContractDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	return s.createDraftLocked(actorID, projectID, serviceID, input, SourceTypeMCPUpload, ctx)
}
func (s *Store) UpdateDraft(actorID, projectID, serviceID, draftID string, input DraftInput, auditCtx ...AuditContext) (*ContractDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canDraftLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	d, ok := s.draftInProjectServiceLocked(projectID, serviceID, draftID)
	if !ok {
		return nil, ErrNotFound
	}
	if !draftCanBeChangedByWriter(d.Status) {
		return nil, ErrFailedPrecondition
	}
	parsed, err := ParseOpenAPI(input.SchemaContent)
	if err != nil {
		return nil, err
	}
	latest := s.latestVersionLocked(serviceID, d.BranchID)
	if latest != nil && latest.NormalizedSchemaHash == sha(parsed.Normalized) {
		return nil, fmt.Errorf("%w: schema has no changes from latest version", ErrFailedPrecondition)
	}
	updated := *d
	updated.VersionName = firstNonEmpty(input.VersionName, d.VersionName)
	updated.Changelog = input.Changelog
	updated.SourceGitCommitID = input.SourceGitCommitID
	updated.SchemaFormat = parsed.SchemaFormat
	updated.RawSchema = input.SchemaContent
	updated.NormalizedSchema = parsed.Normalized
	updated.RawSchemaHash = sha(input.SchemaContent)
	updated.NormalizedSchemaHash = sha(parsed.Normalized)
	rawKey, rawRef, err := s.persistSchemaObjectLocked(projectID, serviceID, updated.BranchID, "draft", updated.ID, "raw", updated.RawSchemaHash, updated.RawSchema)
	if err != nil {
		return nil, err
	}
	normalizedKey, normalizedRef, err := s.persistSchemaObjectLocked(projectID, serviceID, updated.BranchID, "draft", updated.ID, "normalized", updated.NormalizedSchemaHash, updated.NormalizedSchema)
	if err != nil {
		return nil, err
	}
	if err := s.recordObjectRefsLocked(rawRef, normalizedRef); err != nil {
		return nil, err
	}
	updated.RawSchemaObjectKey = rawKey
	updated.NormalizedObjectKey = normalizedKey
	updated.Status = DraftStatusDraft
	updated.DiffPreview = s.previewDiffLocked(serviceID, updated.BranchID, parsed.Endpoints)
	updated.UpdatedAt = time.Now()
	s.drafts[draftID] = &updated
	s.auditLocked(ctx, AuditActorUser, actorID, "contract_draft.update", "contract_draft", draftID, projectID, serviceID, auditMetadata("result", "success", "branch_id", updated.BranchID, "version_name", updated.VersionName, "raw_schema_hash", updated.RawSchemaHash, "normalized_schema_hash", updated.NormalizedSchemaHash))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneDraft(&updated), nil
}
func (s *Store) SubmitDraft(actorID, projectID, serviceID, draftID string, auditCtx ...AuditContext) (*ContractDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canDraftLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	d, ok := s.draftInProjectServiceLocked(projectID, serviceID, draftID)
	if !ok {
		return nil, ErrNotFound
	}
	if !draftCanBeChangedByWriter(d.Status) {
		return nil, ErrFailedPrecondition
	}
	now := time.Now()
	d.Status = DraftStatusSubmitted
	d.SubmittedAt = &now
	d.UpdatedAt = now
	s.auditLocked(ctx, AuditActorUser, actorID, "contract_draft.submit", "contract_draft", draftID, projectID, serviceID, auditMetadata("result", "success", "branch_id", d.BranchID, "version_name", d.VersionName))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneDraft(d), nil
}
func (s *Store) ReviewDraft(actorID, projectID, serviceID, draftID, action string, auditCtx ...AuditContext) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canPublishLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	d, ok := s.draftInProjectServiceLocked(projectID, serviceID, draftID)
	if !ok {
		return nil, ErrNotFound
	}
	switch action {
	case "approve":
		return s.publishDraftLocked(actorID, d, ctx)
	case "request-changes":
		d.Status = DraftStatusChangesRequested
	case "reject":
		d.Status = DraftStatusRejected
	default:
		return nil, ErrInvalidArgument
	}
	d.UpdatedAt = time.Now()
	s.auditLocked(ctx, AuditActorUser, actorID, "contract_draft.review", "contract_draft", draftID, projectID, serviceID, auditMetadata("result", "success", "review_action", action, "branch_id", d.BranchID, "version_name", d.VersionName))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneDraft(d), nil
}
func (s *Store) ListDrafts(actorID, projectID, serviceID string) ([]*ContractDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	out := []*ContractDraft{}
	for _, d := range s.drafts {
		if d.ProjectID == projectID && d.ServiceID == serviceID && s.branchInServiceLocked(d.BranchID, serviceID) {
			out = append(out, cloneDraft(d))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (s *Store) Draft(actorID, projectID, serviceID, draftID string) (*ContractDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	d, ok := s.draftInProjectServiceLocked(projectID, serviceID, draftID)
	if !ok {
		return nil, ErrNotFound
	}
	return cloneDraft(d), nil
}

func (s *Store) DraftSchema(actorID, projectID, serviceID, draftID, kind string) (*SchemaDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	d, ok := s.draftInProjectServiceLocked(projectID, serviceID, draftID)
	if !ok {
		return nil, ErrNotFound
	}
	return schemaDocument("draft", d.ID, kind, d.RawSchema, d.NormalizedSchema, d.RawSchemaObjectKey, d.NormalizedObjectKey, d.RawSchemaHash, d.NormalizedSchemaHash)
}
func (s *Store) PromoteDraft(actorID, projectID, serviceID string, input PromoteInput, auditCtx ...AuditContext) (*ContractDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canPublishLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	if !s.branchInServiceLocked(input.SourceBranchID, serviceID) {
		return nil, ErrNotFound
	}
	v := s.latestVersionLocked(serviceID, input.SourceBranchID)
	if v == nil {
		return nil, ErrNotFound
	}
	return s.createDraftLocked(actorID, projectID, serviceID, DraftInput{BranchID: input.TargetBranchID, VersionName: input.VersionName, Changelog: input.Changelog, SchemaContent: v.RawSchema, SourceGitCommitID: v.SourceGitCommitID, SourceBranchID: input.SourceBranchID, SourceVersionID: v.ID, BaseVersionID: latestID(s.latestVersionLocked(serviceID, input.TargetBranchID))}, SourceTypePromote, ctx)
}

func (s *Store) ListVersions(actorID, projectID, serviceID string) ([]*ContractVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	out := []*ContractVersion{}
	for _, v := range s.versions {
		if v.ProjectID == projectID && v.ServiceID == serviceID {
			out = append(out, cloneVersion(v))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PublishedAt.Equal(out[j].PublishedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].PublishedAt.After(out[j].PublishedAt)
	})
	return out, nil
}
func (s *Store) Version(actorID, projectID, serviceID, versionID string) (*ContractVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	v, ok := s.versions[versionID]
	if !ok || v.ProjectID != projectID || v.ServiceID != serviceID {
		return nil, ErrNotFound
	}
	return cloneVersion(v), nil
}

func (s *Store) VersionSchema(actorID, projectID, serviceID, versionID, kind string) (*SchemaDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	v, ok := s.versions[versionID]
	if !ok || v.ProjectID != projectID || v.ServiceID != serviceID {
		return nil, ErrNotFound
	}
	return schemaDocument("version", v.ID, kind, v.RawSchema, v.NormalizedSchema, v.RawSchemaObjectKey, v.NormalizedObjectKey, v.RawSchemaHash, v.NormalizedSchemaHash)
}
func (s *Store) ListEndpoints(actorID, projectID, serviceID, versionID, pathQuery string) ([]*Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	if !s.versionInProjectLocked(projectID, serviceID, versionID) {
		return nil, ErrNotFound
	}
	out := []*Endpoint{}
	for _, e := range s.endpoints {
		if e.ContractVersionID == versionID && (pathQuery == "" || strings.Contains(e.Path, pathQuery)) {
			out = append(out, cloneEndpointSummary(e))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path+out[i].Method < out[j].Path+out[j].Method })
	return out, nil
}
func (s *Store) Endpoint(actorID, projectID, serviceID, versionID, endpointID string) (*Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	if !s.versionInProjectLocked(projectID, serviceID, versionID) {
		return nil, ErrNotFound
	}
	e, ok := s.endpoints[endpointID]
	if !ok || e.ContractVersionID != versionID {
		return nil, ErrNotFound
	}
	return cloneEndpoint(e), nil
}
func (s *Store) CompareVersions(actorID, projectID, serviceID, fromID, toID string, auditCtx ...AuditContext) (*Diff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	from, ok := s.versions[fromID]
	if !ok || from.ProjectID != projectID || from.ServiceID != serviceID {
		return nil, ErrNotFound
	}
	to, ok := s.versions[toID]
	if !ok || to.ProjectID != projectID || to.ServiceID != serviceID {
		return nil, ErrNotFound
	}
	diff := s.diffVersionsLocked(serviceID, from, to)
	s.diffs[diff.ID] = diff
	s.auditLocked(ctx, AuditActorUser, actorID, "api_version_diff.compare", "api_version_diff", diff.ID, projectID, serviceID, auditMetadata("result", "success", "from_version_id", fromID, "to_version_id", toID))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneDiff(diff), nil
}
func (s *Store) Diff(actorID, projectID, serviceID, diffID string) (*Diff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	if !s.canReadLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	d, ok := s.diffs[diffID]
	if !ok || d.ServiceID != serviceID || !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	if d.FromVersionID != "" && !s.versionInProjectLocked(projectID, serviceID, d.FromVersionID) {
		return nil, ErrNotFound
	}
	if d.ToVersionID != "" && !s.versionInProjectLocked(projectID, serviceID, d.ToVersionID) {
		return nil, ErrNotFound
	}
	return cloneDiff(d), nil
}

func (s *Store) CreateMCPToken(actorID, name string, scopes []int, expiresAt *time.Time, auditCtx ...AuditContext) (*MCPToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive {
		return nil, ErrUnauthenticated
	}
	normalizedScopes, err := normalizeMCPTokenScopes(scopes)
	if err != nil {
		return nil, err
	}
	tokenRaw, err := random.Hex(24)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	secret := "vdoc_" + tokenRaw
	ciphertext, cipherKID, err := encryption.EncryptMCPToken(secret, mcpTokenCipherKey())
	if err != nil {
		return nil, err
	}
	t := &MCPToken{ID: id.GenerateID(), UserID: actorID, Name: firstNonEmpty(name, "default"), TokenHash: sha(secret), TokenCiphertext: ciphertext, CipherKID: cipherKID, Token: secret, Scopes: normalizedScopes, Status: MCPTokenStatusActive, CreatedAt: now, UpdatedAt: now, ExpiresAt: copyTimePtr(expiresAt)}
	s.tokens[t.ID] = t
	s.auditLocked(ctx, AuditActorUser, actorID, "mcp_token.create", "mcp_token", t.ID, "", "", auditMetadata("result", "success", "token_id", t.ID, "name", t.Name, "scopes", intsCSV(normalizedScopes), "expires_at", timePtrString(expiresAt)))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneToken(t, true), nil
}
func (s *Store) ListMCPTokens(actorID string) ([]*MCPToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok {
		return nil, ErrUnauthenticated
	}
	out := []*MCPToken{}
	now := time.Now()
	changed := false
	for _, t := range s.tokens {
		if expireMCPTokenIfNeeded(t, now) {
			changed = true
		}
		if actor.IsSuperAdmin || t.UserID == actorID {
			out = append(out, cloneToken(t, false))
		}
	}
	if changed {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) ListUserMCPTokens(actorID, userID string) ([]*MCPToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive {
		return nil, ErrUnauthenticated
	}
	if !actor.IsSuperAdmin {
		return nil, ErrPermissionDenied
	}
	if _, ok := s.users[userID]; !ok {
		return nil, ErrNotFound
	}
	out := []*MCPToken{}
	now := time.Now()
	changed := false
	for _, token := range s.tokens {
		if expireMCPTokenIfNeeded(token, now) {
			changed = true
		}
		if token.UserID == userID {
			out = append(out, cloneToken(token, false))
		}
	}
	if changed {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) MCPToken(actorID, tokenID string, auditCtx ...AuditContext) (*MCPToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive {
		return nil, ErrUnauthenticated
	}
	t, ok := s.tokens[tokenID]
	if !ok {
		return nil, ErrNotFound
	}
	if t.UserID != actorID {
		return nil, ErrPermissionDenied
	}
	expireMCPTokenIfNeeded(t, time.Now())
	s.auditLocked(ctx, AuditActorUser, actorID, "mcp_token.reveal", "mcp_token", tokenID, "", "", auditMetadata("result", "success", "token_id", tokenID, "status", fmt.Sprint(t.Status)))
	if t.Status != MCPTokenStatusActive {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
		return cloneToken(t, false), nil
	}
	revealed, err := cloneTokenWithSecret(t)
	if err != nil {
		return nil, err
	}
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return revealed, nil
}
func (s *Store) RevokeMCPToken(actorID, tokenID string, auditCtx ...AuditContext) (*MCPToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive {
		return nil, ErrUnauthenticated
	}
	t, ok := s.tokens[tokenID]
	if !ok {
		return nil, ErrNotFound
	}
	if !actor.IsSuperAdmin && t.UserID != actorID {
		return nil, ErrPermissionDenied
	}
	now := time.Now()
	t.Status = MCPTokenStatusRevoked
	t.RevokedAt = &now
	t.RevokedBy = stringPtrValue(actorID)
	t.UpdatedAt = now
	s.auditLocked(ctx, AuditActorUser, actorID, "mcp_token.revoke", "mcp_token", tokenID, "", "", auditMetadata("result", "success", "token_id", tokenID, "owner_user_id", t.UserID))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneToken(t, false), nil
}

func (s *Store) RevokeUserMCPToken(actorID, userID, tokenID string, auditCtx ...AuditContext) (*MCPToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	actor, ok := s.users[actorID]
	if !ok || actor.Status != UserStatusActive {
		return nil, ErrUnauthenticated
	}
	if !actor.IsSuperAdmin {
		return nil, ErrPermissionDenied
	}
	if _, ok := s.users[userID]; !ok {
		return nil, ErrNotFound
	}
	t, ok := s.tokens[tokenID]
	if !ok || t.UserID != userID {
		return nil, ErrNotFound
	}
	now := time.Now()
	t.Status = MCPTokenStatusRevoked
	t.RevokedAt = &now
	t.RevokedBy = stringPtrValue(actorID)
	t.UpdatedAt = now
	s.auditLocked(ctx, AuditActorUser, actorID, "mcp_token.revoke", "mcp_token", tokenID, "", "", auditMetadata("result", "success", "token_id", tokenID, "owner_user_id", t.UserID))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneToken(t, false), nil
}

func (s *Store) AuthenticateMCPToken(token string, auditCtx ...AuditContext) (*MCPToken, *User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := auditContext(auditCtx)
	ctx.ActorType = AuditActorMCPToken
	if err := s.refreshLocked(); err != nil {
		return nil, nil, err
	}
	h := sha(token)
	for _, t := range s.tokens {
		if t.TokenHash != h {
			continue
		}
		now := time.Now()
		if expireMCPTokenIfNeeded(t, now) {
			ctx.ActorTokenID = t.ID
			s.auditLocked(ctx, AuditActorMCPToken, t.UserID, "mcp_token.authenticate", "mcp_token", t.ID, "", "", auditMetadata("result", "failure", "token_id", t.ID, "reason", "expired"))
			_ = s.persistLocked()
			return nil, nil, ErrUnauthenticated
		}
		if t.Status != MCPTokenStatusActive {
			ctx.ActorTokenID = t.ID
			s.auditLocked(ctx, AuditActorMCPToken, t.UserID, "mcp_token.authenticate", "mcp_token", t.ID, "", "", auditMetadata("result", "failure", "token_id", t.ID, "reason", "inactive", "status", fmt.Sprint(t.Status)))
			_ = s.persistLocked()
			return nil, nil, ErrUnauthenticated
		}
		u := s.users[t.UserID]
		if u == nil || u.Status != UserStatusActive {
			ctx.ActorTokenID = t.ID
			s.auditLocked(ctx, AuditActorMCPToken, t.UserID, "mcp_token.authenticate", "mcp_token", t.ID, "", "", auditMetadata("result", "failure", "token_id", t.ID, "reason", "user_inactive"))
			_ = s.persistLocked()
			return nil, nil, ErrUnauthenticated
		}
		t.LastUsedAt = &now
		ctx.ActorTokenID = t.ID
		s.auditLocked(ctx, AuditActorMCPToken, t.UserID, "mcp_token.authenticate", "mcp_token", t.ID, "", "", auditMetadata("result", "success", "token_id", t.ID))
		if err := s.persistLocked(); err != nil {
			return nil, nil, err
		}
		return cloneToken(t, false), cloneUser(u), nil
	}
	s.auditLocked(ctx, AuditActorSystem, "", "mcp_token.authenticate", "mcp_token", "", "", "", auditMetadata("result", "failure", "reason", "not_found"))
	if err := s.persistLocked(); err != nil {
		return nil, nil, err
	}
	return nil, nil, ErrUnauthenticated
}

type DraftInput struct{ BranchID, VersionName, Changelog, SourceGitCommitID, SchemaContent, SourceBranchID, SourceVersionID, BaseVersionID string }
type PromoteInput struct{ SourceBranchID, TargetBranchID, VersionName, Changelog string }

func schemaDocument(ownerType, ownerID, kind, rawContent, normalizedContent, rawObjectKey, normalizedObjectKey, rawHash, normalizedHash string) (*SchemaDocument, error) {
	switch kind {
	case "raw":
		return &SchemaDocument{OwnerType: ownerType, OwnerID: ownerID, Kind: kind, Content: rawContent, ObjectKey: rawObjectKey, Hash: rawHash}, nil
	case "normalized":
		return &SchemaDocument{OwnerType: ownerType, OwnerID: ownerID, Kind: kind, Content: normalizedContent, ObjectKey: normalizedObjectKey, Hash: normalizedHash}, nil
	default:
		return nil, fmt.Errorf("%w: schema kind must be raw or normalized", ErrInvalidArgument)
	}
}

func (s *Store) createDraftLocked(actorID, projectID, serviceID string, input DraftInput, sourceType int, ctx AuditContext) (*ContractDraft, error) {
	if !s.canDraftLocked(actorID, projectID) {
		return nil, ErrPermissionDenied
	}
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, ErrNotFound
	}
	if !s.branchInServiceLocked(input.BranchID, serviceID) {
		return nil, ErrNotFound
	}
	if strings.TrimSpace(input.VersionName) == "" || strings.TrimSpace(input.SchemaContent) == "" {
		return nil, ErrInvalidArgument
	}
	parsed, err := ParseOpenAPI(input.SchemaContent)
	if err != nil {
		return nil, err
	}
	latest := s.latestVersionLocked(serviceID, input.BranchID)
	if latest != nil && latest.NormalizedSchemaHash == sha(parsed.Normalized) {
		return nil, fmt.Errorf("%w: schema has no changes from latest version", ErrFailedPrecondition)
	}
	for _, v := range s.versions {
		if v.ServiceID == serviceID && v.BranchID == input.BranchID && v.VersionName == input.VersionName {
			return nil, ErrAlreadyExists
		}
	}
	now := time.Now()
	d := &ContractDraft{ID: id.GenerateID(), ProjectID: projectID, ServiceID: serviceID, BranchID: input.BranchID, VersionName: input.VersionName, Changelog: input.Changelog, SourceGitCommitID: input.SourceGitCommitID, SchemaFormat: parsed.SchemaFormat, SourceType: sourceType, SourceBranchID: input.SourceBranchID, SourceVersionID: input.SourceVersionID, BaseVersionID: input.BaseVersionID, RawSchema: input.SchemaContent, NormalizedSchema: parsed.Normalized, RawSchemaHash: sha(input.SchemaContent), NormalizedSchemaHash: sha(parsed.Normalized), Status: DraftStatusDraft, CreatedBy: actorID, CreatedAt: now, UpdatedAt: now}
	rawKey, rawRef, err := s.persistSchemaObjectLocked(projectID, serviceID, d.BranchID, "draft", d.ID, "raw", d.RawSchemaHash, d.RawSchema)
	if err != nil {
		return nil, err
	}
	normalizedKey, normalizedRef, err := s.persistSchemaObjectLocked(projectID, serviceID, d.BranchID, "draft", d.ID, "normalized", d.NormalizedSchemaHash, d.NormalizedSchema)
	if err != nil {
		return nil, err
	}
	if err := s.recordObjectRefsLocked(rawRef, normalizedRef); err != nil {
		return nil, err
	}
	d.RawSchemaObjectKey = rawKey
	d.NormalizedObjectKey = normalizedKey
	d.DiffPreview = s.previewDiffLocked(serviceID, input.BranchID, parsed.Endpoints)
	s.drafts[d.ID] = d
	action := "contract_draft.create"
	if sourceType == SourceTypePromote {
		action = "contract_draft.promote"
	}
	s.auditLocked(ctx, AuditActorUser, actorID, action, "contract_draft", d.ID, projectID, serviceID, auditMetadata("result", "success", "branch_id", d.BranchID, "version_name", d.VersionName, "source_type", fmt.Sprint(sourceType), "source_branch_id", d.SourceBranchID, "source_version_id", d.SourceVersionID, "base_version_id", d.BaseVersionID, "raw_schema_hash", d.RawSchemaHash, "normalized_schema_hash", d.NormalizedSchemaHash))
	if err := s.persistLocked(); err != nil {
		return nil, err
	}
	return cloneDraft(d), nil
}

func (s *Store) publishDraftLocked(actorID string, d *ContractDraft, auditCtx AuditContext) (*ContractVersion, error) {
	if d.Status != DraftStatusSubmitted {
		return nil, ErrFailedPrecondition
	}
	service := s.services[d.ServiceID]
	if service == nil || service.ProjectID != d.ProjectID {
		return nil, ErrNotFound
	}
	branch := s.branches[d.BranchID]
	if branch == nil || branch.ServiceID != d.ServiceID {
		return nil, ErrNotFound
	}
	for _, existing := range s.versions {
		if existing.ServiceID == d.ServiceID && existing.BranchID == d.BranchID && existing.VersionName == d.VersionName {
			return nil, ErrAlreadyExists
		}
	}
	parsed, err := ParseOpenAPI(d.RawSchema)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	v := &ContractVersion{ID: id.GenerateID(), ProjectID: d.ProjectID, ServiceID: d.ServiceID, BranchID: d.BranchID, DraftID: d.ID, VersionName: d.VersionName, Changelog: d.Changelog, SourceGitCommitID: d.SourceGitCommitID, SchemaFormat: d.SchemaFormat, SourceType: d.SourceType, SourceBranchID: d.SourceBranchID, SourceVersionID: d.SourceVersionID, BaseVersionID: d.BaseVersionID, RawSchema: d.RawSchema, NormalizedSchema: d.NormalizedSchema, RawSchemaHash: d.RawSchemaHash, NormalizedSchemaHash: d.NormalizedSchemaHash, Status: VersionStatusPublished, PublishedBy: actorID, PublishedAt: now, CreatedAt: now, UpdatedAt: now}
	rawKey, rawRef, err := s.persistSchemaObjectLocked(v.ProjectID, v.ServiceID, v.BranchID, "version", v.ID, "raw", v.RawSchemaHash, v.RawSchema)
	if err != nil {
		return nil, err
	}
	normalizedKey, normalizedRef, err := s.persistSchemaObjectLocked(v.ProjectID, v.ServiceID, v.BranchID, "version", v.ID, "normalized", v.NormalizedSchemaHash, v.NormalizedSchema)
	if err != nil {
		return nil, err
	}
	objectRefs := []domainvdoc.ObjectRef{rawRef, normalizedRef}
	v.RawSchemaObjectKey = rawKey
	v.NormalizedObjectKey = normalizedKey
	newEndpoints := make([]Endpoint, 0, len(parsed.Endpoints))
	for _, ep := range parsed.Endpoints {
		e := ep
		e.ID = id.GenerateID()
		e.ContractVersionID = v.ID
		e.CreatedAt = now
		e.UpdatedAt = now
		newEndpoints = append(newEndpoints, e)
	}
	previous := s.previousVersionLocked(v)
	var diff *Diff
	if previous != nil {
		diff = s.diffEndpointSetsLocked(d.ServiceID, previous.ID, v.ID, s.endpointsForVersionLocked(previous.ID), newEndpoints)
		diffRef, err := s.persistDiffSnapshotLocked(v.ProjectID, v.ServiceID, v.BranchID, diff)
		if err != nil {
			return nil, err
		}
		objectRefs = append(objectRefs, diffRef)
	}
	publishedDraft := *d
	publishedDraft.Status = DraftStatusPublished
	publishedDraft.UpdatedAt = now
	pending := s.cloneStateLocked()
	pending.Versions[v.ID] = v
	pending.Drafts[d.ID] = &publishedDraft
	for index := range newEndpoints {
		e := newEndpoints[index]
		pending.Endpoints[e.ID] = &e
	}
	if diff != nil {
		pending.Diffs[diff.ID] = diff
	}
	appendAuditToState(pending.AuditLogs, auditCtx, AuditActorUser, actorID, "contract_draft.review", "contract_draft", d.ID, d.ProjectID, d.ServiceID, auditMetadata("result", "success", "review_action", "approve", "branch_id", d.BranchID, "version_name", d.VersionName, "version_id", v.ID))
	appendAuditToState(pending.AuditLogs, auditCtx, AuditActorUser, actorID, "api_contract_version.publish", "api_contract_version", v.ID, d.ProjectID, d.ServiceID, auditMetadata("result", "success", "draft_id", d.ID, "branch_id", d.BranchID, "version_name", d.VersionName))
	if s.persistence != nil {
		ctx := context.Background()
		if err := s.persistence.publishLocked(ctx, domainvdoc.PublishStateInput{State: pending, ObjectRefs: objectRefs, ProjectID: d.ProjectID, ServiceID: d.ServiceID, BranchID: d.BranchID, DraftID: d.ID, VersionID: v.ID, VersionName: d.VersionName, ActorID: actorID}); err != nil {
			return nil, err
		}
		if err := s.persistence.load(ctx, s); err != nil {
			return nil, err
		}
		return cloneVersion(s.versions[v.ID]), nil
	}
	s.applyStateLocked(pending)
	return cloneVersion(v), nil
}

func (s *Store) latestVersionLocked(serviceID, branchID string) *ContractVersion {
	var latest *ContractVersion
	for _, v := range s.versions {
		if v.ServiceID == serviceID && v.BranchID == branchID && (latest == nil || v.PublishedAt.After(latest.PublishedAt) || (v.PublishedAt.Equal(latest.PublishedAt) && v.ID > latest.ID)) {
			latest = v
		}
	}
	return latest
}
func (s *Store) previousVersionLocked(v *ContractVersion) *ContractVersion {
	var prev *ContractVersion
	for _, other := range s.versions {
		if other.ID != v.ID && other.ServiceID == v.ServiceID && other.BranchID == v.BranchID && (other.PublishedAt.Before(v.PublishedAt) || (other.PublishedAt.Equal(v.PublishedAt) && other.ID < v.ID)) && (prev == nil || other.PublishedAt.After(prev.PublishedAt) || (other.PublishedAt.Equal(prev.PublishedAt) && other.ID > prev.ID)) {
			prev = other
		}
	}
	return prev
}

func (s *Store) previewDiffLocked(serviceID, branchID string, endpoints []Endpoint) *Diff {
	latest := s.latestVersionLocked(serviceID, branchID)
	if latest == nil {
		return nil
	}
	temp := &ContractVersion{ID: "draft"}
	return s.diffEndpointSetsLocked(serviceID, latest.ID, temp.ID, s.endpointsForVersionLocked(latest.ID), endpoints)
}
func (s *Store) diffVersionsLocked(serviceID string, from, to *ContractVersion) *Diff {
	return s.diffEndpointSetsLocked(serviceID, from.ID, to.ID, s.endpointsForVersionLocked(from.ID), s.endpointsForVersionLocked(to.ID))
}
func (s *Store) endpointsForVersionLocked(versionID string) []Endpoint {
	out := []Endpoint{}
	for _, e := range s.endpoints {
		if e.ContractVersionID == versionID {
			out = append(out, *e)
		}
	}
	return out
}
func (s *Store) diffEndpointSetsLocked(serviceID, fromID, toID string, from, to []Endpoint) *Diff {
	now := time.Now()
	d := &Diff{ID: id.GenerateID(), ServiceID: serviceID, FromVersionID: fromID, ToVersionID: toID, DiffStatus: DiffStatusSucceeded, CreatedAt: now, UpdatedAt: now}
	fm := map[string]Endpoint{}
	tm := map[string]Endpoint{}
	for _, e := range from {
		fm[e.Method+" "+e.Path] = e
	}
	for _, e := range to {
		tm[e.Method+" "+e.Path] = e
	}
	builder := semanticDiffBuilder{}
	for _, key := range sortedStringKeys(tm) {
		te := tm[key]
		if fe, ok := fm[key]; !ok {
			d.Summary.AddedEndpoints++
			builder.add(ChangeEndpointAdded, SeverityInfo, te, "endpoint", "Endpoint added", false, nil, endpointIdentity(te))
		} else {
			before := len(builder.items)
			builder.compareEndpoint(fe, te)
			if len(builder.items) > before {
				d.Summary.ModifiedEndpoints++
			}
		}
	}
	for _, key := range sortedStringKeys(fm) {
		fe := fm[key]
		if _, ok := tm[key]; !ok {
			d.Summary.RemovedEndpoints++
			builder.add(ChangeEndpointRemoved, SeverityBreaking, fe, "endpoint", "Endpoint removed", true, endpointIdentity(fe), nil)
		}
	}
	d.Items = builder.sortedItems()
	for _, item := range d.Items {
		if item.IsBreaking {
			d.Summary.BreakingChanges++
		}
	}
	return d
}
func newDiffItem(change, severity int, e Endpoint, msg string, breaking bool, order int) DiffItem {
	return DiffItem{ID: id.GenerateID(), ChangeType: change, Severity: severity, Method: e.Method, Path: e.Path, OperationID: e.OperationID, Location: "endpoint", Message: msg, FrontendImpact: msg, IsBreaking: breaking, MustHandle: breaking, SortOrder: order}
}

func (s *Store) canReadLocked(userID, projectID string) bool {
	u := s.users[userID]
	if u == nil || u.Status != UserStatusActive {
		return false
	}
	if u.IsSuperAdmin {
		return true
	}
	m := s.members[memberKey(projectID, userID)]
	return m != nil && m.Status == MemberStatusActive
}
func (s *Store) canDraftLocked(userID, projectID string) bool {
	u := s.users[userID]
	if u == nil || u.Status != UserStatusActive {
		return false
	}
	if u.IsSuperAdmin {
		return true
	}
	m := s.members[memberKey(projectID, userID)]
	return m != nil && m.Status == MemberStatusActive && m.Role >= MemberRoleWriter
}
func (s *Store) canPublishLocked(userID, projectID string) bool {
	u := s.users[userID]
	if u == nil || u.Status != UserStatusActive {
		return false
	}
	if u.IsSuperAdmin {
		return true
	}
	m := s.members[memberKey(projectID, userID)]
	return m != nil && m.Status == MemberStatusActive && m.Role == MemberRoleAdmin
}
func (s *Store) canManageProjectLocked(userID, projectID string) bool {
	return s.canPublishLocked(userID, projectID)
}
func (s *Store) canManageMembersLocked(userID, projectID string) bool {
	return s.canPublishLocked(userID, projectID)
}
func (s *Store) serviceInProjectLocked(serviceID, projectID string) bool {
	svc := s.services[serviceID]
	return svc != nil && svc.ProjectID == projectID
}

func (s *Store) serviceActiveInProjectLocked(serviceID, projectID string) bool {
	project := s.projects[projectID]
	svc := s.services[serviceID]
	return project != nil && project.Status != ProjectStatusArchived && svc != nil && svc.ProjectID == projectID && svc.Status != ServiceStatusArchived
}

func (s *Store) branchInServiceLocked(branchID, serviceID string) bool {
	branch := s.branches[branchID]
	return branch != nil && branch.ServiceID == serviceID
}

func (s *Store) draftInProjectServiceLocked(projectID, serviceID, draftID string) (*ContractDraft, bool) {
	if !s.serviceInProjectLocked(serviceID, projectID) {
		return nil, false
	}
	draft := s.drafts[draftID]
	if draft == nil || draft.ProjectID != projectID || draft.ServiceID != serviceID || !s.branchInServiceLocked(draft.BranchID, serviceID) {
		return nil, false
	}
	return draft, true
}

func (s *Store) versionInProjectLocked(projectID, serviceID, versionID string) bool {
	version := s.versions[versionID]
	return version != nil && version.ProjectID == projectID && version.ServiceID == serviceID
}

func memberKey(projectID, userID string) string { return projectID + ":" + userID }
func latestID(version *ContractVersion) string {
	if version == nil {
		return ""
	}
	return version.ID
}
func firstNonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func intsCSV(values []int) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, ",")
}

func timePtrString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func normalizeMCPTokenScopes(scopes []int) ([]int, error) {
	if len(scopes) == 0 {
		return []int{ScopeAPIRead}, nil
	}
	seen := map[int]bool{}
	normalized := make([]int, 0, len(scopes))
	for _, scope := range scopes {
		if scope != ScopeAPIRead && scope != ScopeAPIDraft {
			return nil, fmt.Errorf("%w: invalid mcp token scope", ErrInvalidArgument)
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		normalized = append(normalized, scope)
	}
	if len(normalized) == 0 {
		return []int{ScopeAPIRead}, nil
	}
	return normalized, nil
}

func expireMCPTokenIfNeeded(token *MCPToken, now time.Time) bool {
	if token == nil || token.Status != MCPTokenStatusActive || token.ExpiresAt == nil || now.Before(*token.ExpiresAt) {
		return false
	}
	token.Status = MCPTokenStatusExpired
	token.UpdatedAt = now
	return true
}

func draftCanBeChangedByWriter(status int) bool {
	return status == DraftStatusDraft || status == DraftStatusChangesRequested
}

func cloneTokenWithSecret(token *MCPToken) (*MCPToken, error) {
	clone := cloneToken(token, false)
	if clone == nil {
		return nil, nil
	}
	if token.Token != "" {
		clone.Token = token.Token
		return clone, nil
	}
	secret, err := encryption.DecryptMCPToken(token.TokenCiphertext, mcpTokenCipherKey(), token.CipherKID)
	if err != nil {
		return nil, err
	}
	clone.Token = secret
	return clone, nil
}

func mcpTokenCipherKey() string {
	if strings.TrimSpace(config.MCPTokenCipherKey) != "" {
		return config.MCPTokenCipherKey
	}
	return config.JWTKey
}

func copyTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func stringPtrValue(value string) *string {
	return &value
}

func branchKindForName(name string) (int, error) {
	if name == "" {
		return 0, ErrInvalidArgument
	}
	if name == "dev" || name == "test" || name == "prod" {
		return BranchKindEnvironment, nil
	}
	if strings.HasPrefix(name, "feature/") && strings.TrimSpace(strings.TrimPrefix(name, "feature/")) != "" {
		return BranchKindFeature, nil
	}
	return 0, fmt.Errorf("%w: branch name must be dev, test, prod, or feature/*", ErrInvalidArgument)
}
func sha(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }

func normalizeUserEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateUserPassword(password string) error {
	if strings.TrimSpace(password) != password {
		return fmt.Errorf("%w: password must not have leading or trailing whitespace", ErrInvalidArgument)
	}
	if len(password) < minUserPasswordLength {
		return fmt.Errorf("%w: password must be at least %d characters", ErrInvalidArgument, minUserPasswordLength)
	}
	return nil
}

func validUserStatus(status int) bool {
	return status == UserStatusActive || status == UserStatusDisabled
}

func copyStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	return maps.Clone(input)
}

func cloneUser(u *User) *User {
	if u == nil {
		return nil
	}
	c := *u
	c.PasswordHash = ""
	return &c
}
func cloneTeam(v *Team) *Team {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func cloneProject(v *Project) *Project {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func cloneMember(v *ProjectMember) *ProjectMember {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func cloneService(v *APIService) *APIService {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func cloneBranch(v *ContractBranch) *ContractBranch {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func cloneDraft(v *ContractDraft) *ContractDraft {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func cloneVersion(v *ContractVersion) *ContractVersion {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func cloneEndpoint(v *Endpoint) *Endpoint {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func cloneEndpointSummary(v *Endpoint) *Endpoint {
	c := cloneEndpoint(v)
	if c == nil {
		return nil
	}
	c.Parameters = nil
	c.RequestBody = nil
	c.Responses = nil
	c.Security = nil
	c.Servers = nil
	c.NormalizedOperation = nil
	c.SchemaRefs = nil
	return c
}
func cloneDiff(v *Diff) *Diff {
	if v == nil {
		return nil
	}
	c := *v
	c.Items = append([]DiffItem(nil), v.Items...)
	return &c
}
func cloneAuditLog(v *AuditLog) *AuditLog {
	if v == nil {
		return nil
	}
	c := *v
	c.Metadata = copyStringMap(v.Metadata)
	return &c
}
func cloneToken(v *MCPToken, includeSecret bool) *MCPToken {
	if v == nil {
		return nil
	}
	c := *v
	c.Scopes = append([]int(nil), v.Scopes...)
	c.TokenCiphertext = append([]byte(nil), v.TokenCiphertext...)
	c.ExpiresAt = copyTimePtr(v.ExpiresAt)
	c.RevokedAt = copyTimePtr(v.RevokedAt)
	c.LastUsedAt = copyTimePtr(v.LastUsedAt)
	if v.RevokedBy != nil {
		revokedBy := *v.RevokedBy
		c.RevokedBy = &revokedBy
	}
	if !includeSecret {
		c.Token = ""
	}
	return &c
}
func sortUsers(v []*User)       { sort.Slice(v, func(i, j int) bool { return v[i].Email < v[j].Email }) }
func sortProjects(v []*Project) { sort.Slice(v, func(i, j int) bool { return v[i].Name < v[j].Name }) }
func sortMembers(v []*ProjectMember) {
	sort.Slice(v, func(i, j int) bool { return v[i].UserID < v[j].UserID })
}

func deepCopyMap(in map[string]any) map[string]any {
	var out map[string]any
	b, _ := json.Marshal(in)
	_ = json.Unmarshal(b, &out)
	return out
}
