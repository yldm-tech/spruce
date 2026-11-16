package instance

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// shortDir returns a temporary folder with a path short enough for a socket on macOS, whose t.TempDir lies deep under /var/folders.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "si")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestOnlyOneCopyHoldsTheLock(t *testing.T) {
	dir := shortDir(t)
	first, ok, err := Acquire(dir)
	if err != nil || !ok {
		t.Fatalf("first Acquire = %v, %v", ok, err)
	}
	second, ok, err := Acquire(dir)
	if err != nil || ok || second != nil {
		t.Fatalf("second Acquire = %v, %v, %v; want another copy running", second, ok, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, ok, err := Acquire(dir)
	if err != nil || !ok {
		t.Fatalf("Acquire after Close = %v, %v", ok, err)
	}
	_ = third.Close()
}

func TestLaterCopyActivatesTheFirst(t *testing.T) {
	dir := shortDir(t)
	first, ok, err := Acquire(dir)
	if err != nil || !ok {
		t.Fatalf("Acquire = %v, %v", ok, err)
	}
	defer first.Close()
	activated := make(chan struct{}, 4)
	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- first.Serve(ctx, func() { activated <- struct{}{} }) }()

	for range 2 {
		if _, ok, err := Acquire(dir); ok || err != nil {
			t.Fatalf("later Acquire = %v, %v", ok, err)
		}
		select {
		case <-activated:
		case <-time.After(5 * time.Second):
			t.Fatal("the first copy was not activated")
		}
	}

	// A connection that sends something else does not activate it.
	conn, err := net.Dial("unix", filepath.Join(dir, socketFile))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("hello\n"))
	_ = conn.Close()

	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not stop with its context")
	}
	select {
	case <-activated:
		t.Error("a stray message activated the app")
	default:
	}
}

func TestStaleSocketIsReplaced(t *testing.T) {
	dir := shortDir(t)
	// A copy that crashed left its socket behind, with nobody listening.
	ln, err := net.Listen("unix", filepath.Join(dir, socketFile))
	if err != nil {
		t.Fatal(err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	_ = ln.Close()
	if _, err := os.Stat(filepath.Join(dir, socketFile)); err != nil {
		t.Fatalf("stale socket missing: %v", err)
	}

	first, ok, err := Acquire(dir)
	if err != nil || !ok {
		t.Fatalf("Acquire = %v, %v", ok, err)
	}
	defer first.Close()
	activated := make(chan struct{}, 1)
	go func() { _ = first.Serve(t.Context(), func() { activated <- struct{}{} }) }()
	if _, ok, _ := Acquire(dir); ok {
		t.Fatal("second copy took the lock")
	}
	select {
	case <-activated:
	case <-time.After(5 * time.Second):
		t.Fatal("the new socket did not take the activation")
	}
}

func TestLaterCopyGivesUpWhenNobodyListens(t *testing.T) {
	dir := shortDir(t)
	first, ok, err := Acquire(dir)
	if err != nil || !ok {
		t.Fatal(err)
	}
	defer first.Close()
	// The first copy holds the lock but its socket is gone (it could not listen).
	_ = first.ln.Close()
	_ = os.Remove(filepath.Join(dir, socketFile))
	start := time.Now()
	if _, ok, err := Acquire(dir); ok || err != nil {
		t.Fatalf("Acquire = %v, %v", ok, err)
	}
	if d := time.Since(start); d > dialFor+time.Second {
		t.Errorf("gave up after %v", d)
	}
}

func TestServeReportsAMissingSocket(t *testing.T) {
	dir := shortDir(t)
	long := dir
	for len(long) < maxSocketPath {
		long = filepath.Join(long, "deeper-folder-name")
	}
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	inst, ok, err := Acquire(long)
	if err != nil || !ok {
		t.Fatalf("Acquire = %v, %v", ok, err)
	}
	defer inst.Close()
	if err := inst.Serve(t.Context(), func() {}); err == nil {
		t.Error("Serve succeeded without a socket")
	}
}
