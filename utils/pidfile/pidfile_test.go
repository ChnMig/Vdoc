package pidfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type shortWritePIDFile struct{}

func (shortWritePIDFile) Write(content []byte) (int, error) { return len(content) - 1, nil }
func (shortWritePIDFile) Sync() error                       { return nil }
func (shortWritePIDFile) Close() error                      { return nil }

func TestWriteCreatesExclusivePrivatePIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "vdoc.pid")
	if err := Write(path, 1234); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "1234\n" {
		t.Fatalf("content = %q, want %q", content, "1234\\n")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	if err := Write(path, 5678); !errors.Is(err, ErrExists) {
		t.Fatalf("second Write() error = %v, want ErrExists", err)
	}
}

func TestRemoveVerifiesPIDOwnershipAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vdoc.pid")
	if err := Write(path, 1234); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := Remove(path, 5678); !errors.Is(err, ErrOwnership) {
		t.Fatalf("Remove() error = %v, want ErrOwnership", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("mismatched owner removed file: %v", err)
	}
	if err := Remove(path, 1234); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := Remove(path, 1234); err != nil {
		t.Fatalf("second Remove() error = %v", err)
	}
}

func TestPIDFileRejectsInvalidArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vdoc.pid")
	for _, tt := range []struct {
		name string
		err  error
		want error
	}{
		{name: "write empty path", err: Write("", 1), want: ErrInvalidPath},
		{name: "write invalid pid", err: Write(path, 0), want: ErrInvalidPID},
		{name: "remove empty path", err: Remove("", 1), want: ErrInvalidPath},
		{name: "remove invalid pid", err: Remove(path, 0), want: ErrInvalidPID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.want) {
				t.Fatalf("error = %v, want %v", tt.err, tt.want)
			}
		})
	}
}

func TestWriteRollsBackShortWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vdoc.pid")
	var removedPath string
	err := writeWithOperations(path, 1234, fileOperations{
		open: func(string, int, os.FileMode) (pidFile, error) {
			return shortWritePIDFile{}, nil
		},
		remove: func(path string) error {
			removedPath = path
			return nil
		},
	})
	if !errors.Is(err, ErrWrite) || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeWithOperations() error = %v, want ErrWrite and io.ErrShortWrite", err)
	}
	if removedPath != path {
		t.Fatalf("rollback path = %q, want %q", removedPath, path)
	}
}
