package undo

import (
	"os"

	"golang.org/x/sys/unix"
)

// syncJournal hands what was written to f to the drive with a plain fsync. os.File.Sync on macOS is F_FULLFSYNC, which also flushes the drive's cache and costs milliseconds per call, once per item moved; the journal only has to outlive Spruce and the system's own crashes, which fsync covers.
func syncJournal(f *os.File) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) {
		for {
			serr = unix.Fsync(int(fd))
			if serr != unix.EINTR {
				return
			}
		}
	}); err != nil {
		return err
	}
	return serr
}
