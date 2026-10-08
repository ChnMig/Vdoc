package pgdb

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
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

func TestOpenWithConfigInitialConnectionHonorsDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen locally: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	entered := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		t.Cleanup(func() { _ = conn.Close() })
		defer conn.Close()
		var header [8]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return
		}
		close(entered)
		_, _ = io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := OpenWithConfig(ctx, Config{DSN: "postgres://synthetic:synthetic@" + listener.Addr().String() + "/synthetic?sslmode=disable", MaxOpenConn: 1, MaxIdleConn: 0})
		result <- err
	}()
	select {
	case err = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("initial connection ignored its context deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("initial stalled connection did not return context deadline: %v", err)
	}
	select {
	case <-entered:
	default:
		t.Fatal("probe did not reach PostgreSQL startup handshake")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled connection remained open")
	}
}
