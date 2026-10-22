package private

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"vdoc/api/middleware"
	app "vdoc/appstore"
	"vdoc/config"
)

const passwordChangePath = "/api/v1/private/identity/me/password"
const newPrivatePassword = "a new private account password"

func TestChangePasswordRouteOnlyChangesAuthenticatedAccount(t *testing.T) {
	oldRate, oldBurst := config.AuthRateLimit, config.AuthRateBurst
	config.AuthRateLimit, config.AuthRateBurst = 100, 100
	middleware.CleanupAllLimiters()
	t.Cleanup(func() {
		config.AuthRateLimit, config.AuthRateBurst = oldRate, oldBurst
		middleware.CleanupAllLimiters()
	})
	router := setupPrivateRouter(t)
	admin, err := app.DefaultStore().Register("change-admin@example.test", "Admin", privateTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	member, err := app.DefaultStore().CreateUser(admin.ID, "change-member@example.test", "Member", privateTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []*app.User{admin, member} {
		t.Run(actor.Email, func(t *testing.T) {
			target := member
			if actor.ID == member.ID {
				target = admin
			}
			// 即使提交其他用户 ID，服务端也只使用当前 JWT 身份。
			body := fmt.Sprintf(`{"current_password":%q,"new_password":%q,"user_id":%q}`, privateTestPassword, newPrivatePassword, target.ID)
			recorder := performPrivateJSON(router, http.MethodPost, passwordChangePath, issuePrivateTestToken(t, actor.ID), body)
			envelope := decodePrivateEnvelope(t, recorder)
			if envelope.Code != 200 || envelope.Status != "OK" {
				t.Fatalf("password change response: %s", recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), privateTestPassword) || strings.Contains(recorder.Body.String(), newPrivatePassword) || strings.Contains(recorder.Body.String(), "password_hash") {
				t.Fatal("password change response exposed credentials")
			}
			if _, err := app.DefaultStore().Login(actor.Email, privateTestPassword); !app.Is(err, app.ErrUnauthenticated) {
				t.Fatalf("old password login = %v", err)
			}
			if _, err := app.DefaultStore().Login(actor.Email, newPrivatePassword); err != nil {
				t.Fatalf("new password login: %v", err)
			}
		})
		if actor.ID == admin.ID {
			if _, err := app.DefaultStore().Login(member.Email, privateTestPassword); err != nil {
				t.Fatalf("admin changed another user's password: %v", err)
			}
		}
	}
}

func TestChangePasswordRouteReturnsRecoverableValidationErrors(t *testing.T) {
	oldRate, oldBurst := config.AuthRateLimit, config.AuthRateBurst
	config.AuthRateLimit, config.AuthRateBurst = 100, 100
	middleware.CleanupAllLimiters()
	t.Cleanup(func() {
		config.AuthRateLimit, config.AuthRateBurst = oldRate, oldBurst
		middleware.CleanupAllLimiters()
	})
	router := setupPrivateRouter(t)
	user, err := app.DefaultStore().Register("change-validation@example.test", "User", privateTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	token := issuePrivateTestToken(t, user.ID)
	for _, test := range []struct {
		name, body string
	}{
		{"missing current", `{"new_password":"a new private account password"}`},
		{"wrong current", `{"current_password":"incorrect old password","new_password":"a new private account password"}`},
		{"weak new password", `{"current_password":"correct horse battery staple","new_password":"short"}`},
		{"unchanged password", `{"current_password":"correct horse battery staple","new_password":"correct horse battery staple"}`},
		{"malformed", `{`},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := performPrivateJSON(router, http.MethodPost, passwordChangePath, token, test.body)
			envelope := decodePrivateEnvelope(t, recorder)
			if envelope.Code != 400 || envelope.Status != "INVALID_ARGUMENT" {
				t.Fatalf("validation response = %s", recorder.Body.String())
			}
			if _, err := app.DefaultStore().Login(user.Email, privateTestPassword); err != nil {
				t.Fatalf("rejected password change replaced credentials: %v", err)
			}
		})
	}
	identity := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodGet, "/api/v1/private/identity/me", token, ""))
	if identity.Code != 200 {
		t.Fatal("validation errors invalidated the current session")
	}
}

func TestChangePasswordRouteRejectsMissingOrDisabledIdentity(t *testing.T) {
	router := setupPrivateRouter(t)
	body := `{"current_password":"correct horse battery staple","new_password":"a new private account password"}`
	envelope := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, passwordChangePath, "", body))
	if envelope.Code != 401 || envelope.Status != "UNAUTHENTICATED" {
		t.Fatalf("missing token response = %+v", envelope)
	}
	admin, err := app.DefaultStore().Register("change-active@example.test", "Admin", privateTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := app.DefaultStore().CreateUser(admin.ID, "change-disabled@example.test", "Disabled", privateTestPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	status := app.UserStatusDisabled
	if _, err := app.DefaultStore().PatchUser(admin.ID, disabled.ID, &status, nil); err != nil {
		t.Fatal(err)
	}
	envelope = decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, passwordChangePath, issuePrivateTestToken(t, disabled.ID), body))
	if envelope.Code != 401 || envelope.Status != "UNAUTHENTICATED" {
		encoded, _ := json.Marshal(envelope)
		t.Fatalf("disabled identity response = %s", encoded)
	}
}

func TestChangePasswordRouteHasIndependentRateLimit(t *testing.T) {
	oldRate, oldBurst := config.AuthRateLimit, config.AuthRateBurst
	config.AuthRateLimit, config.AuthRateBurst = 1, 1
	middleware.CleanupAllLimiters()
	t.Cleanup(func() {
		config.AuthRateLimit, config.AuthRateBurst = oldRate, oldBurst
		middleware.CleanupAllLimiters()
	})
	router := setupPrivateRouter(t)
	user, err := app.DefaultStore().Register("change-rate@example.test", "User", privateTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	token := issuePrivateTestToken(t, user.ID)
	first := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, passwordChangePath, token, `{}`))
	second := decodePrivateEnvelope(t, performPrivateJSON(router, http.MethodPost, passwordChangePath, token, `{}`))
	if first.Code != 400 || second.Code != 429 || second.Status != "RESOURCE_EXHAUSTED" {
		t.Fatalf("rate limiter responses: first=%+v second=%+v", first, second)
	}
}
