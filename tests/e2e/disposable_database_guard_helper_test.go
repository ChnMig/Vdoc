package e2e

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const fallbackApplicationDatabase = "vdoc"

var errUnsafeTestDatabase = errors.New("unsafe test database")

func assertDisposableTestDatabase(testDSN string) error {
	testDatabase, err := postgresURLDatabaseName(testDSN)
	if err != nil {
		return fmt.Errorf("parse VDOC_TEST_DATABASE_DSN: %w", err)
	}

	applicationDatabase := strings.TrimSpace(os.Getenv("VDOC_POSTGRES_DB"))
	if applicationDatabase == "" {
		applicationDatabase = fallbackApplicationDatabase
	}
	if testDatabase == applicationDatabase {
		return fmt.Errorf("test database %q matches VDOC_POSTGRES_DB: %w", testDatabase, errUnsafeTestDatabase)
	}

	applicationDSN := strings.TrimSpace(os.Getenv("VDOC_DATABASE_DSN"))
	if applicationDSN == "" {
		return nil
	}
	applicationDSNDatabase, err := postgresURLDatabaseName(applicationDSN)
	if err != nil {
		return nil
	}
	if testDatabase == applicationDSNDatabase {
		return fmt.Errorf("test database %q matches VDOC_DATABASE_DSN database: %w", testDatabase, errUnsafeTestDatabase)
	}
	return nil
}

func postgresURLDatabaseName(rawDSN string) (string, error) {
	dsn := strings.TrimSpace(rawDSN)
	if dsn == "" {
		return "", fmt.Errorf("empty PostgreSQL URL DSN: %w", errUnsafeTestDatabase)
	}
	parsedURL, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse PostgreSQL URL DSN: %w", err)
	}
	if parsedURL.Scheme != "postgres" && parsedURL.Scheme != "postgresql" {
		return "", fmt.Errorf("unsupported PostgreSQL URL scheme %q: %w", parsedURL.Scheme, errUnsafeTestDatabase)
	}
	if parsedURL.Opaque != "" {
		return "", fmt.Errorf("opaque PostgreSQL URL DSN: %w", errUnsafeTestDatabase)
	}

	escapedPath := parsedURL.EscapedPath()
	if escapedPath == "" || escapedPath == "/" {
		return "", fmt.Errorf("empty PostgreSQL database name: %w", errUnsafeTestDatabase)
	}
	if !strings.HasPrefix(escapedPath, "/") {
		return "", fmt.Errorf("undecidable PostgreSQL database path %q: %w", escapedPath, errUnsafeTestDatabase)
	}
	if strings.Contains(strings.ToLower(escapedPath), "%2f") {
		return "", fmt.Errorf("PostgreSQL database name contains encoded slash: %w", errUnsafeTestDatabase)
	}

	escapedDatabase := strings.TrimPrefix(escapedPath, "/")
	if strings.Contains(escapedDatabase, "/") {
		return "", fmt.Errorf("PostgreSQL database URL has extra path segments: %w", errUnsafeTestDatabase)
	}
	databaseName, err := url.PathUnescape(escapedDatabase)
	if err != nil {
		return "", fmt.Errorf("decode PostgreSQL database name: %w", err)
	}
	if databaseName == "" || strings.Contains(databaseName, "/") {
		return "", fmt.Errorf("unsafe PostgreSQL database name %q: %w", databaseName, errUnsafeTestDatabase)
	}
	return databaseName, nil
}
