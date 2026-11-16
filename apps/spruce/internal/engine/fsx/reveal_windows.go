package fsx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"spruce/apps/spruce/internal/engine/execx"
)

var (
	shell32                        = windows.NewLazySystemDLL("shell32.dll")
	procILCreateFromPathW          = shell32.NewProc("ILCreateFromPathW")
	procILFree                     = shell32.NewProc("ILFree")
	procSHOpenFolderAndSelectItems = shell32.NewProc("SHOpenFolderAndSelectItems")
)

// Reveal shows path selected in an Explorer window with SHOpenFolderAndSelectItems, the call Explorer's own "Show in folder" makes. It does not run explorer.exe /select through runner: Explorer splits that argument at commas and Go quotes arguments only when they contain spaces, so a path such as C:\a,b\c.txt would open the wrong folder; explorer.exe also exits with status 1 on success.
func Reveal(ctx context.Context, _ execx.Runner, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %q", ErrNotAbsolute, path)
	}
	if _, err := os.Lstat(path); err != nil {
		return fmt.Errorf("fsx: %w", err)
	}
	p, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("fsx: reveal %q: %w", path, err)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE (already initialized on this thread) also needs a matching CoUninitialize; RPC_E_CHANGED_MODE (initialized as multithreaded) does not, and the shell call works there too.
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil || err == syscall.Errno(windows.S_FALSE) {
		defer windows.CoUninitialize()
	}
	pidl, _, _ := procILCreateFromPathW.Call(uintptr(unsafe.Pointer(p)))
	if pidl == 0 {
		return fmt.Errorf("fsx: reveal %q: ILCreateFromPathW failed", path)
	}
	defer func() { _, _, _ = procILFree.Call(pidl) }()
	// With no child items, the folder PIDL is the item to select.
	if hr, _, _ := procSHOpenFolderAndSelectItems.Call(pidl, 0, 0, 0); hr != 0 {
		return fmt.Errorf("fsx: reveal %q: SHOpenFolderAndSelectItems: %w", path, syscall.Errno(hr))
	}
	return nil
}
