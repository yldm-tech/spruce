package settings

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func devRootCandidates() []string {
	return []string{`source\repos`, "Workspace", "Projects", "Developer", "code", "src"}
}

// installerRoots uses the known folders, which the user may have moved off the home folder, and falls back to the home folder's own.
func installerRoots(home string) []string {
	var out []string
	for _, f := range []struct {
		id   *windows.KNOWNFOLDERID
		name string
	}{{windows.FOLDERID_Downloads, "Downloads"}, {windows.FOLDERID_Desktop, "Desktop"}} {
		if dir, err := windows.KnownFolderPath(f.id, windows.KF_FLAG_DEFAULT); err == nil && dir != "" {
			out = append(out, dir)
			continue
		}
		out = append(out, filepath.Join(home, f.name))
	}
	return out
}
