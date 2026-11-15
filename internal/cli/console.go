package cli

import "os"

// Handle values and file types of the Windows console check, here without a build tag so the decision is tested on every platform.
const (
	// invalidHandle is INVALID_HANDLE_VALUE.
	invalidHandle = ^uintptr(0)
	// fileTypeUnknown is FILE_TYPE_UNKNOWN, what GetFileType says of a handle that names nothing.
	fileTypeUnknown = 0
)

// redirected reports whether a standard handle of a GUI-subsystem program already goes somewhere (a pipe, a file, a console the parent passed down), so AttachConsole must leave it alone: a handle that is set, valid and of a known type. Without a console a GUI program's standard handles are 0.
func redirected(handle uintptr, fileType uint32, typeErr error) bool {
	return handle != 0 && handle != invalidHandle && typeErr == nil && fileType != fileTypeUnknown
}

// conoutFlags opens CONOUT$ for reading and writing although the command line only writes to it. Go's os package treats a handle as a console, and writes to it through WriteConsoleW as UTF-16, only when GetConsoleMode succeeds, and GetConsoleMode needs GENERIC_READ; with O_WRONLY the handle would be a plain file and the Chinese output would reach the console as raw UTF-8 bytes, garbled under code page 936 or 437.
const conoutFlags = os.O_RDWR
