package space

import (
	"os"
	"path/filepath"
	"strings"
)

// foldPaths: Windows paths are case-insensitive.
const foldPaths = true

// SystemLocations are the folders outside the home folder where the page moves nothing to the Recycle Bin: Windows, the program folders and ProgramData.
func SystemLocations() []string {
	return []string{
		envOr("SystemRoot", `C:\Windows`),
		envOr("ProgramFiles", `C:\Program Files`),
		envOr("ProgramFiles(x86)", `C:\Program Files (x86)`),
		envOr("ProgramData", `C:\ProgramData`),
	}
}

func startupRoot() string {
	drive := os.Getenv("SystemDrive")
	if len(drive) != 2 || drive[1] != ':' {
		drive = "C:"
	}
	return strings.ToUpper(drive) + `\`
}

func appsFolder() string { return "" }

// isVolumeRoot reports whether a cleaned absolute path is the root of a volume.
func isVolumeRoot(p string) bool {
	vol := filepath.VolumeName(p)
	return vol != "" && (p == vol || p == vol+`\`)
}

// planFor returns nothing on Windows: other volumes are reachable only through reparse points, which the walk never follows.
func planFor(string) (skip, same []string) { return nil, nil }

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" && filepath.IsAbs(v) {
		return filepath.Clean(v)
	}
	return fallback
}
