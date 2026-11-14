package installers

import (
	"io/fs"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// fillPlatform sets the Windows defaults of what env leaves zero: the Downloads known folder, which the user may have moved. Attached .iso images are not listed: Windows refuses to delete one while it is mounted, and the clean then reports it.
func fillPlatform(env *Env) {
	if env.Downloads == "" {
		if dir, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, windows.KF_FLAG_DEFAULT); err == nil && dir != "" {
			env.Downloads = dir
		} else {
			env.Downloads = filepath.Join(env.Home, "Downloads")
		}
	}
}

// hidden reports whether the item has the hidden attribute.
func hidden(info fs.FileInfo) bool {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && d.FileAttributes&windows.FILE_ATTRIBUTE_HIDDEN != 0
}

// created is the file's creation time, or the zero time.
func created(info fs.FileInfo) time.Time {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return time.Time{}
	}
	return time.Unix(0, d.CreationTime.Nanoseconds())
}
