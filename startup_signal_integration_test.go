package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSIGTERMCancelsDependencyStartupAndRemovesPID(t *testing.T) {
	if testing.Short() || runtime.GOOS == "windows" {
		t.Skip("requires a real binary and SIGTERM")
	}
	bin := filepath.Join(t.TempDir(), "vdoc")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, phase := range []string{"postgres", "object_storage"} {
		t.Run(phase, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			endpoint := ""
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Skipf("cannot listen locally: %v", err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			if phase == "postgres" {
				endpoint = listener.Addr().String()
				go func() {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					defer conn.Close()
					var header [8]byte
					if _, err := io.ReadFull(conn, header[:]); err != nil {
						return
					}
					entered <- struct{}{}
					_, _ = io.Copy(io.Discard, conn)
				}()
			} else {
				server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() })}}
				server.Start()
				t.Cleanup(server.Close)
				endpoint = strings.TrimPrefix(server.URL, "http://")
			}
			workdir := t.TempDir()
			pid := filepath.Join(workdir, "vdoc.pid")
			outputPath := filepath.Join(workdir, "output.log")
			output, err := os.Create(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			cmd := exec.Command(bin, "--dev")
			cmd.Dir = workdir
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "VDOC_") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "VDOC_SERVER_HOST=127.0.0.1", "VDOC_SERVER_PORT="+reserveLocalPort(t), "VDOC_SERVER_PID_FILE="+pid, "VDOC_JWT_KEY=0123456789abcdef0123456789abcdef", "VDOC_AUTH_ALLOW_REGISTRATION=true")
			if phase == "postgres" {
				cmd.Env = append(cmd.Env, "VDOC_DATABASE_ENABLED=true", "VDOC_DATABASE_DSN=postgres://synthetic:synthetic@"+endpoint+"/synthetic?sslmode=disable", "VDOC_STORAGE_ENABLED=false")
			} else {
				cmd.Env = append(cmd.Env, "VDOC_DATABASE_ENABLED=false", "VDOC_STORAGE_ENABLED=true", "VDOC_STORAGE_ENDPOINT="+endpoint, "VDOC_STORAGE_BUCKET=synthetic", "VDOC_STORAGE_ACCESS_KEY=synthetic", "VDOC_STORAGE_SECRET_KEY=synthetic-password", "VDOC_STORAGE_USE_SSL=false")
			}
			cmd.Stdout, cmd.Stderr = output, output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			finished := false
			t.Cleanup(func() {
				if !finished {
					_ = cmd.Process.Kill()
					<-wait
				}
			})
			select {
			case <-entered:
			case err := <-wait:
				finished = true
				t.Fatalf("exited before dependency handshake: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("dependency handshake did not start")
			}
			if _, err := os.Stat(pid); err != nil {
				t.Fatalf("PID ownership was not obtained before initialization: %v", err)
			}
			started := time.Now()
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			select {
			case <-wait:
				finished = true
			case <-time.After(2 * time.Second):
				t.Fatal("SIGTERM did not interrupt stalled startup")
			}
			out, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if cmd.ProcessState.ExitCode() != 1 {
				t.Fatalf("startup exit was not controlled failure: %s\n%s", cmd.ProcessState, out)
			}
			if _, err := os.Stat(pid); !os.IsNotExist(err) {
				t.Fatalf("cancelled startup left a PID file: %v\n%s", err, out)
			}
			if strings.Contains(string(out), "Starting HTTP service") {
				t.Fatalf("cancelled startup exposed HTTP service: %s", out)
			}
			t.Logf("%s cancelled in %s and removed PID", phase, time.Since(started))
		})
	}
}
