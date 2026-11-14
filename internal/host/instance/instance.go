// Package instance keeps Spruce to one running copy per user. The first copy holds a file lock on instance.lock in the data folder and listens on the Unix socket instance.sock beside it; a later copy fails to take the lock, writes "activate" to the socket so the first comes to the front, and exits. Go's net package speaks Unix sockets on macOS and on Windows 10 1803 and later.
package instance

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The files in the data folder.
const (
	lockFile   = "instance.lock"
	socketFile = "instance.sock"
)

// activate is the line a later copy sends.
const activate = "activate"

// The timing of a later copy: how long it keeps trying to reach the first, which may not listen yet, and how long one exchange may take.
const (
	dialFor      = 2 * time.Second
	dialEvery    = 50 * time.Millisecond
	exchangeTime = 2 * time.Second
)

// maxSocketPath is the longest socket path the platforms take (sockaddr_un's sun_path less its terminating zero on macOS; Windows takes 108).
const maxSocketPath = 103

// Instance is the running copy's hold on the lock and its socket.
type Instance struct {
	lock      *os.File
	ln        net.Listener
	listenErr error
	sock      string
	closeOnce sync.Once
}

// Acquire takes the lock in dir. first is true when this copy is the first: it must Serve and, as it ends, Close. When another copy holds the lock, Acquire tells it to come to the front and returns first false; the caller exits. An error means the lock could not be taken or tested at all; the caller may run without the single-instance guarantee.
func Acquire(dir string) (inst *Instance, first bool, err error) {
	f, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("instance: %w", err)
	}
	held, err := tryLock(f)
	if err != nil {
		_ = f.Close()
		return nil, false, fmt.Errorf("instance: lock %s: %w", f.Name(), err)
	}
	sock := filepath.Join(dir, socketFile)
	if !held {
		_ = f.Close()
		allowForeground()
		// The first copy may have died between its lock and its socket, or not listen yet; either way this copy has done what it can.
		_ = notify(sock)
		return nil, false, nil
	}
	inst = &Instance{lock: f, sock: sock}
	inst.ln, inst.listenErr = listen(sock)
	return inst, true, nil
}

// listen listens on sock, removing a socket a copy that crashed left behind: only the holder of the lock gets here, so no live copy listens on it.
func listen(sock string) (net.Listener, error) {
	if len(sock) > maxSocketPath {
		return nil, fmt.Errorf("instance: socket path is %d bytes, more than %d: %s", len(sock), maxSocketPath, sock)
	}
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("instance: remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("instance: listen: %w", err)
	}
	return ln, nil
}

// notify tells the copy listening on sock to come to the front, trying for up to dialFor while it starts.
func notify(sock string) error {
	deadline := time.Now().Add(dialFor)
	for {
		conn, err := net.DialTimeout("unix", sock, exchangeTime)
		if err == nil {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(exchangeTime))
			_, err = fmt.Fprintln(conn, activate)
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("instance: reach the running copy: %w", err)
		}
		time.Sleep(dialEvery)
	}
}

// Serve calls onActivate, on its own goroutine, each time a later copy asks this one to come to the front, until ctx ends or Close is called. It returns at once with the reason when the socket could not be opened, which leaves the lock in place: later copies still exit, only without bringing this one forward.
func (in *Instance) Serve(ctx context.Context, onActivate func()) error {
	if in.ln == nil {
		return in.listenErr
	}
	stop := context.AfterFunc(ctx, func() { _ = in.ln.Close() })
	defer stop()
	for {
		conn, err := in.ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("instance: accept: %w", err)
		}
		if read(conn) == activate {
			onActivate()
		}
	}
}

// read returns the first line conn sends, within exchangeTime, and closes it.
func read(conn net.Conn) string {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(exchangeTime))
	line, _ := bufio.NewReader(conn).ReadString('\n')
	return strings.TrimSpace(line)
}

// Close stops serving, removes the socket and releases the lock.
func (in *Instance) Close() error {
	var err error
	in.closeOnce.Do(func() {
		if in.ln != nil {
			_ = in.ln.Close()
			_ = os.Remove(in.sock)
		}
		err = errors.Join(unlock(in.lock), in.lock.Close())
	})
	return err
}
