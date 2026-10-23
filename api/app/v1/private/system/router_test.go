package system

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vdoc/api/middleware"
	app "vdoc/appstore"
	"vdoc/config"
	"vdoc/domain/release"
	systemsvc "vdoc/services/system"
	"vdoc/utils/authentication"

	"github.com/gin-gonic/gin"
)

type checkFunc func(context.Context, string) (systemsvc.UpdateResult, error)

func (fn checkFunc) Check(ctx context.Context, version string) (systemsvc.UpdateResult, error) {
	return fn(ctx, version)
}

type testEnvelope struct {
	Code   int             `json:"code"`
	Status string          `json:"status"`
	Detail json.RawMessage `json:"detail"`
}

func setupRouter(t *testing.T, checker updateChecker) (*gin.Engine, *app.User, *app.User) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	oldKey, oldExpiration := config.JWTKey, config.JWTExpiration
	config.JWTKey, config.JWTExpiration = "system-api-test-key-with-more-than-32-bytes", time.Hour
	app.ResetDefaultStoreForTest()
	t.Cleanup(func() {
		gin.SetMode(oldMode)
		config.JWTKey, config.JWTExpiration = oldKey, oldExpiration
		app.ResetDefaultStoreForTest()
	})
	admin, err := app.DefaultStore().Register("system-admin@example.test", "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	member, err := app.DefaultStore().CreateUser(admin.ID, "system-member@example.test", "Member", "correct horse battery staple", false)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	private := router.Group("/api/v1/private", middleware.TokenVerify)
	registerRoutes(private, func() systemsvc.VersionInfo {
		return systemsvc.VersionInfo{Version: "v0.3.17-beta.4", GitCommit: "abc1234", BuildTime: "2026-10-23_10:00:00", Channel: release.Beta}
	}, checker)
	return router, admin, member
}

func tokenFor(t *testing.T, userID string) string {
	t.Helper()
	token, err := authentication.JWTIssue(map[string]any{"user_id": userID})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func requestSystem(t *testing.T, router *gin.Engine, path, token string) (testEnvelope, []byte) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/private/system/"+path, nil)
	if token != "" {
		request.Header.Set(middleware.AuthorizationHeader, token)
	}
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d; body = %s", recorder.Code, recorder.Body.String())
	}
	var envelope testEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope, recorder.Body.Bytes()
}

func TestVersionAndUpdatesAreAvailableToAllActiveUsers(t *testing.T) {
	calls := 0
	router, admin, member := setupRouter(t, checkFunc(func(_ context.Context, current string) (systemsvc.UpdateResult, error) {
		calls++
		if current != "v0.3.17-beta.4" {
			t.Fatalf("current version = %q", current)
		}
		return systemsvc.UpdateResult{CurrentVersion: current, Channel: release.Beta}, nil
	}))
	for _, user := range []*app.User{admin, member} {
		token := tokenFor(t, user.ID)
		envelope, body := requestSystem(t, router, "version", token)
		var version versionDTO
		if err := json.Unmarshal(envelope.Detail, &version); err != nil {
			t.Fatal(err)
		}
		if envelope.Status != "OK" || envelope.Code != 200 || version != (versionDTO{"v0.3.17-beta.4", "abc1234", "2026-10-23_10:00:00", "beta"}) {
			t.Fatalf("version response = %s", body)
		}
		if calls != 0 {
			t.Fatal("version read initiated an update check")
		}
	}
	for _, user := range []*app.User{admin, member} {
		envelope, body := requestSystem(t, router, "updates", tokenFor(t, user.ID))
		var detail map[string]any
		if err := json.Unmarshal(envelope.Detail, &detail); err != nil {
			t.Fatal(err)
		}
		if envelope.Code != 200 || detail["current_version"] != "v0.3.17-beta.4" || detail["channel"] != "beta" || detail["update_available"] != false {
			t.Fatalf("updates response = %s", body)
		}
		for _, key := range []string{"latest_version", "release_url", "published_at"} {
			if value, exists := detail[key]; !exists || value != nil {
				t.Fatalf("no-release %s must be explicit null: %s", key, body)
			}
		}
	}
	if calls != 2 {
		t.Fatalf("update checker calls = %d", calls)
	}
}

func TestSystemRoutesRejectMissingInvalidAndDisabledIdentities(t *testing.T) {
	router, admin, member := setupRouter(t, checkFunc(func(context.Context, string) (systemsvc.UpdateResult, error) {
		t.Fatal("unauthenticated request reached update checker")
		return systemsvc.UpdateResult{}, nil
	}))
	status := app.UserStatusDisabled
	if _, err := app.DefaultStore().PatchUser(admin.ID, member.ID, &status, nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"version", "updates"} {
		for _, token := range []string{"", "invalid", tokenFor(t, "missing-user"), tokenFor(t, member.ID)} {
			envelope, body := requestSystem(t, router, path, token)
			if envelope.Code != 401 || envelope.Status != "UNAUTHENTICATED" {
				t.Fatalf("%s authentication response = %s", path, body)
			}
		}
	}
}

func TestSystemUpdatesPreserveFailureStatus(t *testing.T) {
	for _, test := range []struct {
		name, status string
		code         int
		err          error
	}{
		{"development", "FAILED_PRECONDITION", 400, systemsvc.ErrUnsupportedVersion},
		{"network", "UNAVAILABLE", 503, errors.New("network unavailable")},
		{"timeout", "DEADLINE_EXCEEDED", 504, context.DeadlineExceeded},
		{"cancel", "CANCELLED", 499, context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, admin, _ := setupRouter(t, checkFunc(func(context.Context, string) (systemsvc.UpdateResult, error) {
				return systemsvc.UpdateResult{}, test.err
			}))
			envelope, body := requestSystem(t, router, "updates", tokenFor(t, admin.ID))
			if envelope.Code != test.code || envelope.Status != test.status || len(envelope.Detail) != 0 {
				t.Fatalf("failure response = %s", body)
			}
		})
	}
}

func TestSystemUpdatesExposeOnlyExplicitReleaseFields(t *testing.T) {
	latest, url := "v0.3.17-beta.5", "https://github.com/ChnMig/Vdoc-site/releases/tag/v0.3.17-beta.5"
	published := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	router, admin, _ := setupRouter(t, checkFunc(func(ctx context.Context, current string) (systemsvc.UpdateResult, error) {
		if ctx == nil {
			t.Fatal("request context was not passed through")
		}
		return systemsvc.UpdateResult{CurrentVersion: current, Channel: release.Beta, LatestVersion: &latest, UpdateAvailable: true, ReleaseURL: &url, PublishedAt: &published}, nil
	}))
	envelope, body := requestSystem(t, router, "updates", tokenFor(t, admin.ID))
	var detail updatesDTO
	if err := json.Unmarshal(envelope.Detail, &detail); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 200 || !detail.UpdateAvailable || detail.LatestVersion == nil || *detail.LatestVersion != latest || detail.ReleaseURL == nil || *detail.ReleaseURL != url || detail.PublishedAt == nil || !detail.PublishedAt.Equal(published) {
		t.Fatalf("updates response = %s", body)
	}
}
