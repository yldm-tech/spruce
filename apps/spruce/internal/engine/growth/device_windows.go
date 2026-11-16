package growth

import "io/fs"

// deviceOf reports no volume on Windows, where other volumes are mounted through reparse points, which Take never follows.
func deviceOf(fs.FileInfo) (uint64, bool) {
	return 0, false
}
