package cleanup

import "io/fs"

// identity is zero on Windows: os.Lstat does not report a file index, so changed compares the type, and for files in the Trash the time and size.
func identity(fs.FileInfo) (dev, ino uint64) { return 0, 0 }
