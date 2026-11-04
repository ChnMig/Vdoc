package private

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"vdoc/api/app/v1/private/shared"
	app "vdoc/appstore"
)

func TestPrivateMCPCapabilityDTOAndOwnerIsolation(t *testing.T) {
	router := setupPrivateRouter(t)
	store := app.DefaultStore()
	super, err := store.Register("scope-super@example.test", "Super", privateTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateUser(super.ID, "scope-owner@example.test", "Owner", privateTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(super.ID, "scope-other@example.test", "Other", privateTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		owner  string
		scopes []int
		model  string
		names  []string
	}{
		{owner.ID, []int{5, 6}, "capability", []string{"read", "write"}},
		{other.ID, []int{1, 4}, "legacy", []string{"api:read", "doc:draft"}},
		{super.ID, []int{1, 6}, "mixed", []string{"api:read", "write"}},
	} {
		jwt := issuePrivateTestToken(t, test.owner)
		body, err := json.Marshal(map[string]any{"name": test.model, "scopes": test.scopes})
		if err != nil {
			t.Fatal(err)
		}
		createdEnvelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, "/api/v1/private/mcp-tokens", jwt, string(body)))
		var created shared.MCPTokenDTO
		if createdEnvelope.Code != 200 || json.Unmarshal(createdEnvelope.Detail, &created) != nil || created.Token == "" {
			t.Fatalf("create envelope=%+v", createdEnvelope)
		}
		for _, path := range []string{"/api/v1/private/mcp-tokens", "/api/v1/private/mcp-tokens/" + created.ID} {
			envelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, path, jwt, ""))
			var got shared.MCPTokenDTO
			if path == "/api/v1/private/mcp-tokens" {
				var list []shared.MCPTokenDTO
				if err = json.Unmarshal(envelope.Detail, &list); err != nil || len(list) != 1 {
					t.Fatalf("own list=%s, %v", envelope.Detail, err)
				}
				got = list[0]
				if got.Token != "" {
					t.Fatal("list exposed private plaintext")
				}
			} else if err = json.Unmarshal(envelope.Detail, &got); err != nil || got.Token != created.Token {
				t.Fatalf("owner reveal failed: %v", err)
			}
			if envelope.Code != 200 || got.UserID != test.owner || got.ScopeModel != test.model || !reflect.DeepEqual(got.Scopes, test.scopes) || !reflect.DeepEqual(got.ScopeNames, test.names) {
				t.Fatalf("scope metadata=%+v", got)
			}
		}
		for _, actor := range []string{owner.ID, other.ID, super.ID} {
			if actor == test.owner {
				continue
			}
			for _, request := range []struct{ method, path string }{
				{http.MethodGet, "/api/v1/private/mcp-tokens/" + created.ID},
				{http.MethodPost, "/api/v1/private/mcp-tokens/" + created.ID + "/revoke"},
				{http.MethodGet, "/api/v1/private/mcp-usage?token_id=" + created.ID},
				{http.MethodGet, "/api/v1/private/system/users/" + test.owner + "/mcp-tokens"},
				{http.MethodPost, "/api/v1/private/system/users/" + test.owner + "/mcp-tokens/" + created.ID + "/revoke"},
			} {
				response := performPrivateJSON(router, request.method, request.path, issuePrivateTestToken(t, actor), "")
				envelope := decodePrivateEnvelope(t, response)
				if envelope.Code != 403 || envelope.Status != "PERMISSION_DENIED" || bytes.Contains(response.Body.Bytes(), []byte(created.Token)) {
					t.Fatalf("actor %s %s %s: %s", actor, request.method, request.path, response.Body.String())
				}
			}
		}
		if _, _, err = store.AuthenticateMCPToken(created.Token); err != nil {
			t.Fatalf("denied cross-owner operations revoked token: %v", err)
		}
	}
}
