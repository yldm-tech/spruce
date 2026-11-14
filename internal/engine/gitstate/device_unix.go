//go:build !windows

package gitstate

import (
	"io/fs"
	"syscall"
)

// device is the device number of what info describes; ok is false when the platform does not say.
func device(info fs.FileInfo) (dev uint64, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}
