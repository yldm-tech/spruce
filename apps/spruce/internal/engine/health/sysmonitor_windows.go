package health

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
	"spruce/apps/spruce/internal/engine/execx"
)

// OpenSystemMonitor opens Task Manager with ShellExecute. It does not use runner: Task Manager's manifest requests the highest available level, so CreateProcess (os/exec) fails with ERROR_ELEVATION_REQUIRED for administrators, and a runner would also wait for Task Manager to close. The executable is named by its full System32 path so the current directory is never searched.
func OpenSystemMonitor(ctx context.Context, _ execx.Runner) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sysDir, err := windows.GetSystemDirectory()
	if err != nil {
		return fmt.Errorf("health: locate System32: %w", err)
	}
	exe, err := windows.UTF16PtrFromString(filepath.Join(sysDir, "Taskmgr.exe"))
	if err != nil {
		return fmt.Errorf("health: Task Manager path: %w", err)
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return fmt.Errorf("health: Task Manager verb: %w", err)
	}

	// ShellExecute may hand the launch to shell extensions, which need COM initialized on the calling thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE (already initialized on this thread) also needs a matching CoUninitialize; RPC_E_CHANGED_MODE (initialized as multithreaded) does not, and ShellExecute works there too.
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil || err == syscall.Errno(windows.S_FALSE) {
		defer windows.CoUninitialize()
	}
	if err := windows.ShellExecute(0, verb, exe, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("health: open Task Manager: %w", err)
	}
	return nil
}
