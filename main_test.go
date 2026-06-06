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

func TestResetAdminCLIRequiresTwoArguments(t *testing.T) {
	email, password, ok, err := parseResetAdminArgs([]string{"--resetadmin", "admin@example.com", "correct horse battery staple"})
	if err != nil || !ok {
		t.Fatalf("parseResetAdminArgs() ok=%v error=%v, want ok", ok, err)
	}
	if email != "admin@example.com" || password != "correct horse battery staple" {
		t.Fatalf("parseResetAdminArgs() = %q %q, want email and password", email, password)
	}
	if _, _, _, err := parseResetAdminArgs([]string{"--resetadmin", "admin@example.com"}); err == nil {
		t.Fatal("parseResetAdminArgs() error = nil, want usage error")
	}
	if _, _, ok, err := parseResetAdminArgs([]string{"--version"}); err != nil || ok {
		t.Fatalf("parseResetAdminArgs(non-reset) ok=%v error=%v, want no-op", ok, err)
	}

	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"parseResetAdminArgs(os.Args[1:])",
		"usage: vdoc --resetadmin <email> <password>",
		"ResetSuperAdminPassword",
		"ValidateInitialAdminPassword",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("main.go missing resetadmin marker %q", want)
		}
	}
}
