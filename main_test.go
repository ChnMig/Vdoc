package main

import (
	"os"
	"strings"
	"testing"
)

func TestResetAdminCLIIsHandledBeforeHTTPStartup(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	resetIndex := strings.Index(text, "runResetAdmin(context.Background()")
	serverIndex := strings.Index(text, "api.InitApi()")
	if resetIndex < 0 {
		t.Fatal("main.go missing resetadmin execution")
	}
	if serverIndex < 0 {
		t.Fatal("main.go missing HTTP startup marker")
	}
	if resetIndex > serverIndex {
		t.Fatal("resetadmin must run before HTTP startup")
	}
}

func TestResetAdminCLIReadsPasswordFromStdin(t *testing.T) {
	email, ok, err := parseResetAdminArgs([]string{"--resetadmin", "admin@example.com"})
	if err != nil || !ok {
		t.Fatalf("parseResetAdminArgs() ok=%v error=%v, want ok", ok, err)
	}
	if email != "admin@example.com" {
		t.Fatalf("parseResetAdminArgs() = %q, want email", email)
	}
	if _, _, err := parseResetAdminArgs([]string{"--resetadmin", "admin@example.com", "password-must-not-be-in-argv"}); err == nil {
		t.Fatal("parseResetAdminArgs() accepted password in argv")
	}
	if _, ok, err := parseResetAdminArgs([]string{"--version"}); err != nil || ok {
		t.Fatalf("parseResetAdminArgs(non-reset) ok=%v error=%v, want no-op", ok, err)
	}
	password, err := readResetAdminPassword(strings.NewReader("correct horse battery staple\n"))
	if err != nil || password != "correct horse battery staple" {
		t.Fatalf("readResetAdminPassword() = %q, %v", password, err)
	}
	if _, err := readResetAdminPassword(strings.NewReader("\n")); err == nil {
		t.Fatal("readResetAdminPassword() accepted empty input")
	}
	if _, err := readResetAdminPassword(strings.NewReader(strings.Repeat("x", resetAdminPasswordMaxBytes+1))); err == nil {
		t.Fatal("readResetAdminPassword() accepted oversized input")
	}

	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"parseResetAdminArgs(os.Args[1:])",
		"readResetAdminPassword(os.Stdin)",
		"ResetSuperAdminPassword",
		"ValidateInitialAdminPassword",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("main.go missing resetadmin marker %q", want)
		}
	}
	if strings.Contains(text, "--resetadmin <email> <password>") {
		t.Fatal("main.go still documents password in argv")
	}
}

func TestDockerHealthcheckRequiresSemanticHealth(t *testing.T) {
	source, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	text := string(source)
	if !strings.Contains(text, `grep -q '"healthy":true'`) {
		t.Fatalf("Docker healthcheck does not verify semantic healthy=true: %s", text)
	}
	if strings.Contains(text, "-O /dev/null") {
		t.Fatal("Docker healthcheck still discards the health response body")
	}
}
