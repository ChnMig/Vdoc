package mcp

import (
	"errors"
	"testing"

	"github.com/gin-gonic/gin"
	app "vdoc/appstore"
)

func TestMCPDiscoversProjectsByNameAcrossTeamsWithoutLeakingAccess(t *testing.T) {
	f := newMCPFixture(t)
	s := app.DefaultStore()
	team, err := s.CreateTeam(f.superID, "Mobile Team", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateProject(f.superID, team.ID, "MCP Project", "", f.superID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddProjectMember(f.superID, second.ID, f.writerID, app.MemberRoleWriter); err != nil {
		t.Fatal(err)
	}
	hiddenTeam, err := s.CreateTeam(f.superID, "Restricted Team", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateProject(f.superID, hiddenTeam.ID, "MCP Project", "", f.superID); err != nil {
		t.Fatal(err)
	}
	token, err := s.CreateMCPToken(f.writerID, "cross-project-discovery", []int{app.ScopeDocRead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var projects []mcpProjectDTO
	repositoryMCPCall(t, f, token.Token, "list_projects", gin.H{"query": " mcp PROJ "}, &projects)
	if len(projects) != 2 || projects[0].TeamName == "" || projects[1].TeamName == "" || projects[0].TeamID == projects[1].TeamID {
		t.Fatalf("ambiguous names lost candidates/team names: %+v", projects)
	}
	for _, args := range []gin.H{{"team_id": team.ID}, {"team_name": " mobile TEAM "}, {"query": "Project", "team_id": team.ID, "team_name": "Mobile Team"}} {
		repositoryMCPCall(t, f, token.Token, "list_projects", args, &projects)
		if len(projects) != 1 || projects[0].ID != second.ID {
			t.Fatalf("filters %v: %+v", args, projects)
		}
	}
	repositoryMCPCall(t, f, token.Token, "list_projects", gin.H{"team_id": hiddenTeam.ID}, &projects)
	if len(projects) != 0 {
		t.Fatalf("inaccessible team leaked: %+v", projects)
	}
	var teams []mcpTeamDTO
	repositoryMCPCall(t, f, token.Token, "list_teams", gin.H{}, &teams)
	if len(teams) != 2 {
		t.Fatalf("teams=%+v", teams)
	}
	for _, got := range teams {
		if got.ID == hiddenTeam.ID || got.ProjectCount != 1 {
			t.Fatalf("team counts leak projects: %+v", teams)
		}
	}
	repositoryMCPCall(t, f, token.Token, "list_teams", gin.H{"query": " MOBILE "}, &teams)
	if len(teams) != 1 || teams[0].ID != team.ID {
		t.Fatalf("team query: %+v", teams)
	}
	for _, tool := range []string{"list_teams", "list_projects"} {
		if _, err := execute(f.writerID, []int{app.ScopeDocDraft}, tool, mustJSON(t, gin.H{})); !errors.Is(err, app.ErrPermissionDenied) {
			t.Fatalf("write-only discovery %s: %v", tool, err)
		}
		if _, err := execute(f.writerID, []int{app.ScopeAPIRead}, tool, mustJSON(t, gin.H{})); err != nil {
			t.Fatalf("API read discovery %s: %v", tool, err)
		}
		assertRPCError(t, callMCPToolRPC(t, f.router, token.Token, tool, gin.H{"unknown": ""}), -32602, "unknown discovery filter")
	}
	assertRPCError(t, callMCPToolRPC(t, f.router, token.Token, "list_teams", gin.H{"team_id": ""}), -32602, "list_teams must follow its schema")
}
