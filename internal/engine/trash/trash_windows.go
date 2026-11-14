package trash

import (
	"context"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32                = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOperationW   = shell32.NewProc("SHFileOperationW")
	procSHQueryRecycleBinW = shell32.NewProc("SHQueryRecycleBinW")
	procSHEmptyRecycleBinW = shell32.NewProc("SHEmptyRecycleBinW")
)

// shellapi.h constants.
const (
	foDelete = 0x0003

	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000

	sherbNoConfirmation = 0x1
	sherbNoProgressUI   = 0x2
	sherbNoSound        = 0x4
)

// shFileOpStruct is SHFILEOPSTRUCTW with the natural alignment of 64-bit Windows (shellapi.h packs it to 1 byte only on 32-bit, which this app does not build for).
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

// shQueryRBInfo is SHQUERYRBINFO.
type shQueryRBInfo struct {
	cbSize      uint32
	i64Size     int64
	i64NumItems int64
}

type shellMover struct{}

// move sends one path to the Recycle Bin. SHFileOperationW returns its own codes, not HRESULTs; 0x7C (DE_INVALIDFILES) and 2 mean the item is missing.
func (shellMover) move(path string) (string, error) {
	from, err := doubleNull(path)
	if err != nil {
		return "", err
	}
	var code uintptr
	withCOM(func() {
		op := shFileOpStruct{
			wFunc:  foDelete,
			pFrom:  &from[0],
			fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI | fofWantNukeWarning,
		}
		code, _, _ = procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
		if code == 0 && op.fAnyOperationsAborted != 0 {
			code = 0x4C7 // ERROR_CANCELLED: the user declined the permanent-deletion warning
		}
		runtime.KeepAlive(from)
	})
	switch code {
	case 0:
		return "", nil
	case 2, 3, 0x7C:
		return "", fmt.Errorf("SHFileOperationW %#x: %w", code, windows.ERROR_FILE_NOT_FOUND)
	case 5, 0x78:
		return "", fmt.Errorf("SHFileOperationW %#x: %w", code, windows.ERROR_ACCESS_DENIED)
	case 32:
		return "", fmt.Errorf("SHFileOperationW %#x: %w", code, windows.ERROR_SHARING_VIOLATION)
	}
	return "", fmt.Errorf("SHFileOperationW failed with %#x", code)
}

// doubleNull encodes path as the double-NUL-terminated list SHFileOperationW expects.
func doubleNull(path string) ([]uint16, error) {
	u, err := windows.UTF16FromString(path)
	if err != nil {
		return nil, err
	}
	return append(u, 0), nil
}

// withCOM runs fn on one OS thread with COM initialized as single-threaded, as shell file operations require.
func withCOM(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE (already initialized on this thread) also needs a matching CoUninitialize; RPC_E_CHANGED_MODE (initialized as multithreaded) does not, and the shell calls work there too.
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil || err == syscall.Errno(windows.S_FALSE) {
		defer windows.CoUninitialize()
	}
	fn()
}

type windowsBin struct{ mover mover }

func systemBin() Bin { return &windowsBin{mover: shellMover{}} }

func (b *windowsBin) MoveToTrash(ctx context.Context, paths []string) ([]Outcome, error) {
	return moveAll(ctx, b.mover, paths)
}

func (b *windowsBin) Size(ctx context.Context) (TrashUsage, error) {
	if err := ctx.Err(); err != nil {
		return TrashUsage{}, err
	}
	info := shQueryRBInfo{cbSize: uint32(unsafe.Sizeof(shQueryRBInfo{}))}
	if hr, _, _ := procSHQueryRecycleBinW.Call(0, uintptr(unsafe.Pointer(&info))); hr != 0 {
		return TrashUsage{}, fmt.Errorf("trash: SHQueryRecycleBinW: %w", syscall.Errno(hr))
	}
	return TrashUsage{Bytes: info.i64Size, Items: info.i64NumItems}, nil
}

func (b *windowsBin) Empty(ctx context.Context) (TrashUsage, error) {
	before, err := b.Size(ctx)
	if err != nil {
		return TrashUsage{}, err
	}
	if before.Items == 0 {
		return TrashUsage{}, nil
	}
	var hr uintptr
	withCOM(func() {
		hr, _, _ = procSHEmptyRecycleBinW.Call(0, 0, sherbNoConfirmation|sherbNoProgressUI|sherbNoSound)
	})
	if hr != 0 {
		return TrashUsage{}, fmt.Errorf("trash: SHEmptyRecycleBinW: %w", syscall.Errno(hr))
	}
	after, err := b.Size(ctx)
	if err != nil {
		return before, nil
	}
	return TrashUsage{Bytes: max(0, before.Bytes-after.Bytes), Items: max(0, before.Items-after.Items)}, nil
}
