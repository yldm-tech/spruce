package uninstall

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SHELLEXECUTEINFOW fMask flags (shellapi.h). golang.org/x/sys/windows binds ShellExecuteW but not ShellExecuteExW, the only variant that returns the process handle.
const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
)

// waitPoll is how often the wait for an uninstaller checks ctx.
const waitPoll = 250 * time.Millisecond

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteInfo is SHELLEXECUTEINFOW (shellapi.h), laid out as the C compiler does on amd64 and arm64.
type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           windows.HWND
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      windows.Handle
	dwHotKey       uint32
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

// errUACDeclined is returned when the user declined or dismissed the UAC prompt.
var errUACDeclined = errors.New("uninstall: the UAC prompt was declined")

// shellExecute starts program with params through the shell, as double-clicking it would: an uninstaller that asks for administrator rights shows the UAC prompt. It returns the process handle, or 0 when the shell handed the program to a running process.
func shellExecute(program, params string) (windows.Handle, error) {
	file, err := windows.UTF16PtrFromString(program)
	if err != nil {
		return 0, err
	}
	var parameters *uint16
	if params != "" {
		if parameters, err = windows.UTF16PtrFromString(params); err != nil {
			return 0, err
		}
	}

	// ShellExecuteEx may hand the request to Shell extensions over COM, so Microsoft asks callers to initialize COM as STA on the calling thread first.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err {
	case nil, syscall.Errno(windows.S_FALSE):
		defer windows.CoUninitialize()
	case syscall.Errno(windows.RPC_E_CHANGED_MODE):
	default:
		return 0, fmt.Errorf("uninstall: CoInitializeEx: %w", err)
	}

	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		hwnd:         ownWindow(),
		lpFile:       file,
		lpParameters: parameters,
		nShow:        windows.SW_SHOWNORMAL,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return 0, errUACDeclined
		}
		return 0, fmt.Errorf("uninstall: ShellExecuteEx %s: %w", program, callErr)
	}
	return info.hProcess, nil
}

// ownWindow returns the foreground window when it belongs to this process, so the UAC prompt and the uninstaller come up in front of the app; otherwise no owner.
func ownWindow() windows.HWND {
	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return 0
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid != windows.GetCurrentProcessId() {
		return 0
	}
	return hwnd
}

// waitExit waits for the process to exit and reports true, or reports false when ctx ends first. It never terminates the process: stopping the wait leaves the uninstaller running.
func waitExit(ctx context.Context, process windows.Handle) (bool, error) {
	for {
		event, err := windows.WaitForSingleObject(process, uint32(waitPoll/time.Millisecond))
		switch event {
		case windows.WAIT_OBJECT_0:
			return true, nil
		case uint32(windows.WAIT_TIMEOUT):
			if ctx.Err() != nil {
				return false, nil
			}
		default:
			return false, fmt.Errorf("uninstall: WaitForSingleObject: %w", err)
		}
	}
}
