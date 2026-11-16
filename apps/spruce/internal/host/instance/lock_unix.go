//go:build !windows

package instance

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// tryLock takes an exclusive lock on f without waiting: false when another open file holds it.
func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }

// allowForeground does nothing: macOS lets the running copy activate itself.
func allowForeground() {}

// Owner returns the user ID owning the file at path, for the log when a lock cannot be taken.
func Owner(path string) (uid int, ok bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
