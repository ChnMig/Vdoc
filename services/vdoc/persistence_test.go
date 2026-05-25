package vdoc

import (
	"os"
	"strings"
	"testing"
)

func TestPostgresPersistenceSourceDoesNotUsePrototypeStateTable(t *testing.T) {
	source, err := os.ReadFile("persistence.go")
	if err != nil {
		t.Fatalf("read persistence.go: %v", err)
	}
	if strings.Contains(strings.ToLower(string(source)), "vdoc_state") {
		t.Fatal("production PostgreSQL persistence must not depend on vdoc_state JSONB")
	}
}
