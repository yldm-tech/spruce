package space

import (
	"os"
	"path/filepath"
)

// Volume is a disk the page offers to scan, with its capacity.
type Volume struct {
	// Path is the folder a scan of the volume walks: "/" for the macOS startup disk, "C:\" for a Windows drive.
	Path string
	// Name is what Finder or Explorer calls it, e.g. "Macintosh HD" or "本地磁盘 (C:)".
	Name string
	// Total and Free are in bytes; Free is what the current user can still write.
	Total int64
	Free  int64
	// Startup is set on the volume the system runs from.
	Startup bool
}

// Target is a folder the page offers to scan in one click.
type Target struct {
	// ID is stable: "home", "downloads", "applications" (macOS) or "programFiles" (Windows).
	ID   string
	Name string
	Path string
}

// usageFunc returns a mounted volume's total and free bytes.
type usageFunc func(mount string) (total, free int64, err error)

// existingDirs keeps the targets whose folder exists.
func existingDirs(targets []Target) []Target {
	out := make([]Target, 0, len(targets))
	for _, t := range targets {
		if t.Path == "" || !filepath.IsAbs(t.Path) {
			continue
		}
		if info, err := os.Stat(t.Path); err == nil && info.IsDir() {
			out = append(out, t)
		}
	}
	return out
}

// clampBytes converts a byte count of the system to int64.
func clampBytes(v uint64) int64 {
	if v > 1<<62 {
		return 1 << 62
	}
	return int64(v)
}
