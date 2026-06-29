package e2e

import "testing"

type e2eScriptInvocation struct {
	args []string
	env  []string
}

type e2eScriptResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func TestVdocE2EScriptCLI_help_documents_modes(t *testing.T) {
	// Given
	invocation := e2eScriptInvocation{args: []string{"help"}}

	// When
	result := runVdocE2EScript(t, invocation)

	// Then
	requireScriptExitCode(t, result, 0)
	for _, want := range []string{"happy", "failure", "all", "live", "live-check", "live-compose", "--env-file", "--check-only"} {
		requireContainsText(t, result.stdout, want)
	}
	if result.stderr != "" {
		t.Fatalf("help stderr = %q, want empty", result.stderr)
	}
}

func TestVdocE2EScriptCLI_unknown_command_exits_2(t *testing.T) {
	// Given
	invocation := e2eScriptInvocation{args: []string{"unknown-mode"}}

	// When
	result := runVdocE2EScript(t, invocation)

	// Then
	requireScriptExitCode(t, result, 2)
	requireContainsText(t, result.stderr, "unknown mode: unknown-mode")
	requireContainsText(t, result.stderr, "usage:")
	requireContainsText(t, result.stderr, "live-compose")
	if result.stdout != "" {
		t.Fatalf("unknown command stdout = %q, want empty", result.stdout)
	}
}

func TestVdocE2EScriptCLI_live_check_reports_missing_env_without_go_tests(t *testing.T) {
	// Given
	invocation := e2eScriptInvocation{args: []string{"live-check"}}

	// When
	result := runVdocE2EScript(t, invocation)

	// Then
	requireScriptExitCode(t, result, 2)
	if result.stderr != missingLiveEnvMessage() {
		t.Fatalf("live-check stderr = %q, want %q", result.stderr, missingLiveEnvMessage())
	}
	requireOutputOmitsText(t, result, "go test", "TestVdocV01EndToEndLivePersistence")
}

func TestVdocE2EScriptCLI_live_check_refuses_application_database(t *testing.T) {
	// Given
	databaseDSN := "postgres://vdoc_user@127.0.0.1:5432/vdoc?sslmode=disable"
	invocation := liveCheckInvocation(databaseDSN)

	// When
	result := runVdocE2EScript(t, invocation)

	// Then
	requireScriptExitCode(t, result, 2)
	requireContainsText(t, result.stderr, "refusing to run live E2E against application database")
	requireOutputOmitsText(t, result,
		databaseDSN,
		"__fixture_storage_access_key__",
		"__fixture_storage_secret_key__",
		"go test",
		"TestVdocV01EndToEndLivePersistence",
	)
}

func TestVdocE2EScriptCLI_live_check_refuses_undecidable_database_name(t *testing.T) {
	tests := []struct {
		name        string
		databaseDSN string
	}{
		{name: "percent encoded app database", databaseDSN: "postgres://vdoc_user@127.0.0.1:5432/%76doc?sslmode=disable"},
		{name: "percent encoded path separator", databaseDSN: "postgres://vdoc_user@127.0.0.1:5432/vdoc%2Fe2e?sslmode=disable"},
		{name: "extra path segment", databaseDSN: "postgres://vdoc_user@127.0.0.1:5432/vdoc/extra?sslmode=disable"},
		{name: "empty database path", databaseDSN: "postgres://vdoc_user@127.0.0.1:5432/?sslmode=disable"},
		{name: "unsupported DSN form", databaseDSN: "host=127.0.0.1 dbname=vdoc user=vdoc_user"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			invocation := liveCheckInvocation(test.databaseDSN)

			// When
			result := runVdocE2EScript(t, invocation)

			// Then
			requireScriptExitCode(t, result, 2)
			requireContainsText(t, result.stderr, "database name could not be determined")
			requireOutputOmitsText(t, result,
				test.databaseDSN,
				"__fixture_storage_access_key__",
				"__fixture_storage_secret_key__",
				"go test",
				"TestVdocV01EndToEndLivePersistence",
			)
		})
	}
}

func TestVdocE2EScriptCLI_live_compose_check_only_derives_env(t *testing.T) {
	// Given
	envPath := writeComposeEnv(t, []string{
		"VDOC_POSTGRES_HOST_PORT=55432",
		"VDOC_POSTGRES_DB=vdoc",
		"VDOC_POSTGRES_USER=vdoc_user",
		"VDOC_POSTGRES_PASSWORD=postgres-secret-value",
		"VDOC_RUSTFS_HOST_PORT=19000",
		"VDOC_STORAGE_BUCKET=vdoc-compose-test",
		"VDOC_STORAGE_ACCESS_KEY=storage-access-value",
		"VDOC_STORAGE_SECRET_KEY=storage-secret-value",
	})
	invocation := e2eScriptInvocation{args: []string{"live-compose", "--env-file", envPath, "--check-only"}}

	// When
	result := runVdocE2EScript(t, invocation)

	// Then
	requireScriptExitCode(t, result, 0)
	requireStdoutContainsText(t, result,
		"live-compose check OK",
		"VDOC_TEST_POSTGRES_DB=vdoc_e2e",
		"VDOC_TEST_DATABASE_DSN",
		"VDOC_TEST_STORAGE_ENDPOINT",
		"VDOC_TEST_STORAGE_BUCKET",
		"VDOC_TEST_STORAGE_ACCESS_KEY",
		"VDOC_TEST_STORAGE_SECRET_KEY",
	)
	requireOutputOmitsText(t, result, "VDOC_TEST_POSTGRES_DB=vdoc\n", "postgres-secret-value", "storage-access-value", "storage-secret-value", "go test")
}

func TestVdocE2EScriptCLI_live_compose_check_only_accepts_env_example(t *testing.T) {
	// Given
	envPath := writeComposeEnv(t, []string{
		"VDOC_POSTGRES_HOST_PORT=5432",
		"VDOC_POSTGRES_DB=vdoc",
		"VDOC_POSTGRES_USER=vdoc",
		"VDOC_POSTGRES_PASSWORD=replace-with-local-postgres-password",
		"VDOC_RUSTFS_HOST_PORT=9000",
		"VDOC_STORAGE_BUCKET=vdoc",
		"VDOC_STORAGE_ACCESS_KEY=replace-with-local-rustfs-access-key",
		"VDOC_STORAGE_SECRET_KEY=replace-with-local-rustfs-secret-key",
		"VDOC_TEST_POSTGRES_DB=vdoc_e2e",
		"VDOC_TEST_STORAGE_USE_SSL=false",
		"VDOC_TEST_STORAGE_PATH_STYLE=true",
	})
	invocation := e2eScriptInvocation{args: []string{"live-compose", "--env-file", envPath, "--check-only"}}

	// When
	result := runVdocE2EScript(t, invocation)

	// Then
	requireScriptExitCode(t, result, 0)
	requireStdoutContainsText(t, result, "live-compose check OK", "VDOC_TEST_POSTGRES_DB=vdoc_e2e", "VDOC_TEST_DATABASE_DSN")
	requireOutputOmitsText(t, result, "replace-with-local-postgres-password", "replace-with-local-rustfs-access-key", "replace-with-local-rustfs-secret-key", "go test")
}

func TestVdocE2EScriptCLI_live_compose_check_only_ignores_unlisted_env_keys(t *testing.T) {
	// Given
	envPath := writeComposeEnv(t, []string{
		"PATH=/tmp/ignored-path-value",
		"GOFLAGS=-run=IgnoredByAllowlist",
		"BASH_ENV=/tmp/ignored-bash-env",
		"VDOC_TEST_DATABASE_DSN=postgres://vdoc_user:__fixture_postgres_password__@127.0.0.1:55432/vdoc?sslmode=disable",
		"VDOC_POSTGRES_HOST_PORT=55432",
		"VDOC_POSTGRES_DB=vdoc",
		"VDOC_POSTGRES_USER=vdoc_user",
		"VDOC_POSTGRES_PASSWORD=__fixture_postgres_password__",
		"VDOC_RUSTFS_HOST_PORT=19000",
		"VDOC_STORAGE_BUCKET=vdoc-compose-test",
		"VDOC_STORAGE_ACCESS_KEY=__fixture_storage_access_key__",
		"VDOC_STORAGE_SECRET_KEY=__fixture_storage_secret_key__",
	})
	invocation := e2eScriptInvocation{args: []string{"live-compose", "--env-file", envPath, "--check-only"}}

	// When
	result := runVdocE2EScript(t, invocation)

	// Then
	requireScriptExitCode(t, result, 0)
	requireStdoutContainsText(t, result, "live-compose check OK", "VDOC_TEST_DATABASE_DSN", "VDOC_TEST_POSTGRES_DB=vdoc_e2e")
	requireOutputOmitsText(t, result,
		"ignored-path-value",
		"IgnoredByAllowlist",
		"ignored-bash-env",
		"__fixture_postgres_password__",
		"__fixture_storage_access_key__",
		"__fixture_storage_secret_key__",
		"go test",
	)
}

func TestVdocE2EScriptCLI_live_compose_check_only_refuses_application_database(t *testing.T) {
	// Given
	databaseDSN := "postgres://vdoc_user:__fixture_postgres_password__@127.0.0.1:55432/vdoc?sslmode=disable"
	envPath := writeComposeEnv(t, []string{
		"VDOC_POSTGRES_HOST_PORT=55432",
		"VDOC_POSTGRES_DB=vdoc",
		"VDOC_TEST_POSTGRES_DB=vdoc",
		"VDOC_POSTGRES_USER=vdoc_user",
		"VDOC_POSTGRES_PASSWORD=__fixture_postgres_password__",
		"VDOC_RUSTFS_HOST_PORT=19000",
		"VDOC_STORAGE_BUCKET=vdoc-compose-test",
		"VDOC_STORAGE_ACCESS_KEY=__fixture_storage_access_key__",
		"VDOC_STORAGE_SECRET_KEY=__fixture_storage_secret_key__",
	})
	invocation := e2eScriptInvocation{args: []string{"live-compose", "--env-file", envPath, "--check-only"}}

	// When
	result := runVdocE2EScript(t, invocation)

	// Then
	requireScriptExitCode(t, result, 2)
	requireContainsText(t, result.stderr, "refusing to run live E2E against application database")
	requireOutputOmitsText(t, result,
		databaseDSN,
		"__fixture_postgres_password__",
		"__fixture_storage_access_key__",
		"__fixture_storage_secret_key__",
		"go test",
		"TestVdocV01EndToEndLivePersistence",
	)
}

func TestVdocE2EScriptCLI_live_compose_check_only_refuses_undecidable_test_database(t *testing.T) {
	tests := []struct {
		name         string
		testDatabase string
		databaseDSN  string
	}{
		{
			name:         "encoded app database",
			testDatabase: "%76doc",
			databaseDSN:  "postgres://vdoc_user:__fixture_postgres_password__@127.0.0.1:55432/%76doc?sslmode=disable",
		},
		{
			name:         "segmented database path",
			testDatabase: "vdoc/extra",
			databaseDSN:  "postgres://vdoc_user:__fixture_postgres_password__@127.0.0.1:55432/vdoc/extra?sslmode=disable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			envPath := writeComposeEnv(t, []string{
				"VDOC_POSTGRES_HOST_PORT=55432",
				"VDOC_POSTGRES_DB=vdoc",
				"VDOC_TEST_POSTGRES_DB=" + test.testDatabase,
				"VDOC_POSTGRES_USER=vdoc_user",
				"VDOC_POSTGRES_PASSWORD=__fixture_postgres_password__",
				"VDOC_RUSTFS_HOST_PORT=19000",
				"VDOC_STORAGE_BUCKET=vdoc-compose-test",
				"VDOC_STORAGE_ACCESS_KEY=__fixture_storage_access_key__",
				"VDOC_STORAGE_SECRET_KEY=__fixture_storage_secret_key__",
			})
			invocation := e2eScriptInvocation{args: []string{"live-compose", "--env-file", envPath, "--check-only"}}

			// When
			result := runVdocE2EScript(t, invocation)

			// Then
			requireScriptExitCode(t, result, 2)
			requireContainsText(t, result.stderr, "database name could not be determined")
			requireOutputOmitsText(t, result,
				"live-compose check OK",
				test.databaseDSN,
				"__fixture_postgres_password__",
				"__fixture_storage_access_key__",
				"__fixture_storage_secret_key__",
				"go test",
				"TestVdocV01EndToEndLivePersistence",
			)
		})
	}
}

func TestVdocE2EScriptCLI_live_compose_check_only_uses_explicit_test_db(t *testing.T) {
	// Given
	envPath := writeComposeEnv(t, []string{
		"VDOC_POSTGRES_DB=vdoc",
		"VDOC_TEST_POSTGRES_DB=vdoc_ci_e2e",
		"VDOC_POSTGRES_PASSWORD=postgres-secret-value",
		"VDOC_STORAGE_ACCESS_KEY=storage-access-value",
		"VDOC_STORAGE_SECRET_KEY=storage-secret-value",
	})
	invocation := e2eScriptInvocation{args: []string{"live-compose", "--env-file", envPath, "--check-only"}}

	// When
	result := runVdocE2EScript(t, invocation)

	// Then
	requireScriptExitCode(t, result, 0)
	requireContainsText(t, result.stdout, "VDOC_TEST_POSTGRES_DB=vdoc_ci_e2e")
	requireOutputOmitsText(t, result, "postgres-secret-value", "storage-access-value", "storage-secret-value", "go test")
}
