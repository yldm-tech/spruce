package gitstate

import "io/fs"

// device reports no device number on Windows: the walk up stops at the root of the drive.
func device(fs.FileInfo) (uint64, bool) { return 0, false }
