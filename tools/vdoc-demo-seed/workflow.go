package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func seedWorkspace(ctx context.Context, client apiClient, cfg config) (seedResult, error) {
	adminEmail := cfg.Email
	password := cfg.Password
	if adminEmail == "" {
		adminEmail = "demo-admin-" + cfg.RunID + "@example.test"
		password = defaultPassword
		if _, err := send[authDetail](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/open/auth/register", Body: authPayload(adminEmail, "Demo Admin", password)}); err != nil {
			return seedResult{}, fmt.Errorf("register admin: %w", err)
		}
	}
	admin, err := send[authDetail](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/open/auth/login", Body: authPayload(adminEmail, "", password)})
	if err != nil {
		return seedResult{}, fmt.Errorf("login admin: %w", err)
	}
	writerEmail := "demo-writer-" + cfg.RunID + "@example.test"
	writer, err := send[userDetail](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/private/system/users", Token: admin.Token, Body: authPayload(writerEmail, "Demo Writer", defaultPassword)})
	if err != nil {
		return seedResult{}, fmt.Errorf("create writer: %w", err)
	}
	writerLogin, err := send[authDetail](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/open/auth/login", Body: authPayload(writer.Email, "", defaultPassword)})
	if err != nil {
		return seedResult{}, fmt.Errorf("login writer: %w", err)
	}
	team, project, err := createTeamProject(ctx, client, teamProjectInput{Admin: admin, RunID: cfg.RunID})
	if err != nil {
		return seedResult{}, err
	}
	if _, err := send[resourceID](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/private/projects/" + project.ID + "/members", Token: admin.Token, Body: map[string]any{"user_id": writer.ID, "role": roleWriter}}); err != nil {
		return seedResult{}, fmt.Errorf("add writer member: %w", err)
	}
	document, branchID, err := createDocumentBranch(ctx, client, documentBranchInput{AdminToken: admin.Token, ProjectID: project.ID, RunID: cfg.RunID})
	if err != nil {
		return seedResult{}, err
	}
	versionOne, err := publishDraft(ctx, client, draftInput{ProjectID: project.ID, DocumentID: document.ID, BranchID: branchID, WriterToken: writerLogin.Token, AdminToken: admin.Token, VersionName: "1.0.0", Schema: openAPI("listPets", false)})
	if err != nil {
		return seedResult{}, err
	}
	versionTwo, err := publishDraft(ctx, client, draftInput{ProjectID: project.ID, DocumentID: document.ID, BranchID: branchID, WriterToken: writerLogin.Token, AdminToken: admin.Token, VersionName: "1.1.0", Schema: openAPI("listPets", true)})
	if err != nil {
		return seedResult{}, err
	}
	diff, err := send[resourceID](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/private/projects/" + project.ID + "/documents/" + document.ID + "/diffs", Token: admin.Token, Body: map[string]string{"from_version_id": versionOne.ID, "to_version_id": versionTwo.ID}})
	if err != nil {
		return seedResult{}, fmt.Errorf("create diff: %w", err)
	}
	token, err := send[mcpToken](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/private/mcp-tokens", Token: admin.Token, Body: map[string]any{"name": "Demo Seed " + cfg.RunID, "scopes": []int{scopeAPIRead, scopeAPIDraft}}})
	if err != nil {
		return seedResult{}, fmt.Errorf("create MCP token: %w", err)
	}
	return seedResult{AdminEmail: admin.User.Email, WriterEmail: writer.Email, TeamID: team.ID, ProjectID: project.ID, DocumentID: document.ID, VersionOneID: versionOne.ID, VersionTwoID: versionTwo.ID, DiffID: diff.ID, MCPTokenID: token.ID}, nil
}

type teamProjectInput struct {
	Admin authDetail
	RunID string
}

func createTeamProject(ctx context.Context, client apiClient, input teamProjectInput) (resourceID, resourceID, error) {
	team, err := send[resourceID](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/private/teams", Token: input.Admin.Token, Body: map[string]string{"name": "Demo Team " + input.RunID, "description": "Local seed team"}})
	if err != nil {
		return resourceID{}, resourceID{}, fmt.Errorf("create team: %w", err)
	}
	project, err := send[resourceID](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/private/projects", Token: input.Admin.Token, Body: map[string]string{"team_id": team.ID, "name": "Demo Project " + input.RunID, "description": "Local seed project", "admin_user_id": input.Admin.User.ID}})
	if err != nil {
		return resourceID{}, resourceID{}, fmt.Errorf("create project: %w", err)
	}
	return team, project, nil
}

type documentBranchInput struct {
	AdminToken string
	ProjectID  string
	RunID      string
}

func createDocumentBranch(ctx context.Context, client apiClient, input documentBranchInput) (resourceID, string, error) {
	document, err := send[resourceID](ctx, client, request{Method: http.MethodPost, Path: "/api/v1/private/projects/" + input.ProjectID + "/documents", Token: input.AdminToken, Body: map[string]any{"name": "demo-openapi-" + input.RunID, "document_type": docTypeOpenAPI, "relative_path": "apis/demo-" + input.RunID + ".yaml", "description": "Local seed OpenAPI"}})
	if err != nil {
		return resourceID{}, "", fmt.Errorf("create document: %w", err)
	}
	branches, err := send[[]branch](ctx, client, request{Method: http.MethodGet, Path: "/api/v1/private/projects/" + input.ProjectID + "/documents/" + document.ID + "/branches", Token: input.AdminToken})
	if err != nil {
		return resourceID{}, "", fmt.Errorf("get branches: %w", err)
	}
	for _, b := range branches {
		if b.Name == "dev" {
			return document, b.ID, nil
		}
	}
	return resourceID{}, "", fmt.Errorf("dev branch not found")
}

type draftInput struct {
	ProjectID   string
	DocumentID  string
	BranchID    string
	WriterToken string
	AdminToken  string
	VersionName string
	Schema      string
}

func publishDraft(ctx context.Context, client apiClient, input draftInput) (resourceID, error) {
	path := "/api/v1/private/projects/" + input.ProjectID + "/documents/" + input.DocumentID + "/drafts"
	draft, err := send[resourceID](ctx, client, request{Method: http.MethodPost, Path: path, Token: input.WriterToken, Body: map[string]string{"branch_id": input.BranchID, "version_name": input.VersionName, "changelog": "Publish " + input.VersionName, "source_git_commit_id": "demo-" + strings.ReplaceAll(input.VersionName, ".", ""), "schema_content": input.Schema}})
	if err != nil {
		return resourceID{}, fmt.Errorf("create draft %s: %w", input.VersionName, err)
	}
	if _, err := send[resourceID](ctx, client, request{Method: http.MethodPost, Path: path + "/" + draft.ID + "/submit", Token: input.WriterToken}); err != nil {
		return resourceID{}, fmt.Errorf("submit draft %s: %w", input.VersionName, err)
	}
	version, err := send[resourceID](ctx, client, request{Method: http.MethodPost, Path: path + "/" + draft.ID + "/approve", Token: input.AdminToken})
	if err != nil {
		return resourceID{}, fmt.Errorf("approve draft %s: %w", input.VersionName, err)
	}
	return version, nil
}

func renderResult(out io.Writer, result seedResult, baseURL string) {
	fmt.Fprintf(out, "Seeded Vdoc demo workspace at %s\n", baseURL)
	fmt.Fprintf(out, "admin_email=%s\nwriter_email=%s\n", result.AdminEmail, result.WriterEmail)
	fmt.Fprintf(out, "team_id=%s\nproject_id=%s\ndocument_id=%s\n", result.TeamID, result.ProjectID, result.DocumentID)
	fmt.Fprintf(out, "version_ids=%s,%s\ndiff_id=%s\nmcp_token_id=%s\n", result.VersionOneID, result.VersionTwoID, result.DiffID, result.MCPTokenID)
	fmt.Fprintln(out, "Next commands:")
	fmt.Fprintf(out, "  export VDOC_BASE_URL=%s\n", baseURL)
	fmt.Fprintln(out, "  export VDOC_MCP_TOKEN=<redacted>")
}

func authPayload(email, name, password string) map[string]string {
	payload := map[string]string{"email": email, "password": password}
	if name != "" {
		payload["name"] = name
	}
	return payload
}

func openAPI(operationID string, includeName bool) string {
	nameRequired := ""
	nameProperty := ""
	if includeName {
		nameRequired = `,"name"`
		nameProperty = `,"name":{"type":"string"}`
	}
	return `{"openapi":"3.1.0","info":{"title":"Demo API","version":"1.0.0"},"paths":{"/pets":{"get":{"operationId":"` + operationID + `","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"type":"object","required":["id"` + nameRequired + `],"properties":{"id":{"type":"string"}` + nameProperty + `}}}}}}}}}}`
}
