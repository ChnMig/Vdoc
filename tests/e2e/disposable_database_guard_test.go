package e2e

import "testing"

const disposableGuardAllowedDSN = "postgres://vdoc_user@127.0.0.1:5432/vdoc_e2e?sslmode=disable"

func TestDisposableDatabaseGuardAllowsDisposableDatabase(t *testing.T) {
	// Given
	t.Setenv("VDOC_POSTGRES_DB", "")
	t.Setenv("VDOC_DATABASE_DSN", "")

	// When
	err := assertDisposableTestDatabase(disposableGuardAllowedDSN)

	// Then
	if err != nil {
		t.Fatalf("expected disposable database DSN to be allowed: %v", err)
	}
}

func TestDisposableDatabaseConnectionGuardMatchesDefaultE2EDatabase(t *testing.T) {
	t.Setenv("VDOC_POSTGRES_DB", "vdoc")
	t.Setenv("VDOC_DATABASE_DSN", "")

	if err := validateDisposableTestDatabaseConnection(disposableGuardAllowedDSN, "vdoc_e2e"); err != nil {
		t.Fatalf("expected default E2E database connection to be allowed: %v", err)
	}
	if err := validateDisposableTestDatabaseConnection(disposableGuardAllowedDSN, "another_database"); err == nil {
		t.Fatal("expected connection to a database other than the guarded DSN to be rejected")
	}
}

func TestDisposableDatabaseGuardRejectsUnsafeDatabaseDSNs(t *testing.T) {
	tests := []struct {
		name           string
		testDSN        string
		appDatabaseDSN string
		appDatabase    string
	}{
		{name: "default application database", testDSN: "postgres://vdoc_user@127.0.0.1:5432/vdoc?sslmode=disable"},
		{name: "encoded application database", testDSN: "postgres://vdoc_user@127.0.0.1:5432/%76doc?sslmode=disable"},
		{name: "encoded slash in database name", testDSN: "postgres://vdoc_user@127.0.0.1:5432/vdoc%2Fe2e?sslmode=disable"},
		{name: "decoded slash in database name", testDSN: "postgres://vdoc_user@127.0.0.1:5432/vdoc/e2e?sslmode=disable"},
		{name: "extra path segment", testDSN: "postgres://vdoc_user@127.0.0.1:5432/vdoc/extra?sslmode=disable"},
		{name: "empty dsn", testDSN: ""},
		{name: "empty database name", testDSN: "postgres://vdoc_user@127.0.0.1:5432/?sslmode=disable"},
		{name: "malformed dsn", testDSN: "postgres://%zz"},
		{name: "unsupported dsn", testDSN: "host=127.0.0.1 dbname=vdoc_e2e"},
		{name: "matches parsed application dsn database", testDSN: "postgres://vdoc_user@127.0.0.1:5432/prod_db?sslmode=disable", appDatabaseDSN: "postgres://vdoc_user@127.0.0.1:5432/prod_db?sslmode=disable", appDatabase: "vdoc"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			t.Setenv("VDOC_POSTGRES_DB", test.appDatabase)
			t.Setenv("VDOC_DATABASE_DSN", test.appDatabaseDSN)

			// When
			err := assertDisposableTestDatabase(test.testDSN)

			// Then
			if err == nil {
				t.Fatal("expected unsafe database DSN to be rejected")
			}
		})
	}
}
