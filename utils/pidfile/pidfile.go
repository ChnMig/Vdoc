// Package pidfile 安全地管理当前进程拥有的 PID 文件。
package pidfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	ErrInvalidPath = errors.New("pidfile: invalid path")
	ErrInvalidPID  = errors.New("pidfile: invalid pid")
	ErrExists      = errors.New("pidfile: already exists")
	ErrWrite       = errors.New("pidfile: write")
	ErrRemove      = errors.New("pidfile: remove")
	ErrOwnership   = errors.New("pidfile: ownership mismatch")
)

// OperationError 保留失败操作与目标路径，同时支持 errors.Is 判断错误类别。
type OperationError struct {
	Err  error
	Op   string
	Path string
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("pidfile %s %s: %v", e.Op, e.Path, e.Err)
}

func (e *OperationError) Unwrap() error { return e.Err }

// Write 以独占方式创建 PID 文件。已有文件不会被覆盖，避免多个实例互相夺取所有权。
func Write(path string, pid int) error {
	return writeWithOperations(path, pid, fileOperations{open: openPIDFile, remove: os.Remove})
}

type pidFile interface {
	io.Writer
	Sync() error
	Close() error
}

type fileOperations struct {
	open   func(string, int, os.FileMode) (pidFile, error)
	remove func(string) error
}

func openPIDFile(path string, flag int, mode os.FileMode) (pidFile, error) {
	return os.OpenFile(path, flag, mode)
}

func writeWithOperations(path string, pid int, operations fileOperations) (err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return &OperationError{Op: "validate", Path: path, Err: ErrInvalidPath}
	}
	if pid <= 0 {
		return &OperationError{Op: "validate", Path: path, Err: ErrInvalidPID}
	}
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o750); mkdirErr != nil {
		return &OperationError{Op: "mkdir", Path: path, Err: errors.Join(ErrWrite, mkdirErr)}
	}

	file, openErr := operations.open(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if openErr != nil {
		category := ErrWrite
		if errors.Is(openErr, os.ErrExist) {
			category = ErrExists
		}
		return &OperationError{Op: "create", Path: path, Err: errors.Join(category, openErr)}
	}
	complete := false
	defer func() {
		closeErr := file.Close()
		if closeErr != nil {
			complete = false
		}
		if !complete {
			removeErr := operations.remove(path)
			if errors.Is(removeErr, os.ErrNotExist) {
				removeErr = nil
			}
			err = errors.Join(
				err,
				wrapFilesystem("close", path, closeErr, ErrWrite),
				wrapFilesystem("rollback", path, removeErr, ErrRemove),
			)
			return
		}
		err = errors.Join(err, wrapFilesystem("close", path, closeErr, ErrWrite))
	}()

	content := []byte(strconv.Itoa(pid) + "\n")
	written, writeErr := file.Write(content)
	if writeErr != nil {
		return &OperationError{Op: "write", Path: path, Err: errors.Join(ErrWrite, writeErr)}
	}
	if written != len(content) {
		return &OperationError{Op: "write", Path: path, Err: errors.Join(ErrWrite, io.ErrShortWrite)}
	}
	if syncErr := file.Sync(); syncErr != nil {
		return &OperationError{Op: "sync", Path: path, Err: errors.Join(ErrWrite, syncErr)}
	}
	complete = true
	return nil
}

// Remove 仅在文件内容仍属于 pid 时删除；文件不存在视为已完成。
func Remove(path string, pid int) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return &OperationError{Op: "validate", Path: path, Err: ErrInvalidPath}
	}
	if pid <= 0 {
		return &OperationError{Op: "validate", Path: path, Err: ErrInvalidPID}
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return nil
		}
		return &OperationError{Op: "read", Path: path, Err: errors.Join(ErrRemove, readErr)}
	}
	if string(content) != strconv.Itoa(pid)+"\n" {
		return &OperationError{Op: "verify", Path: path, Err: ErrOwnership}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &OperationError{Op: "remove", Path: path, Err: errors.Join(ErrRemove, err)}
	}
	return nil
}

func wrapFilesystem(operation, path string, err, category error) error {
	if err == nil {
		return nil
	}
	return &OperationError{Op: operation, Path: path, Err: errors.Join(category, err)}
}
