package fsx

import "io/fs"

// usage returns a file's size: Windows reports no allocation size without opening the file, and Explorer's sizes are file sizes too. Hard links are not detected (that needs a handle per file), so linked files count once per link.
func usage(info fs.FileInfo) (alloc int64, id fileID, linked bool) {
	return info.Size(), fileID{}, false
}

// deviceOf reports no volume: on Windows another volume is reachable only through a mount point, a reparse point the walk never follows.
func deviceOf(fs.FileInfo) (uint64, bool) {
	return 0, false
}
