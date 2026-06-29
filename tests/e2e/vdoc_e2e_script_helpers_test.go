package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func liveCheckInvocation(databaseDSN string) e2eScriptInvocation {
	return e2eScriptInvocation{
		args: []string{"live-check"},
		env: []string{
			"VDOC_TEST_DATABASE_DSN=" + databaseDSN,
			"VDOC_TEST_STORAGE_ENDPOINT=127.0.0.1:19000",
			"VDOC_TEST_STORAGE_BUCKET=vdoc-live-test",
			"VDOC_TEST_STORAGE_ACCESS_KEY=__fixture_storage_access_key__",
			"VDOC_TEST_STORAGE_SECRET_KEY=__fixture_storage_secret_key__",
		},
	}
}

func writeComposeEnv(t *testing.T, lines []string) string {
	t.Helper()
	envPath := filepath.Join(t.TempDir(), "compose.env")
	composeEnv := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(envPath, []byte(composeEnv), 0o600); err != nil {
		t.Fatalf("write compose env fixture: %v", err)
	}
	return envPath
}

func runVdocE2EScript(t *testing.T, invocation e2eScriptInvocation) e2eScriptResult {
	t.Helper()
	root := repoRoot(t)
	command := exec.Command(filepath.Join(root, "scripts", "vdoc-e2e.sh"), invocation.args...)
	command.Dir = root
	command.Env = append(baseScriptEnv(), invocation.env...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run vdoc-e2e.sh %v: %v", invocation.args, err)
		}
	}
	return e2eScriptResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

func baseScriptEnv() []string {
	keys := []string{"PATH", "HOME", "TMPDIR"}
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		value := os.Getenv(key)
		if value != "" {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
	}
	return env
}

func requireScriptExitCode(t *testing.T, result e2eScriptResult, want int) {
	t.Helper()
	if result.exitCode != want {
		t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", result.exitCode, want, result.stdout, result.stderr)
	}
}

func requireContainsText(t *testing.T, got string, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output %q does not contain %q", got, want)
	}
}

func requireStdoutContainsText(t *testing.T, result e2eScriptResult, wants ...string) {
	t.Helper()
	for _, want := range wants {
		requireContainsText(t, result.stdout, want)
	}
}

func requireOmitsText(t *testing.T, got string, forbidden string) {
	t.Helper()
	if strings.Contains(got, forbidden) {
		t.Fatalf("output %q contains forbidden text %q", got, forbidden)
	}
}

func requireOutputOmitsText(t *testing.T, result e2eScriptResult, forbiddenTexts ...string) {
	t.Helper()
	for _, forbidden := range forbiddenTexts {
		requireOmitsText(t, result.stdout, forbidden)
		requireOmitsText(t, result.stderr, forbidden)
	}
}

func missingLiveEnvMessage() string {
	return strings.Join([]string{
		"missing required live E2E environment variables:",
		"  VDOC_TEST_DATABASE_DSN",
		"  VDOC_TEST_STORAGE_ENDPOINT",
		"  VDOC_TEST_STORAGE_BUCKET",
		"  VDOC_TEST_STORAGE_ACCESS_KEY",
		"  VDOC_TEST_STORAGE_SECRET_KEY",
		"See ../PILOT_RUNBOOK.md or tests/e2e/README.md for setup.",
	}, "\n") + "\n"
}
