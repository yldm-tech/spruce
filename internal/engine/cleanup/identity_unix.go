//go:build !windows

package cleanup

import (
	"io/fs"
	"syscall"
)

// identity returns the device and inode os.Lstat reported, as uint64(Stat_t.Dev) and Stat_t.Ino.
func identity(info fs.FileInfo) (dev, ino uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return uint64(st.Dev), st.Ino
}
