package instance

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLock takes an exclusive lock on f's first byte without waiting: false when another handle holds it.
func tryLock(f *os.File) (bool, error) {
	var ol windows.Overlapped
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}

// asfwAny lets any process take the foreground.
const asfwAny = 0xFFFFFFFF

var allowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

// allowForeground lets the running copy bring its window to the front: Windows refuses SetForegroundWindow to a process that did not receive the last input, which this later copy did.
func allowForeground() {
	if allowSetForegroundWindow.Find() == nil {
		_, _, _ = allowSetForegroundWindow.Call(asfwAny)
	}
}

// Owner is unknown on Windows, where files have no user ID.
func Owner(string) (uid int, ok bool) { return 0, false }
