package elevate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/yldm-tech/spruce/internal/engine/execx"
)

// ShellExecuteEx cannot capture the output of an elevated process, so the Windows child writes its response into an exchange directory the parent created with os.MkdirTemp.
const platformChannel = viaFile

// SHELLEXECUTEINFOW fMask flags (shellapi.h). golang.org/x/sys/windows binds ShellExecuteW but not ShellExecuteExW, which is the only variant that returns the process handle.
const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
)

// waitPoll is how often Launch checks ctx while it waits for the elevated process.
const waitPoll = 200 * time.Millisecond

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteInfo is SHELLEXECUTEINFOW (shellapi.h). Go lays the fields out with the same alignment as the C compiler on 386, amd64 and arm64.
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

// NewLauncher returns the Windows Launcher: ShellExecuteEx with the "runas" verb, which shows the UAC prompt, then a wait on the process handle. runner is not used; the parameter keeps the constructor the same on every platform.
// The running executable is recorded here, when main.go creates the launcher at startup.
func NewLauncher(execx.Runner) Launcher {
	img, err := snapshotSelf()
	return &runasLauncher{self: func() (selfImage, error) { return img, err }}
}

type runasLauncher struct {
	// self returns the running executable as recorded at startup.
	self func() (selfImage, error)
}

// Launch implements Launcher. It returns nil stdout: the child answers through the exchange directory.
func (l *runasLauncher) Launch(ctx context.Context, exe string, args []string) ([]byte, error) {
	if err := checkSelf(exe, l.self); err != nil {
		return nil, err
	}
	process, err := shellExecuteRunas(exe, joinParameters(args))
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(process)
	code, err := waitProcess(ctx, process)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("elevate: the elevated process exited with status %d", code)
	}
	return nil, nil
}

// shellExecuteRunas starts exe with params elevated and returns its process handle. It blocks until the user answers the UAC prompt.
func shellExecuteRunas(exe, params string) (windows.Handle, error) {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 0, err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	parameters, err := windows.UTF16PtrFromString(params)
	if err != nil {
		return 0, err
	}

	// ShellExecuteEx may hand the request to Shell extensions over COM, so Microsoft asks callers to initialize COM as STA on the calling thread first.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err {
	case nil, syscall.Errno(windows.S_FALSE):
		defer windows.CoUninitialize()
	case syscall.Errno(windows.RPC_E_CHANGED_MODE):
		// The thread already belongs to a multithreaded apartment; ShellExecuteEx works there too.
	default:
		return 0, fmt.Errorf("elevate: CoInitializeEx: %w", err)
	}

	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		hwnd:         ownWindow(),
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: parameters,
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 0, shellExecuteError(callErr)
	}
	if info.hProcess == 0 {
		return 0, errors.New("elevate: ShellExecuteEx started no process")
	}
	return info.hProcess, nil
}

// shellExecuteError maps the error of ShellExecuteExW: a declined or dismissed UAC prompt is ERROR_CANCELLED.
func shellExecuteError(err error) error {
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return ErrCancelled
	}
	return fmt.Errorf("elevate: ShellExecuteEx: %w", err)
}

// ownWindow returns the foreground window when it belongs to this process (the user just pressed 运行 in it), so that the UAC prompt comes up in front of the app instead of flashing in the taskbar; otherwise no owner.
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

// waitProcess waits for the process to exit and returns its exit code. When ctx ends first it tries to terminate the process (the handle may lack the right to) and returns ctx's error.
func waitProcess(ctx context.Context, process windows.Handle) (uint32, error) {
	for {
		event, err := windows.WaitForSingleObject(process, uint32(waitPoll/time.Millisecond))
		switch event {
		case windows.WAIT_OBJECT_0:
			var code uint32
			if err := windows.GetExitCodeProcess(process, &code); err != nil {
				return 0, fmt.Errorf("elevate: GetExitCodeProcess: %w", err)
			}
			return code, nil
		case uint32(windows.WAIT_TIMEOUT):
			if ctx.Err() != nil {
				_ = windows.TerminateProcess(process, 1)
				return 0, ctx.Err()
			}
		default:
			return 0, fmt.Errorf("elevate: WaitForSingleObject: %w", err)
		}
	}
}

// joinParameters turns args into the lpParameters string, quoted so that CommandLineToArgvW and the Go runtime split it back into exactly args.
func joinParameters(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = windows.EscapeArg(a)
	}
	return strings.Join(quoted, " ")
}

func samePath(a, b string) bool { return strings.EqualFold(a, b) }

func platformChildEnv() childEnv {
	return childEnv{
		channel:  viaFile,
		stderr:   os.Stderr,
		checkDir: dirOwnedByUnprivilegedAccount,
		elevated: func() bool { return windows.GetCurrentProcessToken().IsElevated() },
	}
}

// trustedInstallerSID is the service SID of TrustedInstaller (NT SERVICE\TrustedInstaller), which owns most of the Windows directory.
const trustedInstallerSID = "S-1-5-80-956008885-3425701886-2022237836-3622500891-2224575489"

// dirOwnedByUnprivilegedAccount accepts the exchange directory only when its owner is not a privileged account. The non-elevated parent creates it with os.MkdirTemp, so its owner is the requesting user, both with same-user consent and with over-the-shoulder elevation; objects created by an elevated administrator token are owned by the Administrators group, and system directories by SYSTEM or TrustedInstaller. So the elevated child only writes where the requesting user could already write.
func dirOwnedByUnprivilegedAccount(dir string) error {
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read the owner: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read the owner: %w", err)
	}
	if privilegedOwner(owner) {
		return fmt.Errorf("it is owned by the privileged account %s", owner)
	}
	return nil
}

// privilegedOwner reports whether sid is an account whose directories the elevated child must never write into.
func privilegedOwner(sid *windows.SID) bool {
	for _, k := range []windows.WELL_KNOWN_SID_TYPE{windows.WinLocalSystemSid, windows.WinBuiltinAdministratorsSid, windows.WinLocalServiceSid, windows.WinNetworkServiceSid} {
		if sid.IsWellKnown(k) {
			return true
		}
	}
	ti, err := windows.StringToSid(trustedInstallerSID)
	return err != nil || sid.Equals(ti)
}
