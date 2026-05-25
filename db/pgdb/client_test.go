package pgdb

import (
	"context"
	"strings"
	"testing"
)

func TestOpenWithConfigRequiresDSN(t *testing.T) {
	_, err := OpenWithConfig(context.Background(), Config{RunMigration: true})
	if err == nil {
		t.Fatal("OpenWithConfig() error = nil, want DSN error")
	}
	if !strings.Contains(err.Error(), "database.dsn") {
		t.Fatalf("OpenWithConfig() error = %v, want database.dsn", err)
	}
}
