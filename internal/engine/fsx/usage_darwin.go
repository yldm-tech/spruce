package fsx

import (
	"io/fs"
	"syscall"
)

// usage returns the space a file takes on disk (its allocated 512-byte blocks), its identity and whether it has other hard links, which DirSize must count once and SafeRemove frees only with the last link.
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
