package private

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRepositoryAutomationHTTPEnvelopesRedactSecretsAndEnforceInputs(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	base := "/api/v1/private/projects/" + fixture.project.ID + "/repository"
	createdResponse := performPrivateJSON(fixture.router, http.MethodPost, base+"/subscriptions", fixture.adminToken, `{"name":"Webhook","kind":"webhook","url":"https://8.8.8.8/hook"}`)
	created := decodePrivateEnvelope(t, createdResponse)
	var subscription struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	if created.Code != 200 || json.Unmarshal(created.Detail, &subscription) != nil || subscription.ID == "" || len(subscription.Secret) != 64 {
		t.Fatalf("create envelope=%s", createdResponse.Body.String())
	}
	listedResponse := performPrivateJSON(fixture.router, http.MethodGet, base+"/subscriptions", fixture.adminToken, "")
	listed := decodePrivateEnvelope(t, listedResponse)
	if listed.Code != 200 || strings.Contains(string(listed.Detail), subscription.Secret) || strings.Contains(string(listed.Detail), "cipher") || strings.Contains(string(listed.Detail), `"secret"`) {
		t.Fatalf("redacted list envelope=%s", listedResponse.Body.String())
	}
	for _, action := range []string{"pause", "resume"} {
		response := performPrivateJSON(fixture.router, http.MethodPost, base+"/subscriptions/"+subscription.ID+"/"+action, fixture.adminToken, "")
		if envelope := decodePrivateEnvelope(t, response); envelope.Code != 200 || strings.Contains(string(envelope.Detail), `"secret"`) {
			t.Fatalf("%s envelope=%s", action, response.Body.String())
		}
	}
	for _, scenario := range []struct {
		method, path, token, body string
		code                      int
	}{
		{http.MethodPost, "/subscriptions", fixture.writerToken, `{"name":"Denied","kind":"in_app"}`, 403},
		{http.MethodPost, "/subscriptions", fixture.adminToken, `{"name":"Bad","kind":"in_app","version_name":"client-version"}`, 400},
		{http.MethodGet, "/ai/analyses?after=-1", fixture.adminToken, "", 400},
		{http.MethodGet, "/event-deliveries?after=bad", fixture.adminToken, "", 400},
		{http.MethodGet, "/event-deliveries", fixture.writerToken, "", 403},
		{http.MethodPost, "/ai/analyses", fixture.adminToken, `{"to_commit_id":"missing","version_name":"client-version"}`, 400},
		{http.MethodGet, "/ai/analyses", fixture.writerToken, "", 200},
	} {
		response := performPrivateJSON(fixture.router, scenario.method, base+scenario.path, scenario.token, scenario.body)
		if envelope := decodePrivateEnvelope(t, response); envelope.Code != scenario.code {
			t.Fatalf("%s %s envelope=%s", scenario.method, scenario.path, response.Body.String())
		}
	}
}
