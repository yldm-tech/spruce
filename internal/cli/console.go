package cli

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
