package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUTF8CredentialsRegisterAndLogin(t *testing.T) {
	for _, password := range []string{"密码密码", "🔐🔑🗝"} {
		t.Run(password, func(t *testing.T) {
			router := setupAuthRouter(t)
			if len(password) != 12 {
				t.Fatal("fixture must be exactly 12 UTF-8 bytes")
			}
			var registeredID string
			for _, requestCase := range []struct {
				action, password, status string
				code                     int
			}{
				{"register", password, "OK", 200},
				{"login", password, "OK", 200},
				{"login", password + "wrong", "UNAUTHENTICATED", 401},
				{"login", "", "INVALID_ARGUMENT", 400},
			} {
				body, err := json.Marshal(authRequest{Email: "utf8@example.test", Name: "UTF-8 User", Password: requestCase.password})
				if err != nil {
					t.Fatal(err)
				}
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/api/v1/open/auth/"+requestCase.action, bytes.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(recorder, request)
				var envelope authTestEnvelope
				if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if recorder.Code != http.StatusOK || envelope.Code != requestCase.code || envelope.Status != requestCase.status {
					t.Fatalf("%s returned HTTP %d, code %d, status %q", requestCase.action, recorder.Code, envelope.Code, envelope.Status)
				}
				if requestCase.code == 200 {
					var session struct {
						Token string `json:"token"`
						User  struct {
							ID string `json:"id"`
						} `json:"user"`
					}
					if err := json.Unmarshal(envelope.Detail, &session); err != nil {
						t.Fatal(err)
					}
					if session.User.ID == "" || session.Token == "" {
						t.Fatal("successful authentication must return a user and session")
					}
					if registeredID != "" && session.User.ID != registeredID {
						t.Fatal("login returned a different user")
					}
					registeredID = session.User.ID
				}
			}
		})
	}
}
