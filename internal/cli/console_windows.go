//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachParentProcess is ATTACH_PARENT_PROCESS, (DWORD)-1.
const attachParentProcess = ^uint32(0)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// AttachConsole gives the command line the console of the shell that started it. Spruce.exe is linked as a GUI program (-H=windowsgui), which Windows starts without a console, so its standard output and error go nowhere unless they were redirected. When either is not redirected it attaches to the parent's console and reopens it on CONOUT$; a redirected one (a pipe or a file) is kept. Nothing happens when the parent has no console. main calls it before writing anything.
func AttachConsole() {
	out, errOut := !stdRedirected(os.Stdout), !stdRedirected(os.Stderr)
	if !out && !errOut {
		return
	}
	if ok, _, _ := procAttachConsole.Call(uintptr(attachParentProcess)); ok == 0 {
		return
	}
	if out {
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stdout = f
		}
	}
	if errOut {
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stderr = f
		}
	}
}

// stdRedirected reports whether f, a standard file, already goes somewhere.
func stdRedirected(f *os.File) bool {
	if f == nil {
		return false
	}
	h := f.Fd()
	t, err := windows.GetFileType(windows.Handle(h))
	return redirected(h, t, err)
}
