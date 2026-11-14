package space

import (
	"io/fs"
	"syscall"
)

// fileID identifies a file across hard links (device and inode).
type fileID struct{ dev, ino uint64 }

// usage returns the space a file takes on disk (its allocated 512-byte blocks, as fsx.DirSize counts), its identity and whether it has other hard links, which the walk counts once.
func usage(info fs.FileInfo) (alloc int64, id fileID, linked bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size(), fileID{}, false
	}
	return st.Blocks * 512, fileID{dev: uint64(st.Dev), ino: st.Ino}, st.Nlink > 1
}

// deviceOf returns the volume a file is on.
func deviceOf(info fs.FileInfo) (uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}
