package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCheckDoesNotStartServicesOrWriteFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过真实二进制检查")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "vdoc")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, valid := range []bool{true, false} {
		workdir := t.TempDir()
		command := exec.Command(binary, "--check-config")
		command.Dir = workdir
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "VDOC_") {
				command.Env = append(command.Env, entry)
			}
		}
		key := "0123456789abcdef0123456789abcdef"
		if !valid {
			key = "CHANGE_ME_JWT_KEY"
		}
		command.Env = append(command.Env, "VDOC_JWT_KEY="+key, "VDOC_DATABASE_ENABLED=true", "VDOC_DATABASE_HOST=127.0.0.1", "VDOC_DATABASE_PORT=1", "VDOC_DATABASE_PASSWORD=unit-test-password", "VDOC_STORAGE_ENABLED=false")
		out, err := command.CombinedOutput()
		if valid && (err != nil || !strings.Contains(string(out), "Configuration valid")) {
			t.Fatalf("valid offline check: %v\n%s", err, out)
		}
		if !valid && err == nil {
			t.Fatal("placeholder was accepted")
		}
		if strings.Contains(string(out), key) {
			t.Fatal("configuration check leaked key")
		}
		files, err := os.ReadDir(workdir)
		if err != nil || len(files) != 0 {
			t.Fatal("configuration check created runtime files")
		}
	}
	for _, key := range []string{"VDOC_SERVER_ENABLE_RATE_LIMIT", "VDOC_SERVER_ENABLE_CORS", "VDOC_AUTH_ALLOW_REGISTRATION", "VDOC_DATABASE_ENABLED", "VDOC_STORAGE_ENABLED", "VDOC_STORAGE_USE_SSL", "VDOC_STORAGE_PATH_STYLE"} {
		t.Run(key, func(t *testing.T) {
			command := exec.Command(binary, "--check-config")
			command.Dir = t.TempDir()
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "VDOC_") {
					command.Env = append(command.Env, entry)
				}
			}
			command.Env = append(command.Env, "VDOC_JWT_KEY=0123456789abcdef0123456789abcdef", key+"=treu")
			out, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("invalid boolean passed config check: %s", out)
			}
			if !strings.Contains(string(out), "must be a boolean") || strings.Contains(string(out), "treu") {
				t.Fatalf("boolean diagnostic is missing or leaks input: %s", out)
			}
			files, err := os.ReadDir(command.Dir)
			if err != nil || len(files) != 0 {
				t.Fatal("rejected config wrote runtime files")
			}
		})
	}
}
